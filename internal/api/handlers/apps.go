package handlers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"schooner/internal/build"
	"schooner/internal/cloudflare"
	"schooner/internal/config"
	"schooner/internal/database/queries"
	"schooner/internal/docker"
	"schooner/internal/github"
	"schooner/internal/models"
)

// AppHandler handles app-related requests
type AppHandler struct {
	cfg           *config.Config
	appQueries    *queries.AppQueries
	buildQueries  *queries.BuildQueries
	dockerClient  *docker.Client
	tunnelManager *cloudflare.Manager
	orchestrator  *build.Orchestrator
	githubClient  *github.Client
}

// NewAppHandler creates a new AppHandler
func NewAppHandler(cfg *config.Config, appQueries *queries.AppQueries, buildQueries *queries.BuildQueries, dockerClient *docker.Client, tunnelManager *cloudflare.Manager, orchestrator *build.Orchestrator, githubClient *github.Client) *AppHandler {
	return &AppHandler{
		cfg:           cfg,
		appQueries:    appQueries,
		buildQueries:  buildQueries,
		dockerClient:  dockerClient,
		tunnelManager: tunnelManager,
		orchestrator:  orchestrator,
		githubClient:  githubClient,
	}
}

// AppCreateRequest represents the request body for creating an app
type AppCreateRequest struct {
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	RepoURL        string            `json:"repo_url"`
	Branch         string            `json:"branch"`
	WebhookSecret  string            `json:"webhook_secret"`
	BuildStrategy  string            `json:"build_strategy"`
	DockerfilePath string            `json:"dockerfile_path"`
	ComposeFile    string            `json:"compose_file"`
	BuildContext   string            `json:"build_context"`
	ContainerName  string            `json:"container_name"`
	ImageName      string            `json:"image_name"`
	EnvVars        map[string]string `json:"env_vars"`
	AutoDeploy     bool              `json:"auto_deploy"`
	Enabled        bool              `json:"enabled"`
	Subdomain      string            `json:"subdomain"`
	PublicPort     int               `json:"public_port"`
}

// List handles GET /api/apps
func (h *AppHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	apps, err := h.appQueries.List(ctx)
	if err != nil {
		slog.Error("failed to list apps", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(apps)
}

// Get handles GET /api/apps/{appID}
func (h *AppHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	appID := chi.URLParam(r, "appID")

	app, err := h.appQueries.GetByID(ctx, appID)
	if err != nil {
		slog.Error("failed to get app", "appID", appID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if app == nil {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(app)
}

// Create handles POST /api/apps
func (h *AppHandler) Create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req AppCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if req.RepoURL == "" {
		http.Error(w, "repo_url is required", http.StatusBadRequest)
		return
	}

	// Set defaults
	if req.Branch == "" {
		req.Branch = "main"
	}
	if req.BuildStrategy == "" {
		req.BuildStrategy = "dockerfile"
	}
	if req.DockerfilePath == "" {
		req.DockerfilePath = "Dockerfile"
	}
	if req.ComposeFile == "" {
		req.ComposeFile = "docker-compose.yaml"
	}
	if req.BuildContext == "" {
		req.BuildContext = "."
	}

	// Create app
	app := &models.App{
		ID:             uuid.New().String(),
		Name:           req.Name,
		Description:    sql.NullString{String: req.Description, Valid: req.Description != ""},
		RepoURL:        req.RepoURL,
		Branch:         req.Branch,
		WebhookSecret:  sql.NullString{String: req.WebhookSecret, Valid: req.WebhookSecret != ""},
		BuildStrategy:  models.BuildStrategy(req.BuildStrategy),
		DockerfilePath: req.DockerfilePath,
		ComposeFile:    req.ComposeFile,
		BuildContext:   req.BuildContext,
		ContainerName:  sql.NullString{String: req.ContainerName, Valid: req.ContainerName != ""},
		ImageName:      sql.NullString{String: req.ImageName, Valid: req.ImageName != ""},
		EnvVars:        req.EnvVars,
		AutoDeploy:     req.AutoDeploy,
		Enabled:        req.Enabled,
		Subdomain:      sql.NullString{String: req.Subdomain, Valid: req.Subdomain != ""},
		PublicPort:     sql.NullInt64{Int64: int64(req.PublicPort), Valid: req.PublicPort > 0},
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Save env vars
	if err := app.SaveEnvVars(); err != nil {
		slog.Error("failed to save env vars", "error", err)
		http.Error(w, "failed to save env vars", http.StatusInternalServerError)
		return
	}

	if err := h.appQueries.Create(ctx, app); err != nil {
		slog.Error("failed to create app", "error", err)
		http.Error(w, "failed to create app: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Update tunnel routes if configured
	if h.tunnelManager != nil && h.tunnelManager.IsConfigured() {
		if err := h.tunnelManager.AddRoute(ctx, app); err != nil {
			slog.Warn("failed to add tunnel route", "app", app.Name, "error", err)
		}
	}

	// Auto-install GitHub webhook if this is a GitHub repo
	webhookInstalled := false
	if h.githubClient != nil && h.githubClient.HasToken() && strings.Contains(app.RepoURL, "github.com") {
		webhookInstalled = h.installWebhook(ctx, app)
	}

	slog.Info("app created", "id", app.ID, "name", app.Name, "webhookInstalled", webhookInstalled)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(app)
}

// installWebhook attempts to install a GitHub webhook for the app
func (h *AppHandler) installWebhook(ctx context.Context, app *models.App) bool {
	owner, repo, err := github.ParseRepoURL(app.RepoURL)
	if err != nil {
		slog.Warn("failed to parse repo URL for webhook", "repoURL", app.RepoURL, "error", err)
		return false
	}

	// Generate webhook secret if not set
	secret := app.GetWebhookSecret()
	if secret == "" {
		var err error
		secret, err = generateWebhookSecret()
		if err != nil {
			slog.Warn("failed to generate webhook secret", "error", err)
			return false
		}
		app.WebhookSecret = sql.NullString{String: secret, Valid: true}
		// Update app with the generated secret
		if err := h.appQueries.Update(ctx, app); err != nil {
			slog.Warn("failed to save generated webhook secret", "error", err)
		}
	}

	// Build webhook URL
	webhookURL := h.cfg.Server.BaseURL + "/webhook/github/" + app.ID

	// Ensure webhook exists
	webhook, created, err := h.githubClient.EnsureWebhook(ctx, owner, repo, webhookURL, secret)
	if err != nil {
		slog.Warn("failed to install webhook", "app", app.Name, "error", err)
		return false
	}

	if created {
		slog.Info("webhook installed", "app", app.Name, "webhookID", webhook.ID, "url", webhookURL)
	} else {
		slog.Debug("webhook already exists", "app", app.Name, "webhookID", webhook.ID)
	}

	return true
}

// generateWebhookSecret generates a random webhook secret
func generateWebhookSecret() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

// Update handles PUT /api/apps/{appID}
func (h *AppHandler) Update(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	appID := chi.URLParam(r, "appID")

	// Get existing app
	app, err := h.appQueries.GetByID(ctx, appID)
	if err != nil {
		slog.Error("failed to get app", "appID", appID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if app == nil {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}

	var req AppCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Update fields
	if req.Name != "" {
		app.Name = req.Name
	}
	app.Description = sql.NullString{String: req.Description, Valid: req.Description != ""}
	if req.RepoURL != "" {
		app.RepoURL = req.RepoURL
	}
	if req.Branch != "" {
		app.Branch = req.Branch
	}
	app.WebhookSecret = sql.NullString{String: req.WebhookSecret, Valid: req.WebhookSecret != ""}
	if req.BuildStrategy != "" {
		app.BuildStrategy = models.BuildStrategy(req.BuildStrategy)
	}
	if req.DockerfilePath != "" {
		app.DockerfilePath = req.DockerfilePath
	}
	if req.ComposeFile != "" {
		app.ComposeFile = req.ComposeFile
	}
	if req.BuildContext != "" {
		app.BuildContext = req.BuildContext
	}
	app.ContainerName = sql.NullString{String: req.ContainerName, Valid: req.ContainerName != ""}
	app.ImageName = sql.NullString{String: req.ImageName, Valid: req.ImageName != ""}
	app.EnvVars = req.EnvVars
	app.AutoDeploy = req.AutoDeploy
	app.Enabled = req.Enabled
	app.Subdomain = sql.NullString{String: req.Subdomain, Valid: req.Subdomain != ""}
	app.PublicPort = sql.NullInt64{Int64: int64(req.PublicPort), Valid: req.PublicPort > 0}

	// Save env vars
	if err := app.SaveEnvVars(); err != nil {
		slog.Error("failed to save env vars", "error", err)
		http.Error(w, "failed to save env vars", http.StatusInternalServerError)
		return
	}

	if err := h.appQueries.Update(ctx, app); err != nil {
		slog.Error("failed to update app", "error", err)
		http.Error(w, "failed to update app: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Update tunnel routes if configured
	if h.tunnelManager != nil && h.tunnelManager.IsConfigured() {
		if err := h.tunnelManager.AddRoute(ctx, app); err != nil {
			slog.Warn("failed to update tunnel route", "app", app.Name, "error", err)
		}
	}

	slog.Info("app updated", "id", app.ID, "name", app.Name)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(app)
}

// Delete handles DELETE /api/apps/{appID}
func (h *AppHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	appID := chi.URLParam(r, "appID")

	// Check if app exists
	app, err := h.appQueries.GetByID(ctx, appID)
	if err != nil {
		slog.Error("failed to get app", "appID", appID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if app == nil {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}

	// Remove tunnel route if configured
	if h.tunnelManager != nil && h.tunnelManager.IsConfigured() {
		if err := h.tunnelManager.RemoveRoute(ctx, app); err != nil {
			slog.Warn("failed to remove tunnel route", "app", app.Name, "error", err)
		}
	}

	if err := h.appQueries.Delete(ctx, appID); err != nil {
		slog.Error("failed to delete app", "appID", appID, "error", err)
		http.Error(w, "failed to delete app", http.StatusInternalServerError)
		return
	}

	slog.Info("app deleted", "id", appID, "name", app.Name)

	w.WriteHeader(http.StatusNoContent)
}

// Status handles GET /api/apps/{appID}/status - returns container status
func (h *AppHandler) Status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	appID := chi.URLParam(r, "appID")

	app, err := h.appQueries.GetByID(ctx, appID)
	if err != nil {
		slog.Error("failed to get app", "appID", appID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if app == nil {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}

	// Get latest build
	latestBuild, _ := h.buildQueries.GetLatestByAppID(ctx, appID)

	// Get container status from Docker
	var containerStatus *docker.ContainerStatus
	if h.dockerClient != nil {
		containerStatus, _ = h.dockerClient.GetContainerStatus(ctx, app.GetContainerName())
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"app":              app,
		"latest_build":     latestBuild,
		"container_status": containerStatus,
	})
}

// TriggerDeploy handles POST /api/apps/{appID}/deploy
func (h *AppHandler) TriggerDeploy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	appID := chi.URLParam(r, "appID")

	app, err := h.appQueries.GetByID(ctx, appID)
	if err != nil {
		slog.Error("failed to get app", "appID", appID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if app == nil {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}

	if h.orchestrator == nil {
		http.Error(w, "build orchestrator not available", http.StatusServiceUnavailable)
		return
	}

	// Trigger build via orchestrator
	build, err := h.orchestrator.TriggerManualBuild(ctx, appID)
	if err != nil {
		slog.Error("failed to trigger build", "appID", appID, "error", err)
		http.Error(w, "failed to trigger build: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("build triggered", "appID", appID, "buildID", build.ID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":   "queued",
		"build_id": build.ID,
		"message":  "Build queued successfully",
	})
}

// Stop handles POST /api/apps/{appID}/stop
func (h *AppHandler) Stop(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	appID := chi.URLParam(r, "appID")

	app, err := h.appQueries.GetByID(ctx, appID)
	if err != nil {
		slog.Error("failed to get app", "appID", appID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if app == nil {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}

	if h.dockerClient == nil {
		http.Error(w, "Docker client not available", http.StatusServiceUnavailable)
		return
	}

	if err := h.dockerClient.StopContainer(ctx, app.GetContainerName(), 30*time.Second); err != nil {
		slog.Error("failed to stop container", "app", app.Name, "error", err)
		http.Error(w, "failed to stop container: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("container stopped", "app", app.Name)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "stopped",
		"message": "Container stopped successfully",
	})
}

// Start handles POST /api/apps/{appID}/start
func (h *AppHandler) Start(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	appID := chi.URLParam(r, "appID")

	app, err := h.appQueries.GetByID(ctx, appID)
	if err != nil {
		slog.Error("failed to get app", "appID", appID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if app == nil {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}

	if h.dockerClient == nil {
		http.Error(w, "Docker client not available", http.StatusServiceUnavailable)
		return
	}

	if err := h.dockerClient.StartContainer(ctx, app.GetContainerName()); err != nil {
		slog.Error("failed to start container", "app", app.Name, "error", err)
		http.Error(w, "failed to start container: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("container started", "app", app.Name)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "started",
		"message": "Container started successfully",
	})
}

// Restart handles POST /api/apps/{appID}/restart
func (h *AppHandler) Restart(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	appID := chi.URLParam(r, "appID")

	app, err := h.appQueries.GetByID(ctx, appID)
	if err != nil {
		slog.Error("failed to get app", "appID", appID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if app == nil {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}

	if h.dockerClient == nil {
		http.Error(w, "Docker client not available", http.StatusServiceUnavailable)
		return
	}

	if err := h.dockerClient.RestartContainer(ctx, app.GetContainerName(), 30*time.Second); err != nil {
		slog.Error("failed to restart container", "app", app.Name, "error", err)
		http.Error(w, "failed to restart container: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("container restarted", "app", app.Name)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "restarted",
		"message": "Container restarted successfully",
	})
}

// ConfigureWebhook handles POST /api/apps/{appID}/webhook - sets up GitHub webhook
func (h *AppHandler) ConfigureWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	appID := chi.URLParam(r, "appID")

	app, err := h.appQueries.GetByID(ctx, appID)
	if err != nil {
		slog.Error("failed to get app", "appID", appID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if app == nil {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}

	if h.githubClient == nil || !h.githubClient.HasToken() {
		http.Error(w, "GitHub token not configured", http.StatusBadRequest)
		return
	}

	// Parse repo URL to get owner/repo
	owner, repo, err := github.ParseRepoURL(app.RepoURL)
	if err != nil {
		slog.Error("failed to parse repo URL", "url", app.RepoURL, "error", err)
		http.Error(w, "invalid repository URL: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Generate webhook secret if not set
	webhookSecret := app.GetWebhookSecret()
	if webhookSecret == "" {
		secretBytes := make([]byte, 32)
		if _, err := rand.Read(secretBytes); err != nil {
			slog.Error("failed to generate webhook secret", "error", err)
			http.Error(w, "failed to generate secret", http.StatusInternalServerError)
			return
		}
		webhookSecret = hex.EncodeToString(secretBytes)

		// Save the secret to the app
		app.SetWebhookSecret(webhookSecret)
		if err := h.appQueries.Update(ctx, app); err != nil {
			slog.Error("failed to save webhook secret", "error", err)
			http.Error(w, "failed to save webhook secret", http.StatusInternalServerError)
			return
		}
	}

	// Build webhook URL
	webhookURL := fmt.Sprintf("%s/webhook/github/%s", h.cfg.Server.BaseURL, app.ID)

	// Create or ensure webhook exists
	webhook, created, err := h.githubClient.EnsureWebhook(ctx, owner, repo, webhookURL, webhookSecret)
	if err != nil {
		slog.Error("failed to configure webhook", "error", err)
		http.Error(w, "failed to configure webhook: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if created {
		slog.Info("webhook created", "app", app.Name, "repo", fmt.Sprintf("%s/%s", owner, repo), "webhook_id", webhook.ID)
	} else {
		slog.Info("webhook already exists", "app", app.Name, "repo", fmt.Sprintf("%s/%s", owner, repo), "webhook_id", webhook.ID)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"created":     created,
		"webhook_id":  webhook.ID,
		"webhook_url": webhookURL,
		"message":     "Webhook configured successfully",
	})
}

// AllStatuses handles GET /api/apps/statuses - returns all app container statuses
func (h *AppHandler) AllStatuses(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	apps, err := h.appQueries.List(ctx)
	if err != nil {
		slog.Error("failed to list apps", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	type AppStatus struct {
		AppID           string                  `json:"app_id"`
		AppName         string                  `json:"app_name"`
		ContainerStatus *docker.ContainerStatus `json:"container_status"`
	}

	statuses := make([]AppStatus, 0, len(apps))
	for _, app := range apps {
		status := AppStatus{
			AppID:   app.ID,
			AppName: app.Name,
		}

		if h.dockerClient != nil {
			status.ContainerStatus, _ = h.dockerClient.GetContainerStatus(ctx, app.GetContainerName())
		}

		statuses = append(statuses, status)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(statuses)
}

// ContainerStats handles GET /api/containers/stats - returns stats for all running containers
func (h *AppHandler) ContainerStats(w http.ResponseWriter, r *http.Request) {
	if h.dockerClient == nil {
		http.Error(w, "Docker client not available", http.StatusServiceUnavailable)
		return
	}

	// Use a short timeout for the stats collection
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	containers, err := h.dockerClient.ListContainers(ctx, false, nil)
	if err != nil {
		slog.Error("failed to list containers", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	type ContainerStat struct {
		ID            string  `json:"id"`
		Name          string  `json:"name"`
		CPUPercent    float64 `json:"cpu_percent"`
		MemoryUsage   uint64  `json:"memory_usage"`
		MemoryPercent float64 `json:"memory_percent"`
		MemoryDisplay string  `json:"memory_display"`
	}

	// Fetch stats concurrently
	type result struct {
		stat ContainerStat
		ok   bool
	}
	results := make(chan result, len(containers))

	for _, c := range containers {
		go func(c types.Container) {
			name := ""
			if len(c.Names) > 0 {
				name = c.Names[0]
				if len(name) > 0 && name[0] == '/' {
					name = name[1:]
				}
			}

			containerStats, err := h.dockerClient.GetContainerStats(ctx, c.ID)
			if err != nil {
				slog.Debug("failed to get container stats", "container", name, "error", err)
				results <- result{ok: false}
				return
			}

			results <- result{
				stat: ContainerStat{
					ID:            c.ID[:12],
					Name:          name,
					CPUPercent:    containerStats.CPUPercent,
					MemoryUsage:   containerStats.MemoryUsage,
					MemoryPercent: containerStats.MemoryPercent,
					MemoryDisplay: formatBytes(containerStats.MemoryUsage),
				},
				ok: true,
			}
		}(c)
	}

	// Collect results
	stats := make([]ContainerStat, 0, len(containers))
	for i := 0; i < len(containers); i++ {
		select {
		case r := <-results:
			if r.ok {
				stats = append(stats, r.stat)
			}
		case <-ctx.Done():
			break
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// formatBytes formats bytes to human readable string
func formatBytes(bytes uint64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
