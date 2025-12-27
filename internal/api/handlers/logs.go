package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"

	"schooner/internal/database/queries"
	"schooner/internal/observability"
)

// LogsHandler handles container log requests via Loki
type LogsHandler struct {
	observabilityManager *observability.Manager
	appQueries           *queries.AppQueries
}

// NewLogsHandler creates a new LogsHandler
func NewLogsHandler(observabilityManager *observability.Manager, appQueries *queries.AppQueries) *LogsHandler {
	return &LogsHandler{
		observabilityManager: observabilityManager,
		appQueries:           appQueries,
	}
}

// LogSource represents a log source (app or container)
type LogSource struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"` // "app" or "service"
}

// ListSources handles GET /api/logs - lists available log sources
func (h *LogsHandler) ListSources(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if h.observabilityManager == nil || !h.observabilityManager.IsEnabled(ctx) {
		http.Error(w, "observability not enabled", http.StatusServiceUnavailable)
		return
	}

	// Get all apps
	apps, err := h.appQueries.List(ctx)
	if err != nil {
		slog.Error("failed to list apps", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	sources := make([]LogSource, 0, len(apps))
	for _, app := range apps {
		sources = append(sources, LogSource{
			ID:   app.ID,
			Name: app.Name,
			Type: "app",
		})
	}

	// Add infrastructure services
	infraServices := []LogSource{
		{ID: "schooner-loki", Name: "Loki", Type: "service"},
		{ID: "schooner-promtail", Name: "Promtail", Type: "service"},
		{ID: "schooner-grafana", Name: "Grafana", Type: "service"},
		{ID: "schooner-cloudflared", Name: "Cloudflare Tunnel", Type: "service"},
	}
	sources = append(sources, infraServices...)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sources)
}

// GetLogs handles GET /api/logs/{appID} - fetches logs for an app from Loki
func (h *LogsHandler) GetLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	appID := chi.URLParam(r, "appID")

	if h.observabilityManager == nil || !h.observabilityManager.IsEnabled(ctx) {
		http.Error(w, "observability not enabled", http.StatusServiceUnavailable)
		return
	}

	// Parse query parameters
	start := r.URL.Query().Get("start")
	end := r.URL.Query().Get("end")
	limit := r.URL.Query().Get("limit")

	if start == "" {
		// Default to last 1 hour
		start = fmt.Sprintf("%d", time.Now().Add(-1*time.Hour).UnixNano())
	}
	if end == "" {
		end = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	if limit == "" {
		limit = "1000"
	}

	// Build Loki query
	var query string
	if appID == "schooner-loki" || appID == "schooner-promtail" || appID == "schooner-grafana" || appID == "schooner-cloudflared" {
		// Infrastructure service - query by container name
		query = fmt.Sprintf(`{container="%s"}`, appID)
	} else {
		// App - query by app_id label
		query = fmt.Sprintf(`{app_id="%s"}`, appID)
	}

	// Query Loki
	lokiURL := h.observabilityManager.GetLokiURL()
	queryURL := fmt.Sprintf("%s/loki/api/v1/query_range?query=%s&start=%s&end=%s&limit=%s",
		lokiURL,
		url.QueryEscape(query),
		start,
		end,
		limit,
	)

	resp, err := http.Get(queryURL)
	if err != nil {
		slog.Error("failed to query Loki", "error", err, "url", queryURL)
		http.Error(w, "failed to query logs", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	// Forward Loki response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// StreamLogs handles GET /api/logs/{appID}/stream - SSE stream of logs
func (h *LogsHandler) StreamLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	appID := chi.URLParam(r, "appID")

	if h.observabilityManager == nil || !h.observabilityManager.IsEnabled(ctx) {
		http.Error(w, "observability not enabled", http.StatusServiceUnavailable)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Build Loki query
	var query string
	if appID == "schooner-loki" || appID == "schooner-promtail" || appID == "schooner-grafana" || appID == "schooner-cloudflared" {
		query = fmt.Sprintf(`{container="%s"}`, appID)
	} else {
		query = fmt.Sprintf(`{app_id="%s"}`, appID)
	}

	lokiURL := h.observabilityManager.GetLokiURL()
	lastTimestamp := time.Now().Add(-5 * time.Minute).UnixNano()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Send initial connection message
	fmt.Fprintf(w, "event: connected\ndata: {\"status\": \"connected\"}\n\n")
	flusher.Flush()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Query Loki for new logs since last timestamp
			now := time.Now().UnixNano()
			queryURL := fmt.Sprintf("%s/loki/api/v1/query_range?query=%s&start=%d&end=%d&limit=100&direction=forward",
				lokiURL,
				url.QueryEscape(query),
				lastTimestamp,
				now,
			)

			resp, err := http.Get(queryURL)
			if err != nil {
				slog.Debug("failed to query Loki", "error", err)
				continue
			}

			var lokiResp LokiQueryResponse
			if err := json.NewDecoder(resp.Body).Decode(&lokiResp); err != nil {
				resp.Body.Close()
				continue
			}
			resp.Body.Close()

			// Send each log entry as an SSE event
			for _, stream := range lokiResp.Data.Result {
				for _, entry := range stream.Values {
					if len(entry) >= 2 {
						timestamp := entry[0]
						message := entry[1]

						logEntry := map[string]interface{}{
							"timestamp": timestamp,
							"message":   message,
							"labels":    stream.Stream,
						}

						data, _ := json.Marshal(logEntry)
						fmt.Fprintf(w, "event: log\ndata: %s\n\n", data)

						// Update last timestamp
						if ts, err := parseNanoTimestamp(timestamp); err == nil && ts > lastTimestamp {
							lastTimestamp = ts
						}
					}
				}
			}

			flusher.Flush()
		}
	}
}

// LokiQueryResponse represents Loki query response
type LokiQueryResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Stream map[string]string `json:"stream"`
			Values [][]string        `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

// parseNanoTimestamp parses a nanosecond timestamp string
func parseNanoTimestamp(s string) (int64, error) {
	var ts int64
	_, err := fmt.Sscanf(s, "%d", &ts)
	return ts, err
}
