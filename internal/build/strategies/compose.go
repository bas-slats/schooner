package strategies

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"schooner/internal/build"
	"schooner/internal/docker"
	"schooner/internal/models"
)

// ComposeStrategy builds using Docker Compose
type ComposeStrategy struct {
	dockerClient *docker.Client
}

// NewComposeStrategy creates a new Docker Compose build strategy
func NewComposeStrategy(dockerClient *docker.Client) *ComposeStrategy {
	return &ComposeStrategy{
		dockerClient: dockerClient,
	}
}

// Name returns the strategy name
func (s *ComposeStrategy) Name() models.BuildStrategy {
	return models.BuildStrategyCompose
}

// Validate checks if the strategy can be used
func (s *ComposeStrategy) Validate(ctx context.Context, opts build.BuildOptions) error {
	composePath := filepath.Join(opts.RepoPath, opts.ComposeFile)
	if _, err := os.Stat(composePath); os.IsNotExist(err) {
		return fmt.Errorf("compose file not found: %s", composePath)
	}
	return nil
}

// Build executes the build using docker compose
func (s *ComposeStrategy) Build(ctx context.Context, opts build.BuildOptions) (*build.BuildResult, error) {
	composePath := filepath.Join(opts.RepoPath, opts.ComposeFile)

	fmt.Fprintf(opts.LogWriter, "Building with Docker Compose: %s\n", composePath)

	// Build environment
	env := os.Environ()
	for k, v := range opts.EnvVars {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Run docker compose build
	buildCmd := exec.CommandContext(ctx, "docker", "compose",
		"-f", composePath,
		"build",
		"--pull",
	)
	buildCmd.Dir = opts.RepoPath
	buildCmd.Env = env

	// Stream output
	stdout, err := buildCmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	stderr, err := buildCmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	if err := buildCmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start docker compose build: %w", err)
	}

	// Stream stdout
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			fmt.Fprintf(opts.LogWriter, "%s\n", scanner.Text())
		}
	}()

	// Stream stderr
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			fmt.Fprintf(opts.LogWriter, "%s\n", scanner.Text())
		}
	}()

	if err := buildCmd.Wait(); err != nil {
		return nil, fmt.Errorf("docker compose build failed: %w", err)
	}

	fmt.Fprintf(opts.LogWriter, "\nDocker Compose build complete\n")

	// Return the compose project name as the "image tag"
	// The actual images are managed by compose
	return &build.BuildResult{
		ImageTag: opts.AppName,
	}, nil
}

// Up brings up the compose services
func (s *ComposeStrategy) Up(ctx context.Context, opts build.BuildOptions) error {
	composePath := filepath.Join(opts.RepoPath, opts.ComposeFile)

	fmt.Fprintf(opts.LogWriter, "Starting services with Docker Compose\n")

	// Build environment
	env := os.Environ()
	for k, v := range opts.EnvVars {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Run docker compose up
	upCmd := exec.CommandContext(ctx, "docker", "compose",
		"-f", composePath,
		"up",
		"-d",
		"--remove-orphans",
	)
	upCmd.Dir = opts.RepoPath
	upCmd.Env = env

	// Stream output
	stdout, _ := upCmd.StdoutPipe()
	stderr, _ := upCmd.StderrPipe()

	if err := upCmd.Start(); err != nil {
		return fmt.Errorf("failed to start docker compose up: %w", err)
	}

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			fmt.Fprintf(opts.LogWriter, "%s\n", scanner.Text())
		}
	}()

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			fmt.Fprintf(opts.LogWriter, "%s\n", scanner.Text())
		}
	}()

	if err := upCmd.Wait(); err != nil {
		return fmt.Errorf("docker compose up failed: %w", err)
	}

	fmt.Fprintf(opts.LogWriter, "Services started\n")
	return nil
}

// Down stops the compose services
func (s *ComposeStrategy) Down(ctx context.Context, repoPath, composeFile string) error {
	composePath := filepath.Join(repoPath, composeFile)

	cmd := exec.CommandContext(ctx, "docker", "compose",
		"-f", composePath,
		"down",
	)
	cmd.Dir = repoPath

	return cmd.Run()
}
