package build

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"schooner/internal/database"
	"schooner/internal/database/queries"
	"schooner/internal/docker"
	"schooner/internal/git"
	"schooner/internal/models"
)

// Orchestrator coordinates build execution
type Orchestrator struct {
	strategies   map[models.BuildStrategy]Strategy
	gitClient    *git.Client
	dockerClient *docker.Client
	appQueries   *queries.AppQueries
	buildQueries *queries.BuildQueries
	logQueries   *queries.LogQueries
	logger       *slog.Logger

	// Build queue
	buildQueue chan string
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc

	// Per-app locks to prevent concurrent builds for the same app
	appLocks   map[string]*sync.Mutex
	appLocksMu sync.Mutex
}

// NewOrchestrator creates a new build orchestrator
func NewOrchestrator(
	gitClient *git.Client,
	dockerClient *docker.Client,
	appQueries *queries.AppQueries,
	buildQueries *queries.BuildQueries,
	logQueries *queries.LogQueries,
) *Orchestrator {
	ctx, cancel := context.WithCancel(context.Background())

	o := &Orchestrator{
		strategies:   make(map[models.BuildStrategy]Strategy),
		gitClient:    gitClient,
		dockerClient: dockerClient,
		appQueries:   appQueries,
		buildQueries: buildQueries,
		logQueries:   logQueries,
		logger:       slog.Default(),
		buildQueue:   make(chan string, 100),
		ctx:          ctx,
		cancel:       cancel,
		appLocks:     make(map[string]*sync.Mutex),
	}

	return o
}

// RegisterStrategy registers a build strategy
func (o *Orchestrator) RegisterStrategy(strategy Strategy) {
	o.strategies[strategy.Name()] = strategy
}

// Start begins processing builds
func (o *Orchestrator) Start(workers int) {
	o.logger.Info("starting build orchestrator", "workers", workers)

	for i := 0; i < workers; i++ {
		o.wg.Add(1)
		go o.worker(i)
	}
}

// Stop gracefully stops the orchestrator
func (o *Orchestrator) Stop() {
	o.logger.Info("stopping build orchestrator")
	o.cancel()
	close(o.buildQueue)
	o.wg.Wait()
}

// QueueBuild adds a build to the queue
func (o *Orchestrator) QueueBuild(buildID string) {
	select {
	case o.buildQueue <- buildID:
		o.logger.Debug("build queued", "buildID", buildID)
	default:
		o.logger.Warn("build queue full, dropping build", "buildID", buildID)
	}
}

// getAppLock returns the mutex for a specific app, creating one if needed
func (o *Orchestrator) getAppLock(appID string) *sync.Mutex {
	o.appLocksMu.Lock()
	defer o.appLocksMu.Unlock()

	if lock, ok := o.appLocks[appID]; ok {
		return lock
	}

	lock := &sync.Mutex{}
	o.appLocks[appID] = lock
	return lock
}

// worker processes builds from the queue
func (o *Orchestrator) worker(id int) {
	defer o.wg.Done()

	for {
		select {
		case <-o.ctx.Done():
			return
		case buildID, ok := <-o.buildQueue:
			if !ok {
				return
			}
			o.processBuild(buildID)
		}
	}
}

// processBuild executes a single build
func (o *Orchestrator) processBuild(buildID string) {
	ctx := o.ctx
	logger := o.logger.With("buildID", buildID)

	// Get build
	build, err := o.buildQueries.GetByID(ctx, buildID)
	if err != nil || build == nil {
		logger.Error("failed to get build", "error", err)
		return
	}

	// Acquire per-app lock to prevent concurrent builds for the same app
	appLock := o.getAppLock(build.AppID)
	appLock.Lock()
	defer appLock.Unlock()

	// Get app
	app, err := o.appQueries.GetByID(ctx, build.AppID)
	if err != nil || app == nil {
		logger.Error("failed to get app", "error", err)
		o.failBuild(ctx, build, "failed to get app configuration")
		return
	}

	logger = logger.With("app", app.Name)
	logger.Info("starting build (app locked)")

	// Create log writer
	logWriter := newBuildLogWriter(build.ID, o.logQueries)

	// Update build status to cloning
	build.Status = models.BuildStatusCloning
	build.StartedAt = database.NullTime(time.Now())
	o.buildQueries.Update(ctx, build)

	// Clone/pull repository
	fmt.Fprintf(logWriter, "Cloning repository: %s\n", app.RepoURL)
	fmt.Fprintf(logWriter, "Branch: %s\n", app.Branch)

	repo, err := o.gitClient.CloneOrPull(ctx, git.CloneOptions{
		URL:      app.RepoURL,
		Branch:   app.Branch,
		Depth:    1,
		Progress: logWriter,
	})
	if err != nil {
		logger.Error("clone failed", "error", err)
		fmt.Fprintf(logWriter, "\nERROR: Failed to clone repository: %s\n", err)
		o.failBuild(ctx, build, fmt.Sprintf("clone failed: %v", err))
		return
	}

	// Get commit info
	commit, err := o.gitClient.GetHeadCommit(repo)
	if err == nil {
		build.CommitSHA = database.NullString(commit.Hash.String())
		build.CommitMessage = database.NullString(commit.Message)
		build.CommitAuthor = database.NullString(commit.Author.Name)
		o.buildQueries.Update(ctx, build)

		fmt.Fprintf(logWriter, "\nCommit: %s\n", commit.Hash.String()[:8])
		fmt.Fprintf(logWriter, "Author: %s\n", commit.Author.Name)
		fmt.Fprintf(logWriter, "Message: %s\n", commit.Message)
	}

	// Determine build strategy (autodetect if needed)
	buildStrategy := app.BuildStrategy
	repoPath := o.gitClient.RepoPath(app.RepoURL)

	if buildStrategy == models.BuildStrategyAutodetect {
		detected, composeFile := o.detectBuildStrategy(repoPath)
		buildStrategy = detected

		if composeFile != "" {
			app.ComposeFile = composeFile
		}

		fmt.Fprintf(logWriter, "\nAutodetected build strategy: %s\n", buildStrategy)
		if composeFile != "" {
			fmt.Fprintf(logWriter, "Compose file: %s\n", composeFile)
		}
	}

	// Get build strategy
	strategy, ok := o.strategies[buildStrategy]
	if !ok {
		logger.Error("unknown build strategy", "strategy", buildStrategy)
		fmt.Fprintf(logWriter, "\nERROR: Unknown build strategy: %s\n", buildStrategy)
		o.failBuild(ctx, build, fmt.Sprintf("unknown build strategy: %s", buildStrategy))
		return
	}

	// Prepare build options
	buildOpts := BuildOptions{
		AppID:        app.ID,
		AppName:      app.Name,
		RepoPath:     repoPath,
		ImageName:    app.GetImageName(),
		Tag:          build.ID[:8],
		BuildContext: app.BuildContext,
		Dockerfile:   app.DockerfilePath,
		ComposeFile:  app.ComposeFile,
		EnvVars:      app.EnvVars,
		LogWriter:    logWriter,
	}

	// Validate
	fmt.Fprintf(logWriter, "\nValidating build configuration...\n")
	if err := strategy.Validate(ctx, buildOpts); err != nil {
		logger.Error("validation failed", "error", err)
		fmt.Fprintf(logWriter, "ERROR: Validation failed: %s\n", err)
		o.failBuild(ctx, build, fmt.Sprintf("validation failed: %v", err))
		return
	}

	// Update status to building
	build.Status = models.BuildStatusBuilding
	o.buildQueries.Update(ctx, build)
	fmt.Fprintf(logWriter, "\n--- Starting Build ---\n\n")

	// Execute build
	result, err := strategy.Build(ctx, buildOpts)
	if err != nil {
		logger.Error("build failed", "error", err)
		fmt.Fprintf(logWriter, "\nERROR: Build failed: %s\n", err)
		o.failBuild(ctx, build, fmt.Sprintf("build failed: %v", err))
		return
	}

	build.ImageTag = database.NullString(result.ImageTag)

	// Update status to deploying
	build.Status = models.BuildStatusDeploying
	o.buildQueries.Update(ctx, build)
	fmt.Fprintf(logWriter, "\n--- Deploying ---\n\n")

	// Deploy based on strategy
	if buildStrategy == models.BuildStrategyCompose {
		// For compose, run docker compose up
		composeStrategy := strategy.(composeStrategyWrapper)
		if err := composeStrategy.Up(ctx, buildOpts); err != nil {
			logger.Error("deploy failed", "error", err)
			fmt.Fprintf(logWriter, "ERROR: Deploy failed: %s\n", err)
			o.failBuild(ctx, build, fmt.Sprintf("deploy failed: %v", err))
			return
		}
	} else {
		// For other strategies, run container
		fmt.Fprintf(logWriter, "Deploying container: %s\n", app.GetContainerName())

		containerConfig := docker.ContainerConfig{
			Name:          app.GetContainerName(),
			Image:         result.ImageTag,
			Env:           envMapToSlice(app.EnvVars),
			RestartPolicy: "unless-stopped",
			Labels: map[string]string{
				"schooner.managed":  "true",
				"schooner.app":      app.Name,
				"schooner.app-id":   app.ID,
				"schooner.build-id": build.ID,
			},
		}

		// Parse deploy config for ports/volumes if set
		// TODO: Parse app.DeployConfig for additional settings

		containerID, err := o.dockerClient.RunContainer(ctx, containerConfig)
		if err != nil {
			logger.Error("deploy failed", "error", err)
			fmt.Fprintf(logWriter, "ERROR: Deploy failed: %s\n", err)
			o.failBuild(ctx, build, fmt.Sprintf("deploy failed: %v", err))
			return
		}

		fmt.Fprintf(logWriter, "Container started: %s\n", containerID[:12])
	}

	// Build succeeded
	build.Status = models.BuildStatusSuccess
	build.FinishedAt = database.NullTime(time.Now())
	o.buildQueries.Update(ctx, build)

	duration := build.Duration()
	fmt.Fprintf(logWriter, "\n--- Build Complete ---\n")
	fmt.Fprintf(logWriter, "Duration: %s\n", duration.Round(time.Second))
	fmt.Fprintf(logWriter, "Status: SUCCESS\n")

	logger.Info("build completed", "duration", duration)
}

// failBuild marks a build as failed
func (o *Orchestrator) failBuild(ctx context.Context, build *models.Build, message string) {
	build.Status = models.BuildStatusFailed
	build.ErrorMessage = database.NullString(message)
	build.FinishedAt = database.NullTime(time.Now())
	o.buildQueries.Update(ctx, build)
}

// TriggerManualBuild creates and queues a manual build
func (o *Orchestrator) TriggerManualBuild(ctx context.Context, appID string) (*models.Build, error) {
	app, err := o.appQueries.GetByID(ctx, appID)
	if err != nil {
		return nil, err
	}
	if app == nil {
		return nil, fmt.Errorf("app not found")
	}

	build := &models.Build{
		ID:        uuid.New().String(),
		AppID:     app.ID,
		Status:    models.BuildStatusPending,
		Trigger:   models.TriggerManual,
		Branch:    database.NullString(app.Branch),
		CreatedAt: time.Now(),
	}

	if err := o.buildQueries.Create(ctx, build); err != nil {
		return nil, err
	}

	// Add initial log
	log := &models.BuildLog{
		BuildID:   build.ID,
		Level:     models.LogLevelInfo,
		Message:   "Build triggered manually",
		Source:    models.LogSourceSystem,
		Timestamp: time.Now(),
	}
	o.logQueries.Append(ctx, log)

	o.QueueBuild(build.ID)

	return build, nil
}

// buildLogWriter writes logs to the database
type buildLogWriter struct {
	buildID    string
	logQueries *queries.LogQueries
	buffer     []byte
}

func newBuildLogWriter(buildID string, logQueries *queries.LogQueries) *buildLogWriter {
	return &buildLogWriter{
		buildID:    buildID,
		logQueries: logQueries,
	}
}

func (w *buildLogWriter) Write(p []byte) (n int, err error) {
	w.buffer = append(w.buffer, p...)

	// Process complete lines
	for {
		idx := -1
		for i, b := range w.buffer {
			if b == '\n' {
				idx = i
				break
			}
		}
		if idx == -1 {
			break
		}

		line := string(w.buffer[:idx])
		w.buffer = w.buffer[idx+1:]

		if line == "" {
			continue
		}

		log := &models.BuildLog{
			BuildID:   w.buildID,
			Level:     models.LogLevelInfo,
			Message:   line,
			Source:    models.LogSourceDocker,
			Timestamp: time.Now(),
		}
		w.logQueries.Append(context.Background(), log)
	}

	return len(p), nil
}

func (w *buildLogWriter) Flush() {
	if len(w.buffer) > 0 {
		log := &models.BuildLog{
			BuildID:   w.buildID,
			Level:     models.LogLevelInfo,
			Message:   string(w.buffer),
			Source:    models.LogSourceDocker,
			Timestamp: time.Now(),
		}
		w.logQueries.Append(context.Background(), log)
		w.buffer = nil
	}
}

// detectBuildStrategy examines the repo to determine the best build strategy.
// Returns the detected strategy and the compose file path (if compose is detected).
func (o *Orchestrator) detectBuildStrategy(repoPath string) (models.BuildStrategy, string) {
	// Check for docker-compose files first (higher priority)
	composeFiles := []string{
		"docker-compose.yml",
		"docker-compose.yaml",
		"compose.yml",
		"compose.yaml",
	}

	for _, f := range composeFiles {
		path := filepath.Join(repoPath, f)
		if _, err := os.Stat(path); err == nil {
			return models.BuildStrategyCompose, f
		}
	}

	// Check for Dockerfile
	dockerfilePath := filepath.Join(repoPath, "Dockerfile")
	if _, err := os.Stat(dockerfilePath); err == nil {
		return models.BuildStrategyDockerfile, ""
	}

	// Default to Dockerfile strategy even if not found
	// (validation will catch the missing file)
	return models.BuildStrategyDockerfile, ""
}

// envMapToSlice converts a map to KEY=VALUE slice
func envMapToSlice(m map[string]string) []string {
	var result []string
	for k, v := range m {
		result = append(result, fmt.Sprintf("%s=%s", k, v))
	}
	return result
}

// composeStrategyWrapper wraps compose strategy to expose Up method
type composeStrategyWrapper interface {
	Strategy
	Up(ctx context.Context, opts BuildOptions) error
}

// Ensure strategies can be asserted
var _ io.Writer = (*buildLogWriter)(nil)
