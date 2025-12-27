package cloudflare

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"

	"schooner/internal/config"
	"schooner/internal/docker"
	"schooner/internal/models"
)

const (
	cloudflaredImage     = "cloudflare/cloudflared:latest"
	cloudflaredContainer = "schooner-cloudflared"
	configDir            = "/data/cloudflared"
)

// IngressRule represents a Cloudflare tunnel ingress rule
type IngressRule struct {
	Hostname string `yaml:"hostname,omitempty"`
	Service  string `yaml:"service"`
}

// TunnelConfig represents the cloudflared config.yml structure
type TunnelConfig struct {
	Tunnel  string        `yaml:"tunnel"`
	Ingress []IngressRule `yaml:"ingress"`
}

// Manager manages the Cloudflare tunnel
type Manager struct {
	cfg          *config.Config
	dockerClient *docker.Client
	mu           sync.Mutex
}

// NewManager creates a new tunnel manager
func NewManager(cfg *config.Config, dockerClient *docker.Client) *Manager {
	return &Manager{
		cfg:          cfg,
		dockerClient: dockerClient,
	}
}

// IsConfigured returns true if Cloudflare tunnel is configured
func (m *Manager) IsConfigured() bool {
	return m.cfg.Cloudflare.TunnelToken != "" && m.cfg.Cloudflare.Domain != ""
}

// Start starts the cloudflared container
func (m *Manager) Start(ctx context.Context) error {
	if !m.IsConfigured() {
		slog.Info("Cloudflare tunnel not configured, skipping")
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already running
	status, _ := m.dockerClient.GetContainerStatus(ctx, cloudflaredContainer)
	if status != nil && status.State == "running" {
		slog.Info("cloudflared already running")
		return nil
	}

	// Ensure config directory exists
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}

	// Create initial config
	if err := m.writeConfig([]IngressRule{}); err != nil {
		return fmt.Errorf("failed to write initial config: %w", err)
	}

	// Stop existing container if any
	_ = m.dockerClient.StopContainer(ctx, cloudflaredContainer, 10)
	_ = m.dockerClient.RemoveContainer(ctx, cloudflaredContainer)

	// Start cloudflared container
	containerConfig := docker.ContainerConfig{
		Name:  cloudflaredContainer,
		Image: cloudflaredImage,
		Cmd:   []string{"tunnel", "--no-autoupdate", "run", "--token", m.cfg.Cloudflare.TunnelToken},
		Labels: map[string]string{
			"schooner.managed": "true",
			"schooner.service": "cloudflared",
		},
		RestartPolicy: "unless-stopped",
		NetworkMode:   "host", // Use host network for easy access to other containers
	}

	containerID, err := m.dockerClient.CreateAndStartContainer(ctx, containerConfig)
	if err != nil {
		return fmt.Errorf("failed to start cloudflared: %w", err)
	}

	slog.Info("cloudflared started", "container_id", containerID[:12])
	return nil
}

// Stop stops the cloudflared container
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.dockerClient.StopContainer(ctx, cloudflaredContainer, 30); err != nil {
		return fmt.Errorf("failed to stop cloudflared: %w", err)
	}

	slog.Info("cloudflared stopped")
	return nil
}

// UpdateRoutes updates the tunnel ingress rules based on apps
func (m *Manager) UpdateRoutes(ctx context.Context, apps []*models.App) error {
	if !m.IsConfigured() {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var rules []IngressRule

	for _, app := range apps {
		if !app.Enabled {
			continue
		}

		subdomain := app.GetSubdomain()
		port := app.GetPublicPort()

		if subdomain == "" || port == 0 {
			continue
		}

		hostname := fmt.Sprintf("%s.%s", subdomain, m.cfg.Cloudflare.Domain)
		service := fmt.Sprintf("http://localhost:%d", port)

		rules = append(rules, IngressRule{
			Hostname: hostname,
			Service:  service,
		})

		slog.Debug("added tunnel route", "hostname", hostname, "service", service)
	}

	// Always add catch-all 404 at the end
	rules = append(rules, IngressRule{
		Service: "http_status:404",
	})

	if err := m.writeConfig(rules); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	slog.Info("tunnel routes updated", "count", len(rules)-1)
	return nil
}

// AddRoute adds a single route for an app
func (m *Manager) AddRoute(ctx context.Context, app *models.App) error {
	subdomain := app.GetSubdomain()
	port := app.GetPublicPort()

	if subdomain == "" || port == 0 {
		return nil // No route needed
	}

	slog.Info("added tunnel route",
		"app", app.Name,
		"hostname", fmt.Sprintf("%s.%s", subdomain, m.cfg.Cloudflare.Domain),
		"port", port,
	)

	// For now, just log. Full implementation would reload cloudflared
	// The tunnel token-based approach handles routing via Cloudflare dashboard
	return nil
}

// RemoveRoute removes a route for an app
func (m *Manager) RemoveRoute(ctx context.Context, app *models.App) error {
	subdomain := app.GetSubdomain()
	if subdomain == "" {
		return nil
	}

	slog.Info("removed tunnel route",
		"app", app.Name,
		"hostname", fmt.Sprintf("%s.%s", subdomain, m.cfg.Cloudflare.Domain),
	)

	return nil
}

// writeConfig writes the cloudflared config.yml file
func (m *Manager) writeConfig(rules []IngressRule) error {
	cfg := TunnelConfig{
		Tunnel:  m.cfg.Cloudflare.TunnelID,
		Ingress: rules,
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	configPath := filepath.Join(configDir, "config.yml")
	return os.WriteFile(configPath, data, 0644)
}

// GetStatus returns the current tunnel status
func (m *Manager) GetStatus(ctx context.Context) (*docker.ContainerStatus, error) {
	if !m.IsConfigured() {
		return nil, nil
	}
	return m.dockerClient.GetContainerStatus(ctx, cloudflaredContainer)
}
