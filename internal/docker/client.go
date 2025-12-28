package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

// Client wraps the Docker client with additional functionality
type Client struct {
	cli    *client.Client
	logger *slog.Logger
}

// NewClient creates a new Docker client
func NewClient() (*Client, error) {
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	return &Client{
		cli:    cli,
		logger: slog.Default(),
	}, nil
}

// Close closes the Docker client
func (c *Client) Close() error {
	return c.cli.Close()
}

// Ping checks if Docker is responsive
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.cli.Ping(ctx)
	return err
}

// ContainerConfig holds configuration for running a container
type ContainerConfig struct {
	Name          string
	Image         string
	Cmd           []string
	Env           []string
	Ports         map[string]string // container:host
	Volumes       map[string]string // host:container
	Networks      []string
	NetworkMode   string // e.g., "host", "bridge"
	RestartPolicy string
	Labels        map[string]string
}

// ContainerStatus holds container status information
type ContainerStatus struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	State      string            `json:"state"`
	Status     string            `json:"status"`
	Health     string            `json:"health,omitempty"`
	StartedAt  string            `json:"started_at,omitempty"`
	Ports      map[string]string `json:"ports,omitempty"`
	Image      string            `json:"image"`
	CreatedAt  string            `json:"created_at"`
}

// RunContainer creates and starts a container
func (c *Client) RunContainer(ctx context.Context, cfg ContainerConfig) (string, error) {
	c.logger.Info("running container", "name", cfg.Name, "image", cfg.Image)

	// Ensure image exists
	if err := c.ensureImage(ctx, cfg.Image); err != nil {
		return "", fmt.Errorf("failed to ensure image: %w", err)
	}

	// Stop and remove existing container with same name
	_ = c.StopAndRemove(ctx, cfg.Name)

	// Build container config
	containerConfig := &container.Config{
		Image:  cfg.Image,
		Env:    cfg.Env,
		Labels: cfg.Labels,
	}

	// Build host config
	hostConfig := &container.HostConfig{
		PortBindings: toPortBindings(cfg.Ports),
		Binds:        toBinds(cfg.Volumes),
		RestartPolicy: container.RestartPolicy{
			Name: container.RestartPolicyMode(cfg.RestartPolicy),
		},
	}

	// Build network config
	networkConfig := &network.NetworkingConfig{}
	if len(cfg.Networks) > 0 {
		networkConfig.EndpointsConfig = make(map[string]*network.EndpointSettings)
		for _, net := range cfg.Networks {
			networkConfig.EndpointsConfig[net] = &network.EndpointSettings{}
		}
	}

	// Create container
	resp, err := c.cli.ContainerCreate(ctx, containerConfig, hostConfig, networkConfig, nil, cfg.Name)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	// Start container
	if err := c.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("failed to start container: %w", err)
	}

	c.logger.Info("container started", "id", resp.ID[:12], "name", cfg.Name)
	return resp.ID, nil
}

// StopAndRemove stops and removes a container
func (c *Client) StopAndRemove(ctx context.Context, nameOrID string) error {
	timeout := 30
	stopOptions := container.StopOptions{Timeout: &timeout}

	if err := c.cli.ContainerStop(ctx, nameOrID, stopOptions); err != nil {
		if !client.IsErrNotFound(err) {
			c.logger.Warn("failed to stop container", "name", nameOrID, "error", err)
		}
	}

	if err := c.cli.ContainerRemove(ctx, nameOrID, container.RemoveOptions{Force: true}); err != nil {
		if !client.IsErrNotFound(err) {
			return fmt.Errorf("failed to remove container: %w", err)
		}
	}

	return nil
}

// GetContainerStatus retrieves status of a container
func (c *Client) GetContainerStatus(ctx context.Context, nameOrID string) (*ContainerStatus, error) {
	info, err := c.cli.ContainerInspect(ctx, nameOrID)
	if err != nil {
		if client.IsErrNotFound(err) {
			return &ContainerStatus{Name: nameOrID, State: "not_found"}, nil
		}
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	status := &ContainerStatus{
		ID:        info.ID,
		Name:      info.Name,
		State:     info.State.Status,
		Status:    info.State.Status,
		StartedAt: info.State.StartedAt,
		Image:     info.Config.Image,
		CreatedAt: info.Created,
		Ports:     extractPorts(info.NetworkSettings.Ports),
	}

	if info.State.Health != nil {
		status.Health = info.State.Health.Status
	}

	return status, nil
}

// GetContainerRunArgs returns the docker run arguments needed to recreate a container
func (c *Client) GetContainerRunArgs(ctx context.Context, nameOrID string) ([]string, error) {
	info, err := c.cli.ContainerInspect(ctx, nameOrID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	var args []string

	// Port mappings
	for containerPort, bindings := range info.HostConfig.PortBindings {
		for _, binding := range bindings {
			if binding.HostPort != "" {
				args = append(args, "-p", fmt.Sprintf("%s:%s", binding.HostPort, containerPort.Port()))
			}
		}
	}

	// Volume mounts
	for _, mount := range info.Mounts {
		switch mount.Type {
		case "bind":
			args = append(args, "-v", fmt.Sprintf("%s:%s", mount.Source, mount.Destination))
		case "volume":
			args = append(args, "-v", fmt.Sprintf("%s:%s", mount.Name, mount.Destination))
		}
	}

	// Environment variables
	for _, env := range info.Config.Env {
		args = append(args, "-e", env)
	}

	// Network mode
	if info.HostConfig.NetworkMode != "" && info.HostConfig.NetworkMode != "default" {
		args = append(args, "--network", string(info.HostConfig.NetworkMode))
	}

	// Restart policy
	if info.HostConfig.RestartPolicy.Name != "" {
		args = append(args, "--restart", string(info.HostConfig.RestartPolicy.Name))
	}

	// Labels (preserve existing, skip schooner labels as we'll add our own)
	for k, v := range info.Config.Labels {
		if k != "schooner.managed" && k != "schooner.app" && k != "schooner.app-id" {
			args = append(args, "--label", fmt.Sprintf("%s=%s", k, v))
		}
	}

	return args, nil
}

// ListContainers lists all containers with optional filtering
func (c *Client) ListContainers(ctx context.Context, all bool, filterLabels map[string]string) ([]types.Container, error) {
	filterArgs := filters.NewArgs()
	for k, v := range filterLabels {
		filterArgs.Add("label", fmt.Sprintf("%s=%s", k, v))
	}

	return c.cli.ContainerList(ctx, container.ListOptions{
		All:     all,
		Filters: filterArgs,
	})
}

// GetContainerLogs retrieves container logs
func (c *Client) GetContainerLogs(ctx context.Context, nameOrID string, tail string) (io.ReadCloser, error) {
	return c.cli.ContainerLogs(ctx, nameOrID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       tail,
		Timestamps: true,
	})
}

// ContainerStats holds container resource usage stats
type ContainerStats struct {
	CPUPercent    float64 `json:"cpu_percent"`
	MemoryUsage   uint64  `json:"memory_usage"`
	MemoryLimit   uint64  `json:"memory_limit"`
	MemoryPercent float64 `json:"memory_percent"`
}

// GetContainerStats retrieves container resource usage statistics
func (c *Client) GetContainerStats(ctx context.Context, nameOrID string) (*ContainerStats, error) {
	statsResponse, err := c.cli.ContainerStats(ctx, nameOrID, false)
	if err != nil {
		return nil, err
	}
	defer statsResponse.Body.Close()

	var stats types.StatsJSON
	decoder := json.NewDecoder(statsResponse.Body)
	if err := decoder.Decode(&stats); err != nil {
		return nil, err
	}

	// Calculate CPU percentage
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage - stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemUsage - stats.PreCPUStats.SystemUsage)
	cpuPercent := 0.0
	if systemDelta > 0 && cpuDelta > 0 {
		cpuPercent = (cpuDelta / systemDelta) * float64(len(stats.CPUStats.CPUUsage.PercpuUsage)) * 100.0
	}

	// Calculate memory percentage
	memoryPercent := 0.0
	if stats.MemoryStats.Limit > 0 {
		memoryPercent = float64(stats.MemoryStats.Usage) / float64(stats.MemoryStats.Limit) * 100.0
	}

	return &ContainerStats{
		CPUPercent:    cpuPercent,
		MemoryUsage:   stats.MemoryStats.Usage,
		MemoryLimit:   stats.MemoryStats.Limit,
		MemoryPercent: memoryPercent,
	}, nil
}

// BuildImage builds a Docker image
func (c *Client) BuildImage(ctx context.Context, buildContext io.Reader, options types.ImageBuildOptions) (types.ImageBuildResponse, error) {
	return c.cli.ImageBuild(ctx, buildContext, options)
}

// PullImage pulls a Docker image
func (c *Client) PullImage(ctx context.Context, refStr string) (io.ReadCloser, error) {
	return c.cli.ImagePull(ctx, refStr, image.PullOptions{})
}

// ensureImage ensures an image exists locally
func (c *Client) ensureImage(ctx context.Context, imageName string) error {
	_, _, err := c.cli.ImageInspectWithRaw(ctx, imageName)
	if err == nil {
		return nil
	}

	if !client.IsErrNotFound(err) {
		return err
	}

	// Pull image
	c.logger.Info("pulling image", "image", imageName)
	reader, err := c.cli.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}
	defer reader.Close()

	// Read the pull output to completion
	_, err = io.Copy(io.Discard, reader)
	return err
}

// CleanupOldImages removes old images keeping the specified count
func (c *Client) CleanupOldImages(ctx context.Context, imageName string, keepCount int) error {
	images, err := c.cli.ImageList(ctx, image.ListOptions{
		Filters: filters.NewArgs(filters.Arg("reference", imageName)),
	})
	if err != nil {
		return fmt.Errorf("failed to list images: %w", err)
	}

	if len(images) <= keepCount {
		return nil
	}

	// Sort by creation time (images are already sorted newest first typically)
	// Remove older images
	for i := keepCount; i < len(images); i++ {
		c.logger.Info("removing old image", "id", images[i].ID[:12])
		_, err := c.cli.ImageRemove(ctx, images[i].ID, image.RemoveOptions{
			PruneChildren: true,
		})
		if err != nil {
			c.logger.Warn("failed to remove image", "id", images[i].ID[:12], "error", err)
		}
	}

	return nil
}

// PruneImages removes dangling images
func (c *Client) PruneImages(ctx context.Context) (image.PruneReport, error) {
	return c.cli.ImagesPrune(ctx, filters.NewArgs(filters.Arg("dangling", "true")))
}

// toPortBindings converts port map to Docker port bindings
func toPortBindings(ports map[string]string) nat.PortMap {
	portMap := nat.PortMap{}
	for containerPort, hostPort := range ports {
		port := nat.Port(containerPort + "/tcp")
		portMap[port] = []nat.PortBinding{
			{HostPort: hostPort},
		}
	}
	return portMap
}

// toBinds converts volume map to bind mounts
func toBinds(volumes map[string]string) []string {
	var binds []string
	for hostPath, containerPath := range volumes {
		binds = append(binds, fmt.Sprintf("%s:%s", hostPath, containerPath))
	}
	return binds
}

// extractPorts extracts port mappings from network settings
func extractPorts(portMap nat.PortMap) map[string]string {
	ports := make(map[string]string)
	for containerPort, bindings := range portMap {
		if len(bindings) > 0 {
			ports[string(containerPort)] = bindings[0].HostPort
		}
	}
	return ports
}

// WaitForContainer waits for a container to be in a specific state
func (c *Client) WaitForContainer(ctx context.Context, nameOrID string, condition container.WaitCondition) error {
	statusCh, errCh := c.cli.ContainerWait(ctx, nameOrID, condition)
	select {
	case err := <-errCh:
		return err
	case <-statusCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// StartContainer starts a stopped container
func (c *Client) StartContainer(ctx context.Context, nameOrID string) error {
	return c.cli.ContainerStart(ctx, nameOrID, container.StartOptions{})
}

// StopContainer stops a running container
func (c *Client) StopContainer(ctx context.Context, nameOrID string, timeout time.Duration) error {
	timeoutSecs := int(timeout.Seconds())
	return c.cli.ContainerStop(ctx, nameOrID, container.StopOptions{Timeout: &timeoutSecs})
}

// RestartContainer restarts a container
func (c *Client) RestartContainer(ctx context.Context, nameOrID string, timeout time.Duration) error {
	timeoutSecs := int(timeout.Seconds())
	return c.cli.ContainerRestart(ctx, nameOrID, container.StopOptions{Timeout: &timeoutSecs})
}

// RemoveContainer removes a container
func (c *Client) RemoveContainer(ctx context.Context, nameOrID string) error {
	return c.cli.ContainerRemove(ctx, nameOrID, container.RemoveOptions{Force: true})
}

// EnsureNetwork creates a network if it doesn't exist
func (c *Client) EnsureNetwork(ctx context.Context, name string) error {
	networks, err := c.cli.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	if err != nil {
		return fmt.Errorf("failed to list networks: %w", err)
	}

	// Check if network already exists (exact match)
	for _, net := range networks {
		if net.Name == name {
			c.logger.Debug("network already exists", "name", name)
			return nil
		}
	}

	// Create network
	c.logger.Info("creating network", "name", name)
	_, err = c.cli.NetworkCreate(ctx, name, network.CreateOptions{
		Driver: "bridge",
		Labels: map[string]string{
			"schooner.managed": "true",
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}

	return nil
}

// ConnectToNetwork connects a container to a network
func (c *Client) ConnectToNetwork(ctx context.Context, containerID, networkName string) error {
	return c.cli.NetworkConnect(ctx, networkName, containerID, nil)
}

// CreateAndStartContainer creates and starts a container with full config
func (c *Client) CreateAndStartContainer(ctx context.Context, cfg ContainerConfig) (string, error) {
	c.logger.Info("creating container", "name", cfg.Name, "image", cfg.Image)

	// Ensure image exists
	if err := c.ensureImage(ctx, cfg.Image); err != nil {
		return "", fmt.Errorf("failed to ensure image: %w", err)
	}

	// Build container config
	containerConfig := &container.Config{
		Image:  cfg.Image,
		Cmd:    cfg.Cmd,
		Env:    cfg.Env,
		Labels: cfg.Labels,
	}

	// Build host config
	hostConfig := &container.HostConfig{
		PortBindings: toPortBindings(cfg.Ports),
		Binds:        toBinds(cfg.Volumes),
		RestartPolicy: container.RestartPolicy{
			Name: container.RestartPolicyMode(cfg.RestartPolicy),
		},
	}

	if cfg.NetworkMode != "" {
		hostConfig.NetworkMode = container.NetworkMode(cfg.NetworkMode)
	}

	// Build network config
	networkConfig := &network.NetworkingConfig{}
	if len(cfg.Networks) > 0 && cfg.NetworkMode == "" {
		networkConfig.EndpointsConfig = make(map[string]*network.EndpointSettings)
		for _, net := range cfg.Networks {
			networkConfig.EndpointsConfig[net] = &network.EndpointSettings{}
		}
	}

	// Create container
	resp, err := c.cli.ContainerCreate(ctx, containerConfig, hostConfig, networkConfig, nil, cfg.Name)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	// Start container
	if err := c.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("failed to start container: %w", err)
	}

	c.logger.Info("container started", "id", resp.ID[:12], "name", cfg.Name)
	return resp.ID, nil
}
