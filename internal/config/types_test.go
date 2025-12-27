package config

import (
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	// Test server defaults
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("Server.Host = %v, want 0.0.0.0", cfg.Server.Host)
	}
	if cfg.Server.Port != 7123 {
		t.Errorf("Server.Port = %v, want 7123", cfg.Server.Port)
	}

	// Test database defaults
	if cfg.Database.Path != "/data/homelab-cd.db" {
		t.Errorf("Database.Path = %v, want /data/homelab-cd.db", cfg.Database.Path)
	}

	// Test git defaults
	if cfg.Git.WorkDir != "/data/repos" {
		t.Errorf("Git.WorkDir = %v, want /data/repos", cfg.Git.WorkDir)
	}

	// Test docker defaults
	if cfg.Docker.CleanupEnabled != true {
		t.Errorf("Docker.CleanupEnabled = %v, want true", cfg.Docker.CleanupEnabled)
	}
	if cfg.Docker.KeepImageCount != 5 {
		t.Errorf("Docker.KeepImageCount = %v, want 5", cfg.Docker.KeepImageCount)
	}
	if cfg.Docker.BuildTimeout != 30*time.Minute {
		t.Errorf("Docker.BuildTimeout = %v, want 30m", cfg.Docker.BuildTimeout)
	}
}

func TestServerConfig(t *testing.T) {
	cfg := ServerConfig{
		Host:      "localhost",
		Port:      8080,
		BaseURL:   "http://localhost:8080",
		SecretKey: "secret",
	}

	if cfg.Host != "localhost" {
		t.Errorf("Host = %v, want localhost", cfg.Host)
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %v, want 8080", cfg.Port)
	}
	if cfg.BaseURL != "http://localhost:8080" {
		t.Errorf("BaseURL = %v, want http://localhost:8080", cfg.BaseURL)
	}
	if cfg.SecretKey != "secret" {
		t.Errorf("SecretKey = %v, want secret", cfg.SecretKey)
	}
}

func TestDatabaseConfig(t *testing.T) {
	cfg := DatabaseConfig{
		Path: "/data/test.db",
	}

	if cfg.Path != "/data/test.db" {
		t.Errorf("Path = %v, want /data/test.db", cfg.Path)
	}
}

func TestGitConfig(t *testing.T) {
	cfg := GitConfig{
		WorkDir:    "/repos",
		SSHKeyPath: "/ssh/id_rsa",
		Username:   "user",
		Token:      "token",
	}

	if cfg.WorkDir != "/repos" {
		t.Errorf("WorkDir = %v, want /repos", cfg.WorkDir)
	}
	if cfg.SSHKeyPath != "/ssh/id_rsa" {
		t.Errorf("SSHKeyPath = %v, want /ssh/id_rsa", cfg.SSHKeyPath)
	}
	if cfg.Username != "user" {
		t.Errorf("Username = %v, want user", cfg.Username)
	}
	if cfg.Token != "token" {
		t.Errorf("Token = %v, want token", cfg.Token)
	}
}

func TestGitHubOAuthConfig(t *testing.T) {
	cfg := GitHubOAuthConfig{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
	}

	if cfg.ClientID != "client-id" {
		t.Errorf("ClientID = %v, want client-id", cfg.ClientID)
	}
	if cfg.ClientSecret != "client-secret" {
		t.Errorf("ClientSecret = %v, want client-secret", cfg.ClientSecret)
	}
}

func TestCloudflareConfig(t *testing.T) {
	cfg := CloudflareConfig{
		TunnelToken: "token",
		TunnelID:    "tunnel-id",
		Domain:      "example.com",
	}

	if cfg.TunnelToken != "token" {
		t.Errorf("TunnelToken = %v, want token", cfg.TunnelToken)
	}
	if cfg.TunnelID != "tunnel-id" {
		t.Errorf("TunnelID = %v, want tunnel-id", cfg.TunnelID)
	}
	if cfg.Domain != "example.com" {
		t.Errorf("Domain = %v, want example.com", cfg.Domain)
	}
}

func TestDockerConfig(t *testing.T) {
	cfg := DockerConfig{
		Host:           "unix:///var/run/docker.sock",
		CleanupEnabled: true,
		KeepImageCount: 10,
		BuildTimeout:   15 * time.Minute,
	}

	if cfg.Host != "unix:///var/run/docker.sock" {
		t.Errorf("Host = %v, want unix:///var/run/docker.sock", cfg.Host)
	}
	if cfg.CleanupEnabled != true {
		t.Errorf("CleanupEnabled = %v, want true", cfg.CleanupEnabled)
	}
	if cfg.KeepImageCount != 10 {
		t.Errorf("KeepImageCount = %v, want 10", cfg.KeepImageCount)
	}
	if cfg.BuildTimeout != 15*time.Minute {
		t.Errorf("BuildTimeout = %v, want 15m", cfg.BuildTimeout)
	}
}

func TestAppConfig(t *testing.T) {
	cfg := AppConfig{
		Name:           "my-app",
		Description:    "Test app",
		RepoURL:        "https://github.com/user/repo.git",
		Branch:         "main",
		WebhookSecret:  "secret",
		BuildStrategy:  "dockerfile",
		DockerfilePath: "Dockerfile",
		ComposeFile:    "docker-compose.yaml",
		BuildContext:   ".",
		ContainerName:  "my-container",
		ImageName:      "my-image",
		Ports:          map[string]string{"8080": "80"},
		Volumes:        map[string]string{"/data": "/app/data"},
		EnvVars:        map[string]string{"ENV": "prod"},
		Networks:       []string{"my-network"},
		AutoDeploy:     true,
	}

	if cfg.Name != "my-app" {
		t.Errorf("Name = %v, want my-app", cfg.Name)
	}
	if cfg.Description != "Test app" {
		t.Errorf("Description = %v, want Test app", cfg.Description)
	}
	if cfg.RepoURL != "https://github.com/user/repo.git" {
		t.Errorf("RepoURL = %v, want https://github.com/user/repo.git", cfg.RepoURL)
	}
	if cfg.Branch != "main" {
		t.Errorf("Branch = %v, want main", cfg.Branch)
	}
	if cfg.BuildStrategy != "dockerfile" {
		t.Errorf("BuildStrategy = %v, want dockerfile", cfg.BuildStrategy)
	}
	if cfg.AutoDeploy != true {
		t.Errorf("AutoDeploy = %v, want true", cfg.AutoDeploy)
	}
	if len(cfg.Ports) != 1 {
		t.Errorf("len(Ports) = %v, want 1", len(cfg.Ports))
	}
	if len(cfg.EnvVars) != 1 {
		t.Errorf("len(EnvVars) = %v, want 1", len(cfg.EnvVars))
	}
}
