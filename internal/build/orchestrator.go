package build

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

// Build timeout (1 hour)
const buildTimeout = 1 * time.Hour

// processBuild executes a single build
func (o *Orchestrator) processBuild(buildID string) {
	// Create timeout context for the entire build
	ctx, cancel := context.WithTimeout(o.ctx, buildTimeout)
	defer cancel()

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
	// Use commit SHA for version, fall back to build ID
	version := build.ID[:8]
	commitSHA := ""
	if len(build.CommitSHA.String) >= 8 {
		version = build.CommitSHA.String[:8]
		commitSHA = build.CommitSHA.String
	}

	// Create env vars with git info injected
	envVars := make(map[string]string)
	for k, v := range app.EnvVars {
		envVars[k] = v
	}
	// Inject git SHA into env vars (can be overridden by user if needed)
	if commitSHA != "" {
		envVars["GIT_SHA"] = commitSHA
		envVars["GIT_COMMIT"] = commitSHA
	}
	envVars["VERSION"] = version

	buildOpts := BuildOptions{
		AppID:        app.ID,
		AppName:      app.Name,
		RepoPath:     repoPath,
		ImageName:    app.GetImageName(),
		Tag:          build.ID[:8],
		BuildContext: app.BuildContext,
		Dockerfile:   app.DockerfilePath,
		ComposeFile:  app.ComposeFile,
		EnvVars:      envVars,
		BuildArgs: map[string]string{
			"VERSION": version,
		},
		LogWriter: logWriter,
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

	// Capture previous image for potential rollback (Dockerfile strategy only)
	var previousImage string
	if buildStrategy != models.BuildStrategyCompose {
		if status, err := o.dockerClient.GetContainerStatus(ctx, app.GetContainerName()); err == nil && status != nil {
			previousImage = status.Image
			fmt.Fprintf(logWriter, "Previous image: %s (for rollback)\n", previousImage)
		}
	}

	// Check for self-deployment
	isSelfDeploy := o.isSelfDeploy(app.GetContainerName())
	if isSelfDeploy {
		fmt.Fprintf(logWriter, "⚠️  Self-deployment detected - using fire-and-forget deploy\n")
	}

	// Deploy based on strategy
	if buildStrategy == models.BuildStrategyCompose {
		// For compose, run docker compose up
		composeStrategy := strategy.(composeStrategyWrapper)

		var err error
		if isSelfDeploy {
			err = composeStrategy.UpSelfDeploy(ctx, buildOpts)
			if err == nil {
				// Mark as success immediately - we're about to be killed
				build.Status = models.BuildStatusSuccess
				build.FinishedAt = database.NullTime(time.Now())
				o.buildQueries.Update(context.Background(), build)

				duration := build.Duration()
				fmt.Fprintf(logWriter, "\n--- Build Complete (self-deploy) ---\n")
				fmt.Fprintf(logWriter, "Duration: %s\n", duration.Round(time.Second))
				fmt.Fprintf(logWriter, "Status: SUCCESS\n")
				fmt.Fprintf(logWriter, "\nContainer will restart momentarily...\n")

				logger.Info("self-deploy initiated", "duration", duration)
				return
			}
		} else {
			err = composeStrategy.Up(ctx, buildOpts)
		}

		if err != nil {
			logger.Error("deploy failed", "error", err)
			fmt.Fprintf(logWriter, "ERROR: Deploy failed: %s\n", err)
			o.failBuild(ctx, build, fmt.Sprintf("deploy failed: %v", err))
			return
		}
	} else if isSelfDeploy {
		// Dockerfile self-deployment: use helper container
		fmt.Fprintf(logWriter, "Self-deployment via helper container...\n")

		if err := o.selfDeployDockerfile(ctx, app, result.ImageTag, logWriter); err != nil {
			logger.Error("self-deploy failed", "error", err)
			fmt.Fprintf(logWriter, "ERROR: Self-deploy failed: %s\n", err)
			o.failBuild(ctx, build, fmt.Sprintf("self-deploy failed: %v", err))
			return
		}

		// Mark as success - we're about to be killed
		build.Status = models.BuildStatusSuccess
		build.FinishedAt = database.NullTime(time.Now())
		o.buildQueries.Update(context.Background(), build)

		duration := build.Duration()
		fmt.Fprintf(logWriter, "\n--- Build Complete (self-deploy) ---\n")
		fmt.Fprintf(logWriter, "Duration: %s\n", duration.Round(time.Second))
		fmt.Fprintf(logWriter, "Status: SUCCESS\n")
		fmt.Fprintf(logWriter, "\nContainer will restart momentarily...\n")

		logger.Info("self-deploy initiated", "duration", duration)
		return
	} else {
		// For other strategies, run container
		fmt.Fprintf(logWriter, "Deploying container: %s\n", app.GetContainerName())

		containerConfig := docker.ContainerConfig{
			Name:          app.GetContainerName(),
			Image:         result.ImageTag,
			Env:           envMapToSlice(envVars),
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

			// Attempt rollback if we have a previous image
			if previousImage != "" {
				fmt.Fprintf(logWriter, "\n--- Attempting Rollback ---\n")
				fmt.Fprintf(logWriter, "Restoring previous image: %s\n", previousImage)

				rollbackConfig := containerConfig
				rollbackConfig.Image = previousImage
				delete(rollbackConfig.Labels, "schooner.build-id") // Don't associate with failed build

				if rollbackID, rollbackErr := o.dockerClient.RunContainer(ctx, rollbackConfig); rollbackErr == nil {
					fmt.Fprintf(logWriter, "✓ Rollback successful: %s\n", rollbackID[:12])
					logger.Info("rollback successful", "previousImage", previousImage)
				} else {
					fmt.Fprintf(logWriter, "✗ Rollback failed: %s\n", rollbackErr)
					logger.Error("rollback failed", "error", rollbackErr)
				}
			}

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
	// Check if this was a timeout
	if ctx.Err() == context.DeadlineExceeded {
		message = fmt.Sprintf("build timed out after %s: %s", buildTimeout, message)
	}

	build.Status = models.BuildStatusFailed
	build.ErrorMessage = database.NullString(message)
	build.FinishedAt = database.NullTime(time.Now())

	// Use background context for the update since the original context may be cancelled
	o.buildQueries.Update(context.Background(), build)
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
	UpSelfDeploy(ctx context.Context, opts BuildOptions) error
}

// Ensure strategies can be asserted
var _ io.Writer = (*buildLogWriter)(nil)

// isSelfDeploy checks if we're trying to deploy the container we're running in.
// This would kill us mid-deployment, so we need to skip deployment in this case.
func (o *Orchestrator) isSelfDeploy(targetContainerName string) bool {
	// Check if we're running in a Docker container
	if _, err := os.Stat("/.dockerenv"); os.IsNotExist(err) {
		return false // Not in Docker, safe to deploy anything
	}

	// Get our hostname (in Docker, this is typically the container ID or name)
	hostname, err := os.Hostname()
	if err != nil {
		return false
	}

	// Check if our hostname matches the target container name
	// Docker sets hostname to container ID by default, but it can be set to the container name
	if hostname == targetContainerName {
		return true
	}

	// Also check by querying Docker for our container's name
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	status, err := o.dockerClient.GetContainerStatus(ctx, hostname)
	if err != nil || status == nil {
		return false
	}

	// Check if our container's name matches the target
	// Container names from Docker API may include leading slash
	containerName := status.Name
	if len(containerName) > 0 && containerName[0] == '/' {
		containerName = containerName[1:]
	}

	return containerName == targetContainerName
}

// selfDeployDockerfile handles self-deployment for Dockerfile strategy using a helper container.
// It spawns a small helper container that will stop the current container and start the new one.
func (o *Orchestrator) selfDeployDockerfile(ctx context.Context, app *models.App, newImageTag string, logWriter io.Writer) error {
	containerName := app.GetContainerName()

	// Get current container info to copy its configuration
	status, err := o.dockerClient.GetContainerStatus(ctx, containerName)
	if err != nil || status == nil || status.State == "not_found" {
		return fmt.Errorf("could not get current container status: %w", err)
	}

	// Get container run arguments to recreate with same configuration
	runArgs, err := o.dockerClient.GetContainerRunArgs(ctx, containerName)
	if err != nil {
		return fmt.Errorf("could not get container configuration: %w", err)
	}

	fmt.Fprintf(logWriter, "Current container ID: %s\n", status.ID[:12])
	fmt.Fprintf(logWriter, "New image: %s\n", newImageTag)

	// Build run args string for the script
	runArgsStr := strings.Join(runArgs, " ")

	// Build the helper script that will do the swap
	// The script waits 2 seconds (to let us finish), then stops old and starts new
	helperScript := fmt.Sprintf(`
		sleep 2
		echo "Stopping old container: %s"
		docker stop %s --time 30 || true
		docker rm %s || true
		echo "Starting new container with image: %s"
		docker run -d --name %s \
			--label schooner.managed=true \
			--label "schooner.app=%s" \
			--label "schooner.app-id=%s" \
			%s \
			%s
		echo "Self-deployment complete"
	`, containerName, containerName, containerName, newImageTag, containerName, app.Name, app.ID, runArgsStr, newImageTag)

	// Create and start helper container
	helperConfig := docker.ContainerConfig{
		Name:  "schooner-deploy-helper",
		Image: "docker:cli",
		Cmd:   []string{"sh", "-c", helperScript},
		Volumes: map[string]string{
			"/var/run/docker.sock": "/var/run/docker.sock",
		},
		Labels: map[string]string{
			"schooner.helper": "true",
		},
	}

	fmt.Fprintf(logWriter, "Starting deployment helper container...\n")

	// Remove any existing helper container
	_ = o.dockerClient.StopAndRemove(ctx, "schooner-deploy-helper")

	helperID, err := o.dockerClient.RunContainer(ctx, helperConfig)
	if err != nil {
		return fmt.Errorf("failed to start helper container: %w", err)
	}

	fmt.Fprintf(logWriter, "Helper container started: %s\n", helperID[:12])
	fmt.Fprintf(logWriter, "Deployment will proceed in background...\n")

	return nil
}
