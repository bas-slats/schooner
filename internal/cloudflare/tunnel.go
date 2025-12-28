package cloudflare

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"schooner/internal/config"
	"schooner/internal/docker"
	"schooner/internal/models"
)

const (
	cloudflaredImage     = "cloudflare/cloudflared:latest"
	cloudflaredContainer = "schooner-cloudflared"
	defaultConfigDir     = "/data/cloudflared"
	cloudflaredVolume    = "schooner_schooner-data"
)

// tunnelTokenPayload is the decoded structure of a Cloudflare tunnel token
type tunnelTokenPayload struct {
	AccountTag   string `json:"a"`
	TunnelSecret string `json:"s"`
	TunnelID     string `json:"t"`
}

// IngressRule represents a Cloudflare tunnel ingress rule
type IngressRule struct {
	Hostname string `yaml:"hostname,omitempty"`
	Service  string `yaml:"service"`
}

// TunnelConfig represents the cloudflared config.yml structure
type TunnelConfig struct {
	Tunnel          string        `yaml:"tunnel"`
	CredentialsFile string        `yaml:"credentials-file"`
	Ingress         []IngressRule `yaml:"ingress"`
}

// SettingsGetter interface for getting settings from the database
type SettingsGetter interface {
	Get(ctx context.Context, key string) (string, error)
}

// Manager manages the Cloudflare tunnel
type Manager struct {
	cfg             *config.Config
	dockerClient    *docker.Client
	settingsQueries SettingsGetter
	appQueries      AppGetter
	mu              sync.Mutex
	configDir       string
	dnsClient       *DNSClient
}

// AppGetter interface for getting apps from the database
type AppGetter interface {
	ListEnabled(ctx context.Context) ([]*models.App, error)
}

// NewManager creates a new tunnel manager
func NewManager(cfg *config.Config, dockerClient *docker.Client) *Manager {
	return &Manager{
		cfg:          cfg,
		dockerClient: dockerClient,
		configDir:    defaultConfigDir,
	}
}

// SetSettingsQueries sets the settings queries for database-driven config
func (m *Manager) SetSettingsQueries(sq SettingsGetter) {
	m.settingsQueries = sq
}

// SetAppQueries sets the app queries for loading apps
func (m *Manager) SetAppQueries(aq AppGetter) {
	m.appQueries = aq
}

// decodeToken decodes a Cloudflare tunnel token
func decodeToken(token string) (*tunnelTokenPayload, error) {
	data, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return nil, fmt.Errorf("failed to decode token: %w", err)
	}

	var payload tunnelTokenPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	if payload.TunnelID == "" {
		return nil, fmt.Errorf("invalid token: missing tunnel ID")
	}

	return &payload, nil
}

// writeCredentials writes the tunnel credentials file
func (m *Manager) writeCredentials(payload *tunnelTokenPayload) error {
	creds := map[string]string{
		"AccountTag":   payload.AccountTag,
		"TunnelSecret": payload.TunnelSecret,
		"TunnelID":     payload.TunnelID,
	}

	data, err := json.Marshal(creds)
	if err != nil {
		return err
	}

	credsPath := filepath.Join(m.configDir, payload.TunnelID+".json")
	return os.WriteFile(credsPath, data, 0644)
}

// getTunnelConfig loads tunnel configuration from database or config file
func (m *Manager) getTunnelConfig(ctx context.Context) (token, tunnelID, domain, apiToken string) {
	// Try database first
	if m.settingsQueries != nil {
		if t, err := m.settingsQueries.Get(ctx, "cloudflare_tunnel_token"); err == nil && t != "" {
			token = t
		}
		if id, err := m.settingsQueries.Get(ctx, "cloudflare_tunnel_id"); err == nil && id != "" {
			tunnelID = id
		}
		if d, err := m.settingsQueries.Get(ctx, "cloudflare_domain"); err == nil && d != "" {
			domain = d
		}
		if at, err := m.settingsQueries.Get(ctx, "cloudflare_api_token"); err == nil && at != "" {
			apiToken = at
		}
	}

	// Fall back to config file if not set in database
	if token == "" {
		token = m.cfg.Cloudflare.TunnelToken
	}
	if tunnelID == "" {
		tunnelID = m.cfg.Cloudflare.TunnelID
	}
	if domain == "" {
		domain = m.cfg.Cloudflare.Domain
	}
	if apiToken == "" {
		apiToken = m.cfg.Cloudflare.APIToken
	}

	return
}

// IsConfigured returns true if Cloudflare tunnel is configured
func (m *Manager) IsConfigured() bool {
	token, _, domain, _ := m.getTunnelConfig(context.Background())
	return token != "" && domain != ""
}

// Start starts the cloudflared container
func (m *Manager) Start(ctx context.Context) error {
	token, _, domain, apiToken := m.getTunnelConfig(ctx)

	if token == "" || domain == "" {
		slog.Info("Cloudflare tunnel not configured, skipping", "has_token", token != "", "has_domain", domain != "")
		return fmt.Errorf("tunnel not configured: token and domain are required")
	}

	// Decode token to get tunnel credentials
	payload, err := decodeToken(token)
	if err != nil {
		return fmt.Errorf("failed to decode tunnel token: %w", err)
	}

	// Initialize DNS client if we have an API token
	if apiToken != "" {
		m.dnsClient = NewDNSClient(apiToken)
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
	if err := os.MkdirAll(m.configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}

	// Write credentials file
	if err := m.writeCredentials(payload); err != nil {
		return fmt.Errorf("failed to write credentials: %w", err)
	}

	// Load apps and create initial config with routes
	var apps []*models.App
	if m.appQueries != nil {
		apps, _ = m.appQueries.ListEnabled(ctx)
	}
	if err := m.writeConfigForApps(apps, payload.TunnelID, domain); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	// Configure DNS records if we have an API token
	if m.dnsClient != nil {
		m.configureDNSRecords(ctx, apps, payload.TunnelID, domain)
	}

	// Stop existing container if any
	_ = m.dockerClient.StopContainer(ctx, cloudflaredContainer, 10)
	_ = m.dockerClient.RemoveContainer(ctx, cloudflaredContainer)

	slog.Info("starting cloudflared tunnel", "domain", domain, "tunnel_id", payload.TunnelID, "app_count", len(apps))

	// Start cloudflared container with config mode (not token mode)
	// This allows us to control ingress via the config file
	containerConfig := docker.ContainerConfig{
		Name:  cloudflaredContainer,
		Image: cloudflaredImage,
		Cmd: []string{
			"tunnel",
			"--no-autoupdate",
			"--config", "/data/cloudflared/config.yml",
			"run", payload.TunnelID,
		},
		Labels: map[string]string{
			"schooner.managed": "true",
			"schooner.service": "cloudflared",
		},
		RestartPolicy: "unless-stopped",
		Volumes: map[string]string{
			cloudflaredVolume: "/data",
		},
	}

	containerID, err := m.dockerClient.CreateAndStartContainer(ctx, containerConfig)
	if err != nil {
		return fmt.Errorf("failed to start cloudflared: %w", err)
	}

	slog.Info("cloudflared started", "container_id", containerID[:12])
	return nil
}

// configureDNSRecords sets up DNS CNAME records for tunnel hostnames
func (m *Manager) configureDNSRecords(ctx context.Context, apps []*models.App, tunnelID, domain string) {
	// Configure schooner's own hostname
	if m.cfg.Server.BaseURL != "" {
		if parsed, err := url.Parse(m.cfg.Server.BaseURL); err == nil && parsed.Host != "" {
			if err := m.dnsClient.EnsureTunnelCNAME(ctx, parsed.Host, tunnelID); err != nil {
				slog.Warn("failed to configure DNS for schooner", "hostname", parsed.Host, "error", err)
			}
		}
	}

	// Configure DNS for each app
	for _, app := range apps {
		if !app.Enabled {
			continue
		}
		subdomain := app.GetSubdomain()
		if subdomain == "" {
			continue
		}
		hostname := fmt.Sprintf("%s.%s", subdomain, domain)
		if err := m.dnsClient.EnsureTunnelCNAME(ctx, hostname, tunnelID); err != nil {
			slog.Warn("failed to configure DNS for app", "app", app.Name, "hostname", hostname, "error", err)
		}
	}
}

// writeConfigForApps writes the tunnel config with routes for the given apps
func (m *Manager) writeConfigForApps(apps []*models.App, tunnelID, domain string) error {
	var rules []IngressRule

	// Add schooner's own route first (from base_url config)
	if m.cfg.Server.BaseURL != "" {
		if parsed, err := url.Parse(m.cfg.Server.BaseURL); err == nil && parsed.Host != "" {
			// Use service_port if set, otherwise fall back to server port
			port := m.cfg.Cloudflare.ServicePort
			if port == 0 {
				port = m.cfg.Server.Port
			}
			schoonerService := fmt.Sprintf("http://host.docker.internal:%d", port)
			rules = append(rules, IngressRule{
				Hostname: parsed.Host,
				Service:  schoonerService,
			})
			slog.Debug("added schooner tunnel route", "hostname", parsed.Host, "service", schoonerService)
		}
	}

	// Add app routes
	for _, app := range apps {
		if !app.Enabled {
			continue
		}

		subdomain := app.GetSubdomain()
		port := app.GetPublicPort()

		if subdomain == "" || port == 0 {
			continue
		}

		hostname := fmt.Sprintf("%s.%s", subdomain, domain)
		service := fmt.Sprintf("http://host.docker.internal:%d", port)

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

	return m.writeConfigWithTunnelID(rules, tunnelID)
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

// UpdateRoutes updates the tunnel ingress rules based on apps and restarts if needed
func (m *Manager) UpdateRoutes(ctx context.Context, apps []*models.App) error {
	if !m.IsConfigured() {
		return nil
	}

	token, _, domain, apiToken := m.getTunnelConfig(ctx)
	if domain == "" {
		return fmt.Errorf("domain not configured")
	}

	// Decode token for tunnel ID
	payload, err := decodeToken(token)
	if err != nil {
		return fmt.Errorf("failed to decode token: %w", err)
	}

	// Initialize DNS client if we have an API token
	if apiToken != "" && m.dnsClient == nil {
		m.dnsClient = NewDNSClient(apiToken)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Write new config
	if err := m.writeConfigForApps(apps, payload.TunnelID, domain); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	// Configure DNS records if we have an API token
	if m.dnsClient != nil {
		m.configureDNSRecords(ctx, apps, payload.TunnelID, domain)
	}

	// Count valid routes
	routeCount := 0
	for _, app := range apps {
		if app.Enabled && app.GetSubdomain() != "" && app.GetPublicPort() != 0 {
			routeCount++
		}
	}

	slog.Info("tunnel routes updated", "count", routeCount)

	// Restart tunnel to pick up new config
	// cloudflared doesn't support hot reload, so we need to restart
	status, _ := m.dockerClient.GetContainerStatus(ctx, cloudflaredContainer)
	if status != nil && status.State == "running" {
		slog.Info("restarting cloudflared to apply new routes")
		if err := m.dockerClient.RestartContainer(ctx, cloudflaredContainer, 10*time.Second); err != nil {
			return fmt.Errorf("failed to restart cloudflared: %w", err)
		}
	}

	return nil
}

// Reload reloads the tunnel configuration from the database
func (m *Manager) Reload(ctx context.Context) error {
	if !m.IsConfigured() {
		return nil
	}

	if m.appQueries == nil {
		return fmt.Errorf("app queries not configured")
	}

	apps, err := m.appQueries.ListEnabled(ctx)
	if err != nil {
		return fmt.Errorf("failed to list apps: %w", err)
	}

	return m.UpdateRoutes(ctx, apps)
}

// AddRoute adds a single route for an app by reloading all routes
func (m *Manager) AddRoute(ctx context.Context, app *models.App) error {
	subdomain := app.GetSubdomain()
	port := app.GetPublicPort()

	if subdomain == "" || port == 0 {
		return nil // No route needed
	}

	return m.Reload(ctx)
}

// RemoveRoute removes a route for an app by reloading all routes
func (m *Manager) RemoveRoute(ctx context.Context, app *models.App) error {
	return m.Reload(ctx)
}

// writeConfigWithTunnelID writes the cloudflared config.yml file with a specific tunnel ID
func (m *Manager) writeConfigWithTunnelID(rules []IngressRule, tunnelID string) error {
	cfg := TunnelConfig{
		Tunnel:          tunnelID,
		CredentialsFile: fmt.Sprintf("/data/cloudflared/%s.json", tunnelID),
		Ingress:         rules,
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	configPath := filepath.Join(m.configDir, "config.yml")
	return os.WriteFile(configPath, data, 0644)
}

// GetStatus returns the current tunnel status
func (m *Manager) GetStatus(ctx context.Context) (*docker.ContainerStatus, error) {
	if !m.IsConfigured() {
		return nil, nil
	}
	return m.dockerClient.GetContainerStatus(ctx, cloudflaredContainer)
}
