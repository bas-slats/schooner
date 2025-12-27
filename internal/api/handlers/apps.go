package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"homelab-cd/internal/database/queries"
)

// AppHandler handles app-related requests
type AppHandler struct {
	appQueries   *queries.AppQueries
	buildQueries *queries.BuildQueries
}

// NewAppHandler creates a new AppHandler
func NewAppHandler(appQueries *queries.AppQueries, buildQueries *queries.BuildQueries) *AppHandler {
	return &AppHandler{
		appQueries:   appQueries,
		buildQueries: buildQueries,
	}
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
	// TODO: Implement app creation from API
	http.Error(w, "not implemented - apps are defined in config.yaml", http.StatusNotImplemented)
}

// Update handles PUT /api/apps/{appID}
func (h *AppHandler) Update(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement app update
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

// Delete handles DELETE /api/apps/{appID}
func (h *AppHandler) Delete(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement app deletion
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

// Status handles GET /api/apps/{appID}/status - returns HTMX partial
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

	// TODO: Get container status from Docker
	containerStatus := "unknown"

	// Return JSON for now, will return HTMX partial when templates are set up
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

	// TODO: Trigger build via orchestrator

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "queued",
		"message": "Build will start shortly",
	})
}

// Stop handles POST /api/apps/{appID}/stop
func (h *AppHandler) Stop(w http.ResponseWriter, r *http.Request) {
	// TODO: Stop container via Docker client
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

// Start handles POST /api/apps/{appID}/start
func (h *AppHandler) Start(w http.ResponseWriter, r *http.Request) {
	// TODO: Start container via Docker client
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
