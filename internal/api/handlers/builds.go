package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"schooner/internal/database/queries"
)

// BuildHandler handles build-related requests
type BuildHandler struct {
	buildQueries *queries.BuildQueries
	logQueries   *queries.LogQueries
}

// NewBuildHandler creates a new BuildHandler
func NewBuildHandler(buildQueries *queries.BuildQueries, logQueries *queries.LogQueries) *BuildHandler {
	return &BuildHandler{
		buildQueries: buildQueries,
		logQueries:   logQueries,
	}
}

// List handles GET /api/builds
func (h *BuildHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query params
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	appID := r.URL.Query().Get("app_id")

	var builds interface{}
	var err error

	if appID != "" {
		offset := 0
		if o := r.URL.Query().Get("offset"); o != "" {
			if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
				offset = parsed
			}
		}
		builds, err = h.buildQueries.ListByAppID(ctx, appID, limit, offset)
	} else {
		builds, err = h.buildQueries.ListRecent(ctx, limit)
	}

	if err != nil {
		slog.Error("failed to list builds", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(builds)
}

// Get handles GET /api/builds/{buildID}
func (h *BuildHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	buildID := chi.URLParam(r, "buildID")

	build, err := h.buildQueries.GetByID(ctx, buildID)
	if err != nil {
		slog.Error("failed to get build", "buildID", buildID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if build == nil {
		http.Error(w, "build not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(build)
}

// Cancel handles POST /api/builds/{buildID}/cancel
func (h *BuildHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement build cancellation
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

// Retry handles POST /api/builds/{buildID}/retry
func (h *BuildHandler) Retry(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement build retry
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

// GetLogs handles GET /api/builds/{buildID}/logs
func (h *BuildHandler) GetLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	buildID := chi.URLParam(r, "buildID")

	// Check if build exists
	build, err := h.buildQueries.GetByID(ctx, buildID)
	if err != nil {
		slog.Error("failed to get build", "buildID", buildID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if build == nil {
		http.Error(w, "build not found", http.StatusNotFound)
		return
	}

	// Get logs
	logs, err := h.logQueries.GetByBuildID(ctx, buildID)
	if err != nil {
		slog.Error("failed to get logs", "buildID", buildID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs)
}

// StreamLogs handles GET /api/builds/{buildID}/logs/stream - SSE endpoint
func (h *BuildHandler) StreamLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	buildID := chi.URLParam(r, "buildID")

	// Check if build exists
	build, err := h.buildQueries.GetByID(ctx, buildID)
	if err != nil {
		slog.Error("failed to get build", "buildID", buildID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if build == nil {
		http.Error(w, "build not found", http.StatusNotFound)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	// Send existing logs first
	existingLogs, _ := h.logQueries.GetByBuildID(ctx, buildID)
	for _, log := range existingLogs {
		data, _ := json.Marshal(log)
		fmt.Fprintf(w, "event: log\ndata: %s\n\n", data)
	}
	flusher.Flush()

	// If build is complete, close connection
	if build.IsComplete() {
		fmt.Fprintf(w, "event: complete\ndata: {\"status\": \"%s\"}\n\n", build.Status)
		flusher.Flush()
		return
	}

	// Track last log ID for polling new logs
	var lastLogID int64
	if len(existingLogs) > 0 {
		lastLogID = existingLogs[len(existingLogs)-1].ID
	}

	// Poll for new logs every 500ms
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Get new logs since last ID
			newLogs, err := h.logQueries.GetByBuildIDAfterID(ctx, buildID, lastLogID)
			if err != nil {
				slog.Error("failed to get new logs", "buildID", buildID, "error", err)
				continue
			}

			for _, log := range newLogs {
				data, _ := json.Marshal(log)
				fmt.Fprintf(w, "event: log\ndata: %s\n\n", data)
				lastLogID = log.ID
			}

			if len(newLogs) > 0 {
				flusher.Flush()
			}

			// Check if build is complete
			build, err = h.buildQueries.GetByID(ctx, buildID)
			if err != nil {
				continue
			}

			if build.IsComplete() {
				fmt.Fprintf(w, "event: complete\ndata: {\"status\": \"%s\"}\n\n", build.Status)
				flusher.Flush()
				return
			}
		}
	}
}
