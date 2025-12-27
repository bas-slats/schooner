package strategies

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"

	"homelab-cd/internal/build"
	"homelab-cd/internal/docker"
	"homelab-cd/internal/models"
)

// DefaultBuilder is the default Cloud Native Buildpacks builder
const DefaultBuilder = "paketobuildpacks/builder:base"

// BuildpacksStrategy builds using Cloud Native Buildpacks
type BuildpacksStrategy struct {
	dockerClient *docker.Client
	builder      string
}

// NewBuildpacksStrategy creates a new Buildpacks build strategy
func NewBuildpacksStrategy(dockerClient *docker.Client, builder string) *BuildpacksStrategy {
	if builder == "" {
		builder = DefaultBuilder
	}
	return &BuildpacksStrategy{
		dockerClient: dockerClient,
		builder:      builder,
	}
}

// Name returns the strategy name
func (s *BuildpacksStrategy) Name() models.BuildStrategy {
	return models.BuildStrategyBuildpacks
}

// Validate checks if the strategy can be used
func (s *BuildpacksStrategy) Validate(ctx context.Context, opts build.BuildOptions) error {
	// Check if pack CLI is available
	_, err := exec.LookPath("pack")
	if err != nil {
		return fmt.Errorf("pack CLI not found - install from https://buildpacks.io/docs/tools/pack/")
	}
	return nil
}

// Build executes the build using pack CLI
func (s *BuildpacksStrategy) Build(ctx context.Context, opts build.BuildOptions) (*build.BuildResult, error) {
	imageTag := fmt.Sprintf("%s:%s", opts.ImageName, opts.Tag)

	fmt.Fprintf(opts.LogWriter, "Building with Cloud Native Buildpacks\n")
	fmt.Fprintf(opts.LogWriter, "Builder: %s\n", s.builder)
	fmt.Fprintf(opts.LogWriter, "Image: %s\n", imageTag)

	// Build pack command
	args := []string{
		"build", imageTag,
		"--builder", s.builder,
		"--path", opts.RepoPath,
	}

	// Add environment variables as build-env
	for k, v := range opts.EnvVars {
		args = append(args, "--env", fmt.Sprintf("%s=%s", k, v))
	}

	cmd := exec.CommandContext(ctx, "pack", args...)

	// Stream output
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start pack build: %w", err)
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

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("pack build failed: %w", err)
	}

	fmt.Fprintf(opts.LogWriter, "\nBuildpacks build complete: %s\n", imageTag)

	return &build.BuildResult{
		ImageTag: imageTag,
	}, nil
}
