package strategies

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/pkg/archive"

	"schooner/internal/build"
	"schooner/internal/docker"
	"schooner/internal/models"
)

// DockerfileStrategy builds images using a Dockerfile
type DockerfileStrategy struct {
	dockerClient *docker.Client
}

// NewDockerfileStrategy creates a new Dockerfile build strategy
func NewDockerfileStrategy(dockerClient *docker.Client) *DockerfileStrategy {
	return &DockerfileStrategy{
		dockerClient: dockerClient,
	}
}

// Name returns the strategy name
func (s *DockerfileStrategy) Name() models.BuildStrategy {
	return models.BuildStrategyDockerfile
}

// Validate checks if the strategy can be used
func (s *DockerfileStrategy) Validate(ctx context.Context, opts build.BuildOptions) error {
	dockerfilePath := filepath.Join(opts.RepoPath, opts.BuildContext, opts.Dockerfile)
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		return fmt.Errorf("Dockerfile not found: %s", dockerfilePath)
	}
	return nil
}

// Build executes the build
func (s *DockerfileStrategy) Build(ctx context.Context, opts build.BuildOptions) (*build.BuildResult, error) {
	// Determine build context path
	contextPath := filepath.Join(opts.RepoPath, opts.BuildContext)

	// Create tar archive of build context
	fmt.Fprintf(opts.LogWriter, "Creating build context from %s\n", contextPath)

	buildContext, err := archive.TarWithOptions(contextPath, &archive.TarOptions{
		ExcludePatterns: []string{".git", "node_modules", ".env*", "*.log"},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create build context: %w", err)
	}
	defer buildContext.Close()

	// Prepare image tag
	imageTag := fmt.Sprintf("%s:%s", opts.ImageName, opts.Tag)

	fmt.Fprintf(opts.LogWriter, "Building image: %s\n", imageTag)
	fmt.Fprintf(opts.LogWriter, "Dockerfile: %s\n", opts.Dockerfile)

	// Prepare build args
	buildArgs := make(map[string]*string)
	for k, v := range opts.BuildArgs {
		val := v
		buildArgs[k] = &val
	}

	// Build options
	buildOpts := types.ImageBuildOptions{
		Tags:       []string{imageTag},
		Dockerfile: opts.Dockerfile,
		Remove:     true,
		BuildArgs:  buildArgs,
		Labels: map[string]string{
			"schooner.app":    opts.AppName,
			"schooner.app-id": opts.AppID,
		},
	}

	// Execute build
	resp, err := s.dockerClient.BuildImage(ctx, buildContext, buildOpts)
	if err != nil {
		return nil, fmt.Errorf("docker build failed: %w", err)
	}
	defer resp.Body.Close()

	// Stream build output
	imageID, err := streamBuildOutput(resp.Body, opts.LogWriter)
	if err != nil {
		return nil, err
	}

	fmt.Fprintf(opts.LogWriter, "\nBuild complete: %s\n", imageTag)

	return &build.BuildResult{
		ImageID:  imageID,
		ImageTag: imageTag,
	}, nil
}

// streamBuildOutput streams Docker build output and extracts the image ID
func streamBuildOutput(reader io.Reader, writer io.Writer) (string, error) {
	var imageID string
	scanner := bufio.NewScanner(reader)

	// Increase scanner buffer for large output lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()

		var msg struct {
			Stream      string `json:"stream"`
			Error       string `json:"error"`
			ErrorDetail struct {
				Message string `json:"message"`
			} `json:"errorDetail"`
			Aux struct {
				ID string `json:"ID"`
			} `json:"aux"`
		}

		if err := json.Unmarshal(line, &msg); err != nil {
			// Not JSON, write raw line
			writer.Write(line)
			writer.Write([]byte("\n"))
			continue
		}

		if msg.Error != "" {
			errMsg := msg.Error
			if msg.ErrorDetail.Message != "" {
				errMsg = msg.ErrorDetail.Message
			}
			return "", fmt.Errorf("build error: %s", errMsg)
		}

		if msg.Stream != "" {
			writer.Write([]byte(msg.Stream))
		}

		if msg.Aux.ID != "" {
			imageID = msg.Aux.ID
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error reading build output: %w", err)
	}

	return imageID, nil
}
