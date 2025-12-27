package docker

import (
	"testing"
)

func TestContainerConfig(t *testing.T) {
	cfg := ContainerConfig{
		Name:          "test-container",
		Image:         "nginx:latest",
		Cmd:           []string{"nginx", "-g", "daemon off;"},
		Env:           []string{"ENV=test"},
		Ports:         map[string]string{"80": "8080"},
		Volumes:       map[string]string{"/host/path": "/container/path"},
		Networks:      []string{"my-network"},
		NetworkMode:   "bridge",
		RestartPolicy: "unless-stopped",
		Labels:        map[string]string{"app": "test"},
	}

	if cfg.Name != "test-container" {
		t.Errorf("Name = %v, want test-container", cfg.Name)
	}
	if cfg.Image != "nginx:latest" {
		t.Errorf("Image = %v, want nginx:latest", cfg.Image)
	}
	if len(cfg.Cmd) != 3 {
		t.Errorf("len(Cmd) = %v, want 3", len(cfg.Cmd))
	}
	if len(cfg.Env) != 1 {
		t.Errorf("len(Env) = %v, want 1", len(cfg.Env))
	}
	if cfg.NetworkMode != "bridge" {
		t.Errorf("NetworkMode = %v, want bridge", cfg.NetworkMode)
	}
	if cfg.RestartPolicy != "unless-stopped" {
		t.Errorf("RestartPolicy = %v, want unless-stopped", cfg.RestartPolicy)
	}
}

func TestContainerStatus(t *testing.T) {
	status := ContainerStatus{
		ID:        "abc123",
		Name:      "test-container",
		State:     "running",
		Status:    "Up 5 minutes",
		Health:    "healthy",
		StartedAt: "2024-01-01T00:00:00Z",
		Ports:     map[string]string{"80/tcp": "8080"},
		Image:     "nginx:latest",
		CreatedAt: "2024-01-01T00:00:00Z",
	}

	if status.ID != "abc123" {
		t.Errorf("ID = %v, want abc123", status.ID)
	}
	if status.Name != "test-container" {
		t.Errorf("Name = %v, want test-container", status.Name)
	}
	if status.State != "running" {
		t.Errorf("State = %v, want running", status.State)
	}
	if status.Health != "healthy" {
		t.Errorf("Health = %v, want healthy", status.Health)
	}
	if status.Image != "nginx:latest" {
		t.Errorf("Image = %v, want nginx:latest", status.Image)
	}
}

func TestToPortBindings(t *testing.T) {
	ports := map[string]string{
		"80":   "8080",
		"443":  "8443",
		"3000": "3000",
	}

	portMap := toPortBindings(ports)

	if len(portMap) != 3 {
		t.Errorf("len(portMap) = %v, want 3", len(portMap))
	}

	// Check 80/tcp mapping
	bindings, ok := portMap["80/tcp"]
	if !ok {
		t.Error("Expected 80/tcp binding to exist")
	}
	if len(bindings) != 1 || bindings[0].HostPort != "8080" {
		t.Errorf("80/tcp binding = %v, want [{HostPort:8080}]", bindings)
	}

	// Check 443/tcp mapping
	bindings, ok = portMap["443/tcp"]
	if !ok {
		t.Error("Expected 443/tcp binding to exist")
	}
	if len(bindings) != 1 || bindings[0].HostPort != "8443" {
		t.Errorf("443/tcp binding = %v, want [{HostPort:8443}]", bindings)
	}
}

func TestToBinds(t *testing.T) {
	volumes := map[string]string{
		"/host/path1": "/container/path1",
		"/host/path2": "/container/path2",
	}

	binds := toBinds(volumes)

	if len(binds) != 2 {
		t.Errorf("len(binds) = %v, want 2", len(binds))
	}

	found1 := false
	found2 := false
	for _, bind := range binds {
		if bind == "/host/path1:/container/path1" {
			found1 = true
		}
		if bind == "/host/path2:/container/path2" {
			found2 = true
		}
	}

	if !found1 {
		t.Error("Expected /host/path1:/container/path1 in binds")
	}
	if !found2 {
		t.Error("Expected /host/path2:/container/path2 in binds")
	}
}

func TestExtractPorts(t *testing.T) {
	// Test with nil port map
	ports := extractPorts(nil)
	if len(ports) != 0 {
		t.Errorf("Expected empty map for nil input, got %v", ports)
	}
}

func TestToPortBindingsEmpty(t *testing.T) {
	ports := map[string]string{}
	portMap := toPortBindings(ports)
	if len(portMap) != 0 {
		t.Errorf("Expected empty port map, got %v", portMap)
	}
}

func TestToBindsEmpty(t *testing.T) {
	volumes := map[string]string{}
	binds := toBinds(volumes)
	if len(binds) != 0 {
		t.Errorf("Expected empty binds, got %v", binds)
	}
}
