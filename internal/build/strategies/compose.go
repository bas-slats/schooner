package strategies

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"schooner/internal/build"
	"schooner/internal/docker"
	"schooner/internal/models"
)

const schoonerOverrideFile = ".schooner.compose.override.yml"

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

// composeFileNames is the list of compose file names to check in order
var composeFileNames = []string{
	"docker-compose.yml",
	"docker-compose.yaml",
	"compose.yml",
	"compose.yaml",
}

// FindComposeFile finds the compose file in the repo, checking configured name first.
// Returns empty string if not found or if path validation fails.
func FindComposeFile(repoPath, configuredFile string) string {
	// Try configured file first
	if configuredFile != "" {
		// Validate path to prevent traversal
		composePath, err := build.SafePath(repoPath, configuredFile)
		if err == nil {
			if _, err := os.Stat(composePath); err == nil {
				return configuredFile
			}
		}
	}

	// Try common names (these are safe, hardcoded values)
	for _, name := range composeFileNames {
		composePath := filepath.Join(repoPath, name)
		if _, err := os.Stat(composePath); err == nil {
			return name
		}
	}

	return ""
}

// Validate checks if the strategy can be used
func (s *ComposeStrategy) Validate(ctx context.Context, opts build.BuildOptions) error {
	composeFile := FindComposeFile(opts.RepoPath, opts.ComposeFile)
	if composeFile == "" {
		return fmt.Errorf("compose file not found in %s (tried: %s and common names)", opts.RepoPath, opts.ComposeFile)
	}
	return nil
}

// Build executes the build using docker compose
func (s *ComposeStrategy) Build(ctx context.Context, opts build.BuildOptions) (*build.BuildResult, error) {
	composeFile := FindComposeFile(opts.RepoPath, opts.ComposeFile)
	if composeFile == "" {
		return nil, fmt.Errorf("compose file not found")
	}
	composePath := filepath.Join(opts.RepoPath, composeFile)

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
	return s.upWithOptions(ctx, opts, false)
}

// UpSelfDeploy brings up compose services for self-deployment (fire and forget)
func (s *ComposeStrategy) UpSelfDeploy(ctx context.Context, opts build.BuildOptions) error {
	return s.upWithOptions(ctx, opts, true)
}

func (s *ComposeStrategy) upWithOptions(ctx context.Context, opts build.BuildOptions, selfDeploy bool) error {
	composeFile := FindComposeFile(opts.RepoPath, opts.ComposeFile)
	if composeFile == "" {
		return fmt.Errorf("compose file not found")
	}
	composePath := filepath.Join(opts.RepoPath, composeFile)

	if selfDeploy {
		fmt.Fprintf(opts.LogWriter, "Self-deployment: Starting services with Docker Compose (fire and forget)\n")
	} else {
		fmt.Fprintf(opts.LogWriter, "Starting services with Docker Compose\n")
	}

	// Generate override file with schooner labels
	overridePath, err := generateLabelOverride(composePath, opts)
	if err != nil {
		fmt.Fprintf(opts.LogWriter, "Warning: failed to generate label override: %v\n", err)
	} else {
		fmt.Fprintf(opts.LogWriter, "Generated label override file for container tracking\n")
	}

	// Write .env file with app's environment variables
	if len(opts.EnvVars) > 0 {
		envFilePath := filepath.Join(opts.RepoPath, ".env")
		if err := writeEnvFile(envFilePath, opts.EnvVars); err != nil {
			fmt.Fprintf(opts.LogWriter, "Warning: failed to write .env file: %v\n", err)
		} else {
			fmt.Fprintf(opts.LogWriter, "Wrote %d environment variables to .env\n", len(opts.EnvVars))
		}
	}

	// Build environment
	env := os.Environ()
	for k, v := range opts.EnvVars {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Build command args with both compose files
	args := []string{"compose", "-f", composePath}
	if overridePath != "" {
		args = append(args, "-f", overridePath)
	}
	args = append(args, "up", "-d", "--force-recreate", "--remove-orphans")
	if !selfDeploy {
		args = append(args, "--wait")
	}

	if selfDeploy {
		// For self-deploy, we need to truly detach the process so it survives
		// after our container is stopped. We use a helper container that runs
		// docker compose, ensuring the command completes even if we're killed.
		fmt.Fprintf(opts.LogWriter, "Starting self-deploy via helper container...\n")

		// Build the helper script with override file
		composeCmd := fmt.Sprintf("docker compose -f %s", composePath)
		if overridePath != "" {
			composeCmd += fmt.Sprintf(" -f %s", overridePath)
		}
		composeCmd += " up -d --force-recreate --remove-orphans"

		script := fmt.Sprintf(`
			cd %s
			sleep 3
			%s
			echo "Self-deploy complete"
		`, opts.RepoPath, composeCmd)

		// Run helper container with docker socket mounted
		helperCmd := exec.Command("docker", "run", "-d", "--rm",
			"--name", "schooner-compose-helper",
			"-v", "/var/run/docker.sock:/var/run/docker.sock",
			"-v", opts.RepoPath+":"+opts.RepoPath,
			"-w", opts.RepoPath,
			"docker:cli",
			"sh", "-c", script)
		helperCmd.Env = env

		output, err := helperCmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to start helper container: %w, output: %s", err, string(output))
		}

		fmt.Fprintf(opts.LogWriter, "Helper container started, container will be replaced in ~3 seconds...\n")
		return nil
	}

	// Normal (non-self-deploy) path
	upCmd := exec.CommandContext(ctx, "docker", args...)
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

// writeEnvFile writes environment variables to a .env file
func writeEnvFile(path string, envVars map[string]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	for k, v := range envVars {
		// Escape values that contain special characters
		if strings.ContainsAny(v, " \t\n\"'$`\\") {
			v = "\"" + strings.ReplaceAll(v, "\"", "\\\"") + "\""
		}
		if _, err := fmt.Fprintf(f, "%s=%s\n", k, v); err != nil {
			return err
		}
	}
	return nil
}

// generateLabelOverride creates an override file that adds schooner labels to all services
func generateLabelOverride(composePath string, opts build.BuildOptions) (string, error) {
	// Read the original compose file
	data, err := os.ReadFile(composePath)
	if err != nil {
		return "", fmt.Errorf("failed to read compose file: %w", err)
	}

	// Parse to extract service names
	var compose map[string]interface{}
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return "", fmt.Errorf("failed to parse compose file: %w", err)
	}

	services, ok := compose["services"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("no services found in compose file")
	}

	// Build override structure with labels for each service
	labels := map[string]string{
		"schooner.managed": "true",
		"schooner.app":     opts.AppName,
		"schooner.app-id":  opts.AppID,
	}
	if opts.BuildID != "" {
		labels["schooner.build-id"] = opts.BuildID
	}

	overrideServices := make(map[string]interface{})
	for serviceName := range services {
		overrideServices[serviceName] = map[string]interface{}{
			"labels": labels,
		}
	}

	override := map[string]interface{}{
		"services": overrideServices,
	}

	// Write override file
	overrideData, err := yaml.Marshal(override)
	if err != nil {
		return "", fmt.Errorf("failed to marshal override: %w", err)
	}

	overridePath := filepath.Join(filepath.Dir(composePath), schoonerOverrideFile)
	if err := os.WriteFile(overridePath, overrideData, 0644); err != nil {
		return "", fmt.Errorf("failed to write override file: %w", err)
	}

	return overridePath, nil
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
