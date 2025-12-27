package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"schooner/internal/cloudflare"
	"schooner/internal/database/queries"
	"schooner/internal/github"
	"schooner/internal/observability"
)

// SettingsHandler handles settings-related requests
type SettingsHandler struct {
	settingsQueries      *queries.SettingsQueries
	githubClient         *github.Client
	tunnelManager        *cloudflare.Manager
	observabilityManager *observability.Manager
}

// NewSettingsHandler creates a new SettingsHandler
func NewSettingsHandler(settingsQueries *queries.SettingsQueries, githubClient *github.Client, tunnelManager *cloudflare.Manager, observabilityManager *observability.Manager) *SettingsHandler {
	return &SettingsHandler{
		settingsQueries:      settingsQueries,
		githubClient:         githubClient,
		tunnelManager:        tunnelManager,
		observabilityManager: observabilityManager,
	}
}

// GetAll handles GET /api/settings - returns all settings
func (h *SettingsHandler) GetAll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	settings, err := h.settingsQueries.GetAll(ctx)
	if err != nil {
		slog.Error("failed to get settings", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Mask sensitive values
	if _, ok := settings["github_token"]; ok {
		settings["github_token"] = "********"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settings)
}

// SetGitHubToken handles POST /api/settings/github-token
func (h *SettingsHandler) SetGitHubToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Token == "" {
		http.Error(w, "token is required", http.StatusBadRequest)
		return
	}

	// Validate the token by making a test API call
	testClient := github.NewClient(req.Token)
	username, err := testClient.GetUser(ctx)
	if err != nil {
		slog.Error("invalid GitHub token", "error", err)
		http.Error(w, "invalid GitHub token: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Save the token
	if err := h.settingsQueries.Set(ctx, "github_token", req.Token); err != nil {
		slog.Error("failed to save GitHub token", "error", err)
		http.Error(w, "failed to save token", http.StatusInternalServerError)
		return
	}

	// Update the shared client
	h.githubClient.SetToken(req.Token)

	slog.Info("GitHub token configured", "username", username)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"username": username,
		"message":  "GitHub token configured successfully",
	})
}

// DeleteGitHubToken handles DELETE /api/settings/github-token
func (h *SettingsHandler) DeleteGitHubToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := h.settingsQueries.Delete(ctx, "github_token"); err != nil {
		slog.Error("failed to delete GitHub token", "error", err)
		http.Error(w, "failed to delete token", http.StatusInternalServerError)
		return
	}

	// Clear the shared client
	h.githubClient.SetToken("")

	slog.Info("GitHub token removed")

	w.WriteHeader(http.StatusNoContent)
}

// GetGitHubStatus handles GET /api/settings/github-status
func (h *SettingsHandler) GetGitHubStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	token, err := h.settingsQueries.Get(ctx, "github_token")
	if err != nil {
		slog.Error("failed to get GitHub token", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	status := map[string]interface{}{
		"configured": token != "",
		"username":   "",
	}

	if token != "" {
		h.githubClient.SetToken(token)
		if username, err := h.githubClient.GetUser(ctx); err == nil {
			status["username"] = username
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// GetCloneDirectory handles GET /api/settings/clone-directory
func (h *SettingsHandler) GetCloneDirectory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	cloneDir, err := h.settingsQueries.Get(ctx, "clone_directory")
	if err != nil {
		slog.Error("failed to get clone directory", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Default if not set
	if cloneDir == "" {
		cloneDir = "./data/repos"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"clone_directory": cloneDir,
	})
}

// SetCloneDirectory handles POST /api/settings/clone-directory
func (h *SettingsHandler) SetCloneDirectory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req struct {
		CloneDirectory string `json:"clone_directory"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.CloneDirectory == "" {
		http.Error(w, "clone_directory is required", http.StatusBadRequest)
		return
	}

	// Save the setting
	if err := h.settingsQueries.Set(ctx, "clone_directory", req.CloneDirectory); err != nil {
		slog.Error("failed to save clone directory", "error", err)
		http.Error(w, "failed to save clone directory", http.StatusInternalServerError)
		return
	}

	slog.Info("clone directory configured", "path", req.CloneDirectory)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":         true,
		"clone_directory": req.CloneDirectory,
		"message":         "Clone directory configured successfully",
	})
}

// GetTunnelStatus handles GET /api/settings/tunnel-status
func (h *SettingsHandler) GetTunnelStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	status := map[string]interface{}{
		"configured": false,
		"running":    false,
		"domain":     "",
	}

	if h.tunnelManager == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
		return
	}

	status["configured"] = h.tunnelManager.IsConfigured()

	// Get container status
	containerStatus, err := h.tunnelManager.GetStatus(ctx)
	if err == nil && containerStatus != nil {
		status["running"] = containerStatus.State == "running"
		status["container_status"] = containerStatus.State
	}

	// Get domain from settings
	domain, _ := h.settingsQueries.Get(ctx, "cloudflare_domain")
	status["domain"] = domain

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// SetTunnelConfig handles POST /api/settings/tunnel
func (h *SettingsHandler) SetTunnelConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req struct {
		TunnelToken string `json:"tunnel_token"`
		TunnelID    string `json:"tunnel_id"`
		Domain      string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Save settings
	if req.TunnelToken != "" {
		if err := h.settingsQueries.Set(ctx, "cloudflare_tunnel_token", req.TunnelToken); err != nil {
			slog.Error("failed to save tunnel token", "error", err)
			http.Error(w, "failed to save tunnel token", http.StatusInternalServerError)
			return
		}
	}

	if req.TunnelID != "" {
		if err := h.settingsQueries.Set(ctx, "cloudflare_tunnel_id", req.TunnelID); err != nil {
			slog.Error("failed to save tunnel ID", "error", err)
			http.Error(w, "failed to save tunnel ID", http.StatusInternalServerError)
			return
		}
	}

	if req.Domain != "" {
		if err := h.settingsQueries.Set(ctx, "cloudflare_domain", req.Domain); err != nil {
			slog.Error("failed to save domain", "error", err)
			http.Error(w, "failed to save domain", http.StatusInternalServerError)
			return
		}
	}

	slog.Info("cloudflare tunnel settings saved", "domain", req.Domain)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Tunnel configuration saved",
	})
}

// StartTunnel handles POST /api/settings/tunnel/start
func (h *SettingsHandler) StartTunnel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if h.tunnelManager == nil {
		http.Error(w, "tunnel manager not available", http.StatusServiceUnavailable)
		return
	}

	if err := h.tunnelManager.Start(ctx); err != nil {
		slog.Error("failed to start tunnel", "error", err)
		http.Error(w, "failed to start tunnel: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Tunnel started",
	})
}

// StopTunnel handles POST /api/settings/tunnel/stop
func (h *SettingsHandler) StopTunnel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if h.tunnelManager == nil {
		http.Error(w, "tunnel manager not available", http.StatusServiceUnavailable)
		return
	}

	if err := h.tunnelManager.Stop(ctx); err != nil {
		slog.Error("failed to stop tunnel", "error", err)
		http.Error(w, "failed to stop tunnel: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Tunnel stopped",
	})
}

// GetObservabilityStatus handles GET /api/settings/observability-status
func (h *SettingsHandler) GetObservabilityStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if h.observabilityManager == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"available": false,
			"enabled":   false,
		})
		return
	}

	status, err := h.observabilityManager.GetStatus(ctx)
	if err != nil {
		slog.Error("failed to get observability status", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"available":   true,
		"enabled":     status.Enabled,
		"grafana_url": status.GrafanaURL,
	}

	if status.LokiStatus != nil {
		response["loki_status"] = status.LokiStatus.State
	}
	if status.PromtailStatus != nil {
		response["promtail_status"] = status.PromtailStatus.State
	}
	if status.GrafanaStatus != nil {
		response["grafana_status"] = status.GrafanaStatus.State
	}

	// Check if all services are running
	running := status.LokiStatus != nil && status.LokiStatus.State == "running" &&
		status.PromtailStatus != nil && status.PromtailStatus.State == "running" &&
		status.GrafanaStatus != nil && status.GrafanaStatus.State == "running"
	response["running"] = running

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// SetObservabilityConfig handles POST /api/settings/observability
func (h *SettingsHandler) SetObservabilityConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req struct {
		Enabled       bool   `json:"enabled"`
		GrafanaPort   int    `json:"grafana_port"`
		LokiRetention string `json:"loki_retention"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Save settings
	if err := h.settingsQueries.Set(ctx, "observability_enabled", fmt.Sprintf("%t", req.Enabled)); err != nil {
		slog.Error("failed to save observability enabled", "error", err)
		http.Error(w, "failed to save settings", http.StatusInternalServerError)
		return
	}

	if req.GrafanaPort > 0 {
		if err := h.settingsQueries.Set(ctx, "observability_grafana_port", fmt.Sprintf("%d", req.GrafanaPort)); err != nil {
			slog.Error("failed to save Grafana port", "error", err)
			http.Error(w, "failed to save settings", http.StatusInternalServerError)
			return
		}
	}

	if req.LokiRetention != "" {
		if err := h.settingsQueries.Set(ctx, "observability_loki_retention", req.LokiRetention); err != nil {
			slog.Error("failed to save Loki retention", "error", err)
			http.Error(w, "failed to save settings", http.StatusInternalServerError)
			return
		}
	}

	slog.Info("observability settings saved", "enabled", req.Enabled, "grafana_port", req.GrafanaPort, "retention", req.LokiRetention)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Observability settings saved",
	})
}

// StartObservability handles POST /api/settings/observability/start
func (h *SettingsHandler) StartObservability(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if h.observabilityManager == nil {
		http.Error(w, "observability manager not available", http.StatusServiceUnavailable)
		return
	}

	// Enable observability before starting
	if err := h.settingsQueries.Set(ctx, "observability_enabled", "true"); err != nil {
		slog.Error("failed to enable observability", "error", err)
		http.Error(w, "failed to enable observability", http.StatusInternalServerError)
		return
	}

	if err := h.observabilityManager.Start(ctx); err != nil {
		slog.Error("failed to start observability stack", "error", err)
		http.Error(w, "failed to start observability: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"message":     "Observability stack started",
		"grafana_url": h.observabilityManager.GetGrafanaURL(ctx),
	})
}

// StopObservability handles POST /api/settings/observability/stop
func (h *SettingsHandler) StopObservability(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if h.observabilityManager == nil {
		http.Error(w, "observability manager not available", http.StatusServiceUnavailable)
		return
	}

	if err := h.observabilityManager.Stop(ctx); err != nil {
		slog.Error("failed to stop observability stack", "error", err)
		http.Error(w, "failed to stop observability: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Disable observability after stopping
	if err := h.settingsQueries.Set(ctx, "observability_enabled", "false"); err != nil {
		slog.Warn("failed to disable observability setting", "error", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Observability stack stopped",
	})
}
