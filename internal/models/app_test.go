package models

import (
	"database/sql"
	"encoding/json"
	"testing"
)

func TestApp_GetDescription(t *testing.T) {
	tests := []struct {
		name     string
		app      App
		expected string
	}{
		{
			name:     "valid description",
			app:      App{Description: sql.NullString{String: "Test description", Valid: true}},
			expected: "Test description",
		},
		{
			name:     "null description",
			app:      App{Description: sql.NullString{Valid: false}},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.app.GetDescription(); got != tt.expected {
				t.Errorf("GetDescription() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestApp_GetContainerName(t *testing.T) {
	tests := []struct {
		name     string
		app      App
		expected string
	}{
		{
			name:     "custom container name",
			app:      App{Name: "my-app", ContainerName: sql.NullString{String: "custom-container", Valid: true}},
			expected: "custom-container",
		},
		{
			name:     "fallback to app name",
			app:      App{Name: "my-app", ContainerName: sql.NullString{Valid: false}},
			expected: "my-app",
		},
		{
			name:     "empty container name falls back",
			app:      App{Name: "my-app", ContainerName: sql.NullString{String: "", Valid: true}},
			expected: "my-app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.app.GetContainerName(); got != tt.expected {
				t.Errorf("GetContainerName() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestApp_GetImageName(t *testing.T) {
	tests := []struct {
		name     string
		app      App
		expected string
	}{
		{
			name:     "custom image name",
			app:      App{Name: "my-app", ImageName: sql.NullString{String: "custom-image", Valid: true}},
			expected: "custom-image",
		},
		{
			name:     "fallback to app name",
			app:      App{Name: "my-app", ImageName: sql.NullString{Valid: false}},
			expected: "my-app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.app.GetImageName(); got != tt.expected {
				t.Errorf("GetImageName() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestApp_GetSubdomain(t *testing.T) {
	tests := []struct {
		name     string
		app      App
		expected string
	}{
		{
			name:     "valid subdomain",
			app:      App{Subdomain: sql.NullString{String: "myapp", Valid: true}},
			expected: "myapp",
		},
		{
			name:     "null subdomain",
			app:      App{Subdomain: sql.NullString{Valid: false}},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.app.GetSubdomain(); got != tt.expected {
				t.Errorf("GetSubdomain() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestApp_GetPublicPort(t *testing.T) {
	tests := []struct {
		name     string
		app      App
		expected int
	}{
		{
			name:     "valid port",
			app:      App{PublicPort: sql.NullInt64{Int64: 8080, Valid: true}},
			expected: 8080,
		},
		{
			name:     "null port",
			app:      App{PublicPort: sql.NullInt64{Valid: false}},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.app.GetPublicPort(); got != tt.expected {
				t.Errorf("GetPublicPort() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestApp_LoadSaveEnvVars(t *testing.T) {
	app := &App{}

	// Test loading empty env vars
	err := app.LoadEnvVars()
	if err != nil {
		t.Errorf("LoadEnvVars() error = %v", err)
	}
	if len(app.EnvVars) != 0 {
		t.Errorf("Expected empty env vars, got %v", app.EnvVars)
	}

	// Test saving env vars
	app.EnvVars = map[string]string{
		"KEY1": "value1",
		"KEY2": "value2",
	}
	err = app.SaveEnvVars()
	if err != nil {
		t.Errorf("SaveEnvVars() error = %v", err)
	}
	if !app.EnvVarsJSON.Valid {
		t.Error("EnvVarsJSON should be valid after saving")
	}

	// Test loading saved env vars
	app2 := &App{EnvVarsJSON: app.EnvVarsJSON}
	err = app2.LoadEnvVars()
	if err != nil {
		t.Errorf("LoadEnvVars() error = %v", err)
	}
	if app2.EnvVars["KEY1"] != "value1" {
		t.Errorf("EnvVars[KEY1] = %v, want value1", app2.EnvVars["KEY1"])
	}
	if app2.EnvVars["KEY2"] != "value2" {
		t.Errorf("EnvVars[KEY2] = %v, want value2", app2.EnvVars["KEY2"])
	}
}

func TestApp_GetEnvVarsAsString(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		contains []string
	}{
		{
			name:     "empty env vars",
			envVars:  map[string]string{},
			contains: []string{},
		},
		{
			name:     "single env var",
			envVars:  map[string]string{"KEY": "value"},
			contains: []string{"KEY=value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &App{EnvVars: tt.envVars}
			result := app.GetEnvVarsAsString()
			for _, expected := range tt.contains {
				if len(expected) > 0 && !containsString(result, expected) {
					t.Errorf("GetEnvVarsAsString() = %v, should contain %v", result, expected)
				}
			}
		})
	}
}

func TestApp_ParseEnvVarsFromString(t *testing.T) {
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
		{
			name:     "multiple vars",
			input:    "KEY1=value1\nKEY2=value2",
			expected: map[string]string{"KEY1": "value1", "KEY2": "value2"},
		},
		{
			name:     "with comments",
			input:    "# comment\nKEY=value",
			expected: map[string]string{"KEY": "value"},
		},
		{
			name:     "with empty lines",
			input:    "KEY1=value1\n\nKEY2=value2",
			expected: map[string]string{"KEY1": "value1", "KEY2": "value2"},
		},
		{
			name:     "value with equals",
			input:    "KEY=value=with=equals",
			expected: map[string]string{"KEY": "value=with=equals"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &App{}
			app.ParseEnvVarsFromString(tt.input)
			for key, expectedValue := range tt.expected {
				if app.EnvVars[key] != expectedValue {
					t.Errorf("EnvVars[%s] = %v, want %v", key, app.EnvVars[key], expectedValue)
				}
			}
		})
	}
}

func TestBuildStrategy(t *testing.T) {
	if BuildStrategyDockerfile != "dockerfile" {
		t.Errorf("BuildStrategyDockerfile = %v, want dockerfile", BuildStrategyDockerfile)
	}
	if BuildStrategyCompose != "compose" {
		t.Errorf("BuildStrategyCompose = %v, want compose", BuildStrategyCompose)
	}
	if BuildStrategyBuildpacks != "buildpacks" {
		t.Errorf("BuildStrategyBuildpacks = %v, want buildpacks", BuildStrategyBuildpacks)
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestNullRawMessage_Scan(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected NullRawMessage
	}{
		{
			name:     "nil value",
			input:    nil,
			expected: nil,
		},
		{
			name:     "byte slice",
			input:    []byte(`{"key": "value"}`),
			expected: NullRawMessage(`{"key": "value"}`),
		},
		{
			name:     "string",
			input:    `{"key": "value"}`,
			expected: NullRawMessage(`{"key": "value"}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var n NullRawMessage
			err := n.Scan(tt.input)
			if err != nil {
				t.Errorf("Scan() error = %v", err)
			}
			if tt.expected == nil && n != nil {
				t.Errorf("Scan() = %v, want nil", n)
			}
			if tt.expected != nil && string(n) != string(tt.expected) {
				t.Errorf("Scan() = %v, want %v", string(n), string(tt.expected))
			}
		})
	}
}

func TestNullRawMessage_Value(t *testing.T) {
	tests := []struct {
		name     string
		input    NullRawMessage
		expected interface{}
	}{
		{
			name:     "nil value",
			input:    nil,
			expected: nil,
		},
		{
			name:     "with data",
			input:    NullRawMessage(`{"key": "value"}`),
			expected: []byte(`{"key": "value"}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.input.Value()
			if err != nil {
				t.Errorf("Value() error = %v", err)
			}
			if tt.expected == nil && got != nil {
				t.Errorf("Value() = %v, want nil", got)
			}
			if tt.expected != nil {
				gotBytes, ok := got.([]byte)
				if !ok {
					t.Errorf("Value() returned non-byte type")
				}
				expectedBytes := tt.expected.([]byte)
				if string(gotBytes) != string(expectedBytes) {
					t.Errorf("Value() = %v, want %v", string(gotBytes), string(expectedBytes))
				}
			}
		})
	}
}

func TestNullRawMessage_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    NullRawMessage
		expected string
	}{
		{
			name:     "nil value",
			input:    nil,
			expected: "null",
		},
		{
			name:     "with data",
			input:    NullRawMessage(`{"key":"value"}`),
			expected: `{"key":"value"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.input)
			if err != nil {
				t.Errorf("MarshalJSON() error = %v", err)
			}
			if string(got) != tt.expected {
				t.Errorf("MarshalJSON() = %v, want %v", string(got), tt.expected)
			}
		})
	}
}

func TestNullRawMessage_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected NullRawMessage
	}{
		{
			name:     "null value",
			input:    "null",
			expected: nil,
		},
		{
			name:     "with data",
			input:    `{"key":"value"}`,
			expected: NullRawMessage(`{"key":"value"}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var n NullRawMessage
			err := json.Unmarshal([]byte(tt.input), &n)
			if err != nil {
				t.Errorf("UnmarshalJSON() error = %v", err)
			}
			if tt.expected == nil && n != nil {
				t.Errorf("UnmarshalJSON() = %v, want nil", n)
			}
			if tt.expected != nil && string(n) != string(tt.expected) {
				t.Errorf("UnmarshalJSON() = %v, want %v", string(n), string(tt.expected))
			}
		})
	}
}
