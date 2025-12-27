package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewSettingsHandler(t *testing.T) {
	handler := NewSettingsHandler(nil, nil, nil)
	if handler == nil {
		t.Error("Expected non-nil handler")
	}
	if handler.settingsQueries != nil {
		t.Error("Expected nil settingsQueries")
	}
	if handler.githubClient != nil {
		t.Error("Expected nil githubClient")
	}
	if handler.tunnelManager != nil {
		t.Error("Expected nil tunnelManager")
	}
}

func TestSettingsHandler_GetTunnelStatus_NoManager(t *testing.T) {
	handler := NewSettingsHandler(nil, nil, nil)

	req := httptest.NewRequest("GET", "/api/settings/tunnel-status", nil)
	w := httptest.NewRecorder()

	handler.GetTunnelStatus(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %v, want %v", w.Code, http.StatusOK)
	}

	var response map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&response)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["configured"] != false {
		t.Errorf("configured = %v, want false", response["configured"])
	}
	if response["running"] != false {
		t.Errorf("running = %v, want false", response["running"])
	}
}

func TestSettingsHandler_SetTunnelConfig_InvalidBody(t *testing.T) {
	handler := NewSettingsHandler(nil, nil, nil)

	req := httptest.NewRequest("POST", "/api/settings/tunnel", strings.NewReader("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.SetTunnelConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %v, want %v", w.Code, http.StatusBadRequest)
	}
}

func TestSettingsHandler_StartTunnel_NoManager(t *testing.T) {
	handler := NewSettingsHandler(nil, nil, nil)

	req := httptest.NewRequest("POST", "/api/settings/tunnel/start", nil)
	w := httptest.NewRecorder()

	handler.StartTunnel(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Status = %v, want %v", w.Code, http.StatusServiceUnavailable)
	}
}

func TestSettingsHandler_StopTunnel_NoManager(t *testing.T) {
	handler := NewSettingsHandler(nil, nil, nil)

	req := httptest.NewRequest("POST", "/api/settings/tunnel/stop", nil)
	w := httptest.NewRecorder()

	handler.StopTunnel(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Status = %v, want %v", w.Code, http.StatusServiceUnavailable)
	}
}

func TestSettingsHandler_SetCloneDirectory_InvalidBody(t *testing.T) {
	handler := NewSettingsHandler(nil, nil, nil)

	req := httptest.NewRequest("POST", "/api/settings/clone-directory", strings.NewReader("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.SetCloneDirectory(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %v, want %v", w.Code, http.StatusBadRequest)
	}
}

func TestSettingsHandler_SetCloneDirectory_EmptyPath(t *testing.T) {
	handler := NewSettingsHandler(nil, nil, nil)

	body := `{"clone_directory": ""}`
	req := httptest.NewRequest("POST", "/api/settings/clone-directory", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.SetCloneDirectory(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %v, want %v", w.Code, http.StatusBadRequest)
	}
}

func TestSettingsHandler_SetGitHubToken_InvalidBody(t *testing.T) {
	handler := NewSettingsHandler(nil, nil, nil)

	req := httptest.NewRequest("POST", "/api/settings/github-token", strings.NewReader("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.SetGitHubToken(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %v, want %v", w.Code, http.StatusBadRequest)
	}
}
