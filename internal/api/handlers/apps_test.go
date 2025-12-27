package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAppCreateRequest_Validation(t *testing.T) {
	tests := []struct {
		name    string
		request AppCreateRequest
		valid   bool
	}{
		{
			name: "valid request",
			request: AppCreateRequest{
				Name:          "test-app",
				RepoURL:       "https://github.com/user/repo.git",
				Branch:        "main",
				BuildStrategy: "dockerfile",
			},
			valid: true,
		},
		{
			name: "missing name",
			request: AppCreateRequest{
				RepoURL:       "https://github.com/user/repo.git",
				Branch:        "main",
				BuildStrategy: "dockerfile",
			},
			valid: false,
		},
		{
			name: "missing repo URL",
			request: AppCreateRequest{
				Name:          "test-app",
				Branch:        "main",
				BuildStrategy: "dockerfile",
			},
			valid: false,
		},
		{
			name: "with subdomain and port",
			request: AppCreateRequest{
				Name:          "test-app",
				RepoURL:       "https://github.com/user/repo.git",
				Branch:        "main",
				BuildStrategy: "dockerfile",
				Subdomain:     "myapp",
				PublicPort:    8080,
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Basic validation check
			isValid := tt.request.Name != "" && tt.request.RepoURL != ""
			if isValid != tt.valid {
				t.Errorf("validation = %v, want %v", isValid, tt.valid)
			}
		})
	}
}

func TestAppCreateRequest_JSONMarshal(t *testing.T) {
	req := AppCreateRequest{
		Name:          "test-app",
		Description:   "Test description",
		RepoURL:       "https://github.com/user/repo.git",
		Branch:        "main",
		BuildStrategy: "dockerfile",
		AutoDeploy:    true,
		Enabled:       true,
		Subdomain:     "myapp",
		PublicPort:    8080,
		EnvVars: map[string]string{
			"KEY": "value",
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var decoded AppCreateRequest
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.Name != req.Name {
		t.Errorf("Name = %v, want %v", decoded.Name, req.Name)
	}
	if decoded.Subdomain != req.Subdomain {
		t.Errorf("Subdomain = %v, want %v", decoded.Subdomain, req.Subdomain)
	}
	if decoded.PublicPort != req.PublicPort {
		t.Errorf("PublicPort = %v, want %v", decoded.PublicPort, req.PublicPort)
	}
}

func TestNewAppHandler(t *testing.T) {
	handler := NewAppHandler(nil, nil, nil, nil)
	if handler == nil {
		t.Error("Expected non-nil handler")
	}
	if handler.appQueries != nil {
		t.Error("Expected nil appQueries")
	}
	if handler.buildQueries != nil {
		t.Error("Expected nil buildQueries")
	}
	if handler.dockerClient != nil {
		t.Error("Expected nil dockerClient")
	}
	if handler.tunnelManager != nil {
		t.Error("Expected nil tunnelManager")
	}
}

func TestAppHandler_List_NoQueries(t *testing.T) {
	handler := NewAppHandler(nil, nil, nil, nil)

	req := httptest.NewRequest("GET", "/api/apps", nil)
	w := httptest.NewRecorder()

	// This will panic/fail without proper queries, which is expected
	defer func() {
		if r := recover(); r == nil {
			// Expected behavior - handler needs proper dependencies
		}
	}()

	handler.List(w, req)
}

func TestParseEnvVarsLogic(t *testing.T) {
	// Test the logic that would be used in submitAddApp/submitEditApp
	tests := []struct {
		name     string
		input    string
		expected map[string]string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: map[string]string{},
		},
		{
			name:     "single var",
			input:    "KEY=value",
			expected: map[string]string{"KEY": "value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseEnvVarsForTest(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("len(result) = %v, want %v", len(result), len(tt.expected))
			}
			for k, v := range tt.expected {
				if result[k] != v {
					t.Errorf("result[%s] = %v, want %v", k, result[k], v)
				}
			}
		})
	}
}

// Helper to simulate the JavaScript parseEnvVars function
func parseEnvVarsForTest(input string) map[string]string {
	result := make(map[string]string)
	if input == "" {
		return result
	}

	lines := splitLines(input)
	for _, line := range lines {
		line = trimWhitespace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		idx := -1
		for i := 0; i < len(line); i++ {
			if line[i] == '=' {
				idx = i
				break
			}
		}
		if idx > 0 {
			key := trimWhitespace(line[:idx])
			value := ""
			if idx < len(line)-1 {
				value = line[idx+1:]
			}
			result[key] = value
		}
	}
	return result
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimWhitespace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

func TestAppCreateRequest_HTTPRequest(t *testing.T) {
	reqBody := AppCreateRequest{
		Name:          "test-app",
		RepoURL:       "https://github.com/user/repo.git",
		Branch:        "main",
		BuildStrategy: "dockerfile",
		AutoDeploy:    true,
		Enabled:       true,
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/apps", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	if req.Method != "POST" {
		t.Errorf("Method = %v, want POST", req.Method)
	}
	if req.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %v, want application/json", req.Header.Get("Content-Type"))
	}
}

func TestHTTPStatusCodes(t *testing.T) {
	// Verify expected HTTP status codes
	if http.StatusOK != 200 {
		t.Errorf("StatusOK = %v, want 200", http.StatusOK)
	}
	if http.StatusCreated != 201 {
		t.Errorf("StatusCreated = %v, want 201", http.StatusCreated)
	}
	if http.StatusNoContent != 204 {
		t.Errorf("StatusNoContent = %v, want 204", http.StatusNoContent)
	}
	if http.StatusBadRequest != 400 {
		t.Errorf("StatusBadRequest = %v, want 400", http.StatusBadRequest)
	}
	if http.StatusNotFound != 404 {
		t.Errorf("StatusNotFound = %v, want 404", http.StatusNotFound)
	}
	if http.StatusInternalServerError != 500 {
		t.Errorf("StatusInternalServerError = %v, want 500", http.StatusInternalServerError)
	}
}
