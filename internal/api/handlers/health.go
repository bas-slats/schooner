package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"schooner/internal/health"
)

// HealthHandler handles health check requests
type HealthHandler struct {
	startTime time.Time
}

// NewHealthHandler creates a new HealthHandler
func NewHealthHandler() *HealthHandler {
	return &HealthHandler{
		startTime: time.Now(),
	}
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status  string `json:"status"`
	Uptime  string `json:"uptime"`
	Version string `json:"version,omitempty"`
}

// Check handles GET /health
func (h *HealthHandler) Check(w http.ResponseWriter, r *http.Request) {
	response := HealthResponse{
		Status: "ok",
		Uptime: time.Since(h.startTime).Round(time.Second).String(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetSystemHealth handles GET /api/health/system
func (h *HealthHandler) GetSystemHealth(w http.ResponseWriter, r *http.Request) {
	systemHealth, err := health.GetSystemHealth()
	if err != nil {
		http.Error(w, "failed to get system health", http.StatusInternalServerError)
		return
	}

	// Format the response with human-readable values
	response := map[string]interface{}{
		"cpu": map[string]interface{}{
			"usage_percent": systemHealth.CPU.UsagePercent,
			"load_avg_1":    systemHealth.CPU.LoadAvg1,
			"load_avg_5":    systemHealth.CPU.LoadAvg5,
			"load_avg_15":   systemHealth.CPU.LoadAvg15,
			"num_cores":     systemHealth.NumCPU,
		},
		"memory": map[string]interface{}{
			"total":         systemHealth.Memory.Total,
			"used":          systemHealth.Memory.Used,
			"free":          systemHealth.Memory.Free,
			"used_percent":  systemHealth.Memory.UsedPercent,
			"total_display": health.FormatBytes(systemHealth.Memory.Total),
			"used_display":  health.FormatBytes(systemHealth.Memory.Used),
			"free_display":  health.FormatBytes(systemHealth.Memory.Free),
		},
		"disk": map[string]interface{}{
			"total":         systemHealth.Disk.Total,
			"used":          systemHealth.Disk.Used,
			"free":          systemHealth.Disk.Free,
			"used_percent":  systemHealth.Disk.UsedPercent,
			"total_display": health.FormatBytes(systemHealth.Disk.Total),
			"used_display":  health.FormatBytes(systemHealth.Disk.Used),
			"free_display":  health.FormatBytes(systemHealth.Disk.Free),
			"path":          systemHealth.Disk.Path,
		},
		"platform":    systemHealth.Platform,
		"go_routines": systemHealth.GoRoutines,
	}

	if systemHealth.Uptime > 0 {
		response["uptime"] = systemHealth.Uptime.Seconds()
		response["uptime_display"] = health.FormatDuration(systemHealth.Uptime)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
