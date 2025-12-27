package deploy

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"schooner/internal/docker"
	"schooner/internal/models"
)

// Deployer handles container deployment
type Deployer struct {
	dockerClient *docker.Client
	logger       *slog.Logger
}

// NewDeployer creates a new deployer
func NewDeployer(dockerClient *docker.Client) *Deployer {
	return &Deployer{
		dockerClient: dockerClient,
		logger:       slog.Default(),
	}
}

// DeployOptions contains deployment configuration
type DeployOptions struct {
	ContainerName string
	ImageTag      string
	Ports         map[string]string
	Volumes       map[string]string
	EnvVars       map[string]string
	Networks      []string
	Labels        map[string]string
	RestartPolicy string
}

// Deploy deploys a container
func (d *Deployer) Deploy(ctx context.Context, opts DeployOptions) (string, error) {
	d.logger.Info("deploying container", "name", opts.ContainerName, "image", opts.ImageTag)

	// Set defaults
	if opts.RestartPolicy == "" {
		opts.RestartPolicy = "unless-stopped"
	}

	// Build environment slice
	var env []string
	for k, v := range opts.EnvVars {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	config := docker.ContainerConfig{
		Name:          opts.ContainerName,
		Image:         opts.ImageTag,
		Env:           env,
		Ports:         opts.Ports,
		Volumes:       opts.Volumes,
		Networks:      opts.Networks,
		Labels:        opts.Labels,
		RestartPolicy: opts.RestartPolicy,
	}

	containerID, err := d.dockerClient.RunContainer(ctx, config)
	if err != nil {
		return "", fmt.Errorf("failed to run container: %w", err)
	}

	d.logger.Info("container deployed", "id", containerID[:12], "name", opts.ContainerName)
	return containerID, nil
}

// Stop stops a container
func (d *Deployer) Stop(ctx context.Context, nameOrID string) error {
	d.logger.Info("stopping container", "container", nameOrID)
	return d.dockerClient.StopContainer(ctx, nameOrID, 30*time.Second)
}

// Start starts a stopped container
func (d *Deployer) Start(ctx context.Context, nameOrID string) error {
	d.logger.Info("starting container", "container", nameOrID)
	return d.dockerClient.StartContainer(ctx, nameOrID)
}

// Restart restarts a container
func (d *Deployer) Restart(ctx context.Context, nameOrID string) error {
	d.logger.Info("restarting container", "container", nameOrID)
	return d.dockerClient.RestartContainer(ctx, nameOrID, 30*time.Second)
}

// Remove stops and removes a container
func (d *Deployer) Remove(ctx context.Context, nameOrID string) error {
	d.logger.Info("removing container", "container", nameOrID)
	return d.dockerClient.StopAndRemove(ctx, nameOrID)
}

// GetStatus gets container status
func (d *Deployer) GetStatus(ctx context.Context, nameOrID string) (*docker.ContainerStatus, error) {
	return d.dockerClient.GetContainerStatus(ctx, nameOrID)
}

// Rollback rolls back to a previous deployment
func (d *Deployer) Rollback(ctx context.Context, app *models.App, targetBuild *models.Build) error {
	if !targetBuild.ImageTag.Valid {
		return fmt.Errorf("target build has no image tag")
	}

	d.logger.Info("rolling back",
		"app", app.Name,
		"targetBuild", targetBuild.ID[:8],
		"imageTag", targetBuild.ImageTag.String,
	)

	// Deploy the old image
	opts := DeployOptions{
		ContainerName: app.GetContainerName(),
		ImageTag:      targetBuild.ImageTag.String,
		EnvVars:       app.EnvVars,
		Labels: map[string]string{
			"schooner.managed":  "true",
			"schooner.app":      app.Name,
			"schooner.app-id":   app.ID,
			"schooner.build-id": targetBuild.ID,
			"schooner.rollback": "true",
		},
	}

	_, err := d.Deploy(ctx, opts)
	return err
}

// HealthCheck checks if a container is healthy
func (d *Deployer) HealthCheck(ctx context.Context, nameOrID string) (bool, error) {
	status, err := d.dockerClient.GetContainerStatus(ctx, nameOrID)
	if err != nil {
		return false, err
	}

	if status.State == "not_found" {
		return false, nil
	}

	if status.State != "running" {
		return false, nil
	}

	// If container has health check, check health status
	if status.Health != "" {
		return status.Health == "healthy", nil
	}

	// No health check, assume healthy if running
	return true, nil
}
