package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"schooner/internal/config"
	"schooner/internal/docker"
)

const (
	lokiImage       = "grafana/loki:2.9.0"
	grafanaImage    = "grafana/grafana:10.2.0"
	promtailImage   = "grafana/promtail:2.9.0"

	lokiContainer      = "schooner-loki"
	grafanaContainer   = "schooner-grafana"
	promtailContainer  = "schooner-promtail"

	observabilityNetwork = "schooner-observability"
	defaultDataDir       = "/data/observability"
	defaultGrafanaPort   = 3000
	defaultLokiRetention = "168h"
)

// SettingsGetter interface for getting settings from the database
type SettingsGetter interface {
	Get(ctx context.Context, key string) (string, error)
}

// StackStatus represents the status of the observability stack
type StackStatus struct {
	Enabled       bool                   `json:"enabled"`
	LokiStatus    *docker.ContainerStatus `json:"loki_status,omitempty"`
	PromtailStatus *docker.ContainerStatus `json:"promtail_status,omitempty"`
	GrafanaStatus *docker.ContainerStatus `json:"grafana_status,omitempty"`
	GrafanaURL    string                  `json:"grafana_url,omitempty"`
}

// Manager manages the observability stack (Loki, Promtail, Grafana)
type Manager struct {
	cfg             *config.Config
	dockerClient    *docker.Client
	settingsQueries SettingsGetter
	mu              sync.Mutex
}

// NewManager creates a new observability manager
func NewManager(cfg *config.Config, dockerClient *docker.Client) *Manager {
	return &Manager{
		cfg:          cfg,
		dockerClient: dockerClient,
	}
}

// SetSettingsQueries sets the settings queries for database-driven config
func (m *Manager) SetSettingsQueries(sq SettingsGetter) {
	m.settingsQueries = sq
}

// getConfig loads observability configuration from database or config file
func (m *Manager) getConfig(ctx context.Context) (enabled bool, grafanaPort int, lokiRetention, dataDir string) {
	grafanaPort = defaultGrafanaPort
	lokiRetention = defaultLokiRetention
	dataDir = defaultDataDir

	// Try database first
	if m.settingsQueries != nil {
		if e, err := m.settingsQueries.Get(ctx, "observability_enabled"); err == nil && e == "true" {
			enabled = true
		}
		if p, err := m.settingsQueries.Get(ctx, "observability_grafana_port"); err == nil && p != "" {
			fmt.Sscanf(p, "%d", &grafanaPort)
		}
		if r, err := m.settingsQueries.Get(ctx, "observability_loki_retention"); err == nil && r != "" {
			lokiRetention = r
		}
		if d, err := m.settingsQueries.Get(ctx, "observability_data_dir"); err == nil && d != "" {
			dataDir = d
		}
	}

	// Fall back to config file if not set in database
	if m.cfg.Observability.GrafanaPort > 0 && grafanaPort == defaultGrafanaPort {
		grafanaPort = m.cfg.Observability.GrafanaPort
	}
	if m.cfg.Observability.LokiRetention != "" && lokiRetention == defaultLokiRetention {
		lokiRetention = m.cfg.Observability.LokiRetention
	}
	if m.cfg.Observability.DataDir != "" && dataDir == defaultDataDir {
		dataDir = m.cfg.Observability.DataDir
	}
	if m.cfg.Observability.Enabled && !enabled {
		enabled = m.cfg.Observability.Enabled
	}

	return
}

// IsEnabled returns true if observability is enabled
func (m *Manager) IsEnabled(ctx context.Context) bool {
	enabled, _, _, _ := m.getConfig(ctx)
	return enabled
}

// Start starts the observability stack (Loki, Promtail, Grafana)
func (m *Manager) Start(ctx context.Context) error {
	enabled, grafanaPort, lokiRetention, dataDir := m.getConfig(ctx)

	if !enabled {
		return fmt.Errorf("observability is not enabled")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	slog.Info("starting observability stack", "grafana_port", grafanaPort, "retention", lokiRetention)

	// Ensure data directories exist
	dirs := []string{
		filepath.Join(dataDir, "loki"),
		filepath.Join(dataDir, "grafana"),
		filepath.Join(dataDir, "promtail"),
		filepath.Join(dataDir, "grafana-provisioning", "datasources"),
		filepath.Join(dataDir, "grafana-provisioning", "dashboards"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Ensure network exists
	if err := m.dockerClient.EnsureNetwork(ctx, observabilityNetwork); err != nil {
		return fmt.Errorf("failed to ensure network: %w", err)
	}

	// Write configuration files
	if err := m.writeConfigs(dataDir, lokiRetention); err != nil {
		return fmt.Errorf("failed to write configs: %w", err)
	}

	// Start Loki
	if err := m.startLoki(ctx, dataDir); err != nil {
		return fmt.Errorf("failed to start Loki: %w", err)
	}

	// Wait for Loki to be ready
	if err := m.waitForLoki(ctx); err != nil {
		slog.Warn("Loki may not be fully ready", "error", err)
	}

	// Start Promtail
	if err := m.startPromtail(ctx, dataDir); err != nil {
		return fmt.Errorf("failed to start Promtail: %w", err)
	}

	// Start Grafana
	if err := m.startGrafana(ctx, dataDir, grafanaPort); err != nil {
		return fmt.Errorf("failed to start Grafana: %w", err)
	}

	slog.Info("observability stack started successfully")
	return nil
}

// startLoki starts the Loki container
func (m *Manager) startLoki(ctx context.Context, dataDir string) error {
	// Stop existing container if any
	_ = m.dockerClient.StopContainer(ctx, lokiContainer, 10)
	_ = m.dockerClient.RemoveContainer(ctx, lokiContainer)

	// Convert to absolute path for Docker mount
	absDataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}
	lokiConfigPath := filepath.Join(absDataDir, "loki-config.yaml")

	containerConfig := docker.ContainerConfig{
		Name:  lokiContainer,
		Image: lokiImage,
		Cmd:   []string{"-config.file=/etc/loki/local-config.yaml"},
		Labels: map[string]string{
			"schooner.managed": "true",
			"schooner.service": "loki",
		},
		Volumes: map[string]string{
			"schooner-loki-data": "/loki",
			lokiConfigPath:       "/etc/loki/local-config.yaml",
		},
		Networks:      []string{observabilityNetwork},
		RestartPolicy: "unless-stopped",
	}

	containerID, err := m.dockerClient.CreateAndStartContainer(ctx, containerConfig)
	if err != nil {
		return err
	}

	slog.Info("Loki started", "container_id", containerID[:12])
	return nil
}

// waitForLoki waits for Loki to be ready
func (m *Manager) waitForLoki(ctx context.Context) error {
	for i := 0; i < 30; i++ {
		status, err := m.dockerClient.GetContainerStatus(ctx, lokiContainer)
		if err == nil && status != nil && status.State == "running" {
			// Give Loki a moment to initialize
			time.Sleep(2 * time.Second)
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("timeout waiting for Loki to be ready")
}

// startPromtail starts the Promtail container
func (m *Manager) startPromtail(ctx context.Context, dataDir string) error {
	// Stop existing container if any
	_ = m.dockerClient.StopContainer(ctx, promtailContainer, 10)
	_ = m.dockerClient.RemoveContainer(ctx, promtailContainer)

	// Convert to absolute path for Docker mount
	absDataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	containerConfig := docker.ContainerConfig{
		Name:  promtailContainer,
		Image: promtailImage,
		Cmd:   []string{"-config.file=/etc/promtail/config.yml"},
		Labels: map[string]string{
			"schooner.managed": "true",
			"schooner.service": "promtail",
		},
		Volumes: map[string]string{
			"/var/run/docker.sock":                            "/var/run/docker.sock:ro",
			"/var/lib/docker/containers":                      "/var/lib/docker/containers:ro",
			filepath.Join(absDataDir, "promtail-config.yaml"): "/etc/promtail/config.yml",
			"schooner-promtail-data":                          "/tmp",
		},
		Networks:      []string{observabilityNetwork},
		RestartPolicy: "unless-stopped",
	}

	containerID, err := m.dockerClient.CreateAndStartContainer(ctx, containerConfig)
	if err != nil {
		return err
	}

	slog.Info("Promtail started", "container_id", containerID[:12])
	return nil
}

// startGrafana starts the Grafana container
func (m *Manager) startGrafana(ctx context.Context, dataDir string, port int) error {
	// Stop existing container if any
	_ = m.dockerClient.StopContainer(ctx, grafanaContainer, 10)
	_ = m.dockerClient.RemoveContainer(ctx, grafanaContainer)

	// Convert to absolute path for Docker mount
	absDataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Get or generate admin password
	adminPassword := "admin" // Default, should be changed
	if m.settingsQueries != nil {
		if p, err := m.settingsQueries.Get(ctx, "observability_grafana_password"); err == nil && p != "" {
			adminPassword = p
		}
	}

	containerConfig := docker.ContainerConfig{
		Name:  grafanaContainer,
		Image: grafanaImage,
		Labels: map[string]string{
			"schooner.managed": "true",
			"schooner.service": "grafana",
		},
		Ports: map[string]string{
			"3000": fmt.Sprintf("%d", port),
		},
		Volumes: map[string]string{
			"schooner-grafana-data":                           "/var/lib/grafana",
			filepath.Join(absDataDir, "grafana-provisioning"): "/etc/grafana/provisioning",
		},
		Env: []string{
			"GF_SECURITY_ADMIN_PASSWORD=" + adminPassword,
			"GF_AUTH_ANONYMOUS_ENABLED=false",
			"GF_USERS_ALLOW_SIGN_UP=false",
		},
		Networks:      []string{observabilityNetwork},
		RestartPolicy: "unless-stopped",
	}

	containerID, err := m.dockerClient.CreateAndStartContainer(ctx, containerConfig)
	if err != nil {
		return err
	}

	slog.Info("Grafana started", "container_id", containerID[:12], "port", port)
	return nil
}

// Stop stops the observability stack
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	slog.Info("stopping observability stack")

	var errs []error

	// Stop in reverse order
	if err := m.dockerClient.StopContainer(ctx, grafanaContainer, 30); err != nil {
		slog.Warn("failed to stop Grafana", "error", err)
		errs = append(errs, err)
	}

	if err := m.dockerClient.StopContainer(ctx, promtailContainer, 10); err != nil {
		slog.Warn("failed to stop Promtail", "error", err)
		errs = append(errs, err)
	}

	if err := m.dockerClient.StopContainer(ctx, lokiContainer, 30); err != nil {
		slog.Warn("failed to stop Loki", "error", err)
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors stopping observability stack: %v", errs)
	}

	slog.Info("observability stack stopped")
	return nil
}

// GetStatus returns the status of the observability stack
func (m *Manager) GetStatus(ctx context.Context) (*StackStatus, error) {
	enabled, grafanaPort, _, _ := m.getConfig(ctx)

	status := &StackStatus{
		Enabled: enabled,
	}

	if !enabled {
		return status, nil
	}

	// Get container statuses
	lokiStatus, _ := m.dockerClient.GetContainerStatus(ctx, lokiContainer)
	promtailStatus, _ := m.dockerClient.GetContainerStatus(ctx, promtailContainer)
	grafanaStatus, _ := m.dockerClient.GetContainerStatus(ctx, grafanaContainer)

	status.LokiStatus = lokiStatus
	status.PromtailStatus = promtailStatus
	status.GrafanaStatus = grafanaStatus

	if grafanaStatus != nil && grafanaStatus.State == "running" {
		status.GrafanaURL = fmt.Sprintf("%s:%d", m.getExternalHost(), grafanaPort)
	}

	return status, nil
}

// GetGrafanaURL returns the Grafana URL
func (m *Manager) GetGrafanaURL(ctx context.Context) string {
	_, grafanaPort, _, _ := m.getConfig(ctx)
	return fmt.Sprintf("%s:%d", m.getExternalHost(), grafanaPort)
}

// getExternalHost returns the scheme and hostname from the base URL (without port)
func (m *Manager) getExternalHost() string {
	if m.cfg.Server.BaseURL == "" {
		return "http://localhost"
	}
	parsed, err := url.Parse(m.cfg.Server.BaseURL)
	if err != nil {
		return "http://localhost"
	}
	return fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Hostname())
}

// GetLokiURL returns the internal Loki URL (for API queries)
func (m *Manager) GetLokiURL() string {
	return fmt.Sprintf("http://%s:3100", lokiContainer)
}

// writeConfigs writes all configuration files
func (m *Manager) writeConfigs(dataDir, lokiRetention string) error {
	// Write Loki config
	lokiConfig := getLokiConfig(lokiRetention)
	if err := os.WriteFile(filepath.Join(dataDir, "loki-config.yaml"), []byte(lokiConfig), 0644); err != nil {
		return fmt.Errorf("failed to write Loki config: %w", err)
	}

	// Write Promtail config
	promtailConfig := getPromtailConfig()
	if err := os.WriteFile(filepath.Join(dataDir, "promtail-config.yaml"), []byte(promtailConfig), 0644); err != nil {
		return fmt.Errorf("failed to write Promtail config: %w", err)
	}

	// Write Grafana datasource provisioning
	datasourceConfig := getGrafanaDatasourceConfig()
	if err := os.WriteFile(filepath.Join(dataDir, "grafana-provisioning", "datasources", "loki.yaml"), []byte(datasourceConfig), 0644); err != nil {
		return fmt.Errorf("failed to write Grafana datasource config: %w", err)
	}

	// Write Grafana dashboard provisioning
	dashboardProvisionerConfig := getGrafanaDashboardProvisionerConfig()
	if err := os.WriteFile(filepath.Join(dataDir, "grafana-provisioning", "dashboards", "default.yaml"), []byte(dashboardProvisionerConfig), 0644); err != nil {
		return fmt.Errorf("failed to write Grafana dashboard provisioner config: %w", err)
	}

	// Write Schooner dashboard
	schoonerDashboard := getSchoonerDashboard()
	if err := os.WriteFile(filepath.Join(dataDir, "grafana-provisioning", "dashboards", "schooner.json"), []byte(schoonerDashboard), 0644); err != nil {
		return fmt.Errorf("failed to write Schooner dashboard: %w", err)
	}

	return nil
}
