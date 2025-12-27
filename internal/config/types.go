package config

import "time"

// Config represents the application configuration
type Config struct {
	Server      ServerConfig      `yaml:"server" mapstructure:"server"`
	Database    DatabaseConfig    `yaml:"database" mapstructure:"database"`
	Git         GitConfig         `yaml:"git" mapstructure:"git"`
	GitHubOAuth GitHubOAuthConfig `yaml:"github_oauth" mapstructure:"github_oauth"`
	Cloudflare  CloudflareConfig  `yaml:"cloudflare" mapstructure:"cloudflare"`
	Docker      DockerConfig      `yaml:"docker" mapstructure:"docker"`
	Apps        []AppConfig       `yaml:"apps" mapstructure:"apps"`
}

// ServerConfig holds HTTP server settings
type ServerConfig struct {
	Host      string `yaml:"host" mapstructure:"host"`
	Port      int    `yaml:"port" mapstructure:"port"`
	BaseURL   string `yaml:"base_url" mapstructure:"base_url"`
	SecretKey string `yaml:"secret_key" mapstructure:"secret_key"`
}

// DatabaseConfig holds database settings
type DatabaseConfig struct {
	Path string `yaml:"path" mapstructure:"path"`
}

// GitConfig holds git client settings
type GitConfig struct {
	WorkDir    string `yaml:"work_dir" mapstructure:"work_dir"`
	SSHKeyPath string `yaml:"ssh_key_path" mapstructure:"ssh_key_path"`
	Username   string `yaml:"username" mapstructure:"username"`
	Token      string `yaml:"token" mapstructure:"token"`
}

// GitHubOAuthConfig holds GitHub OAuth settings
type GitHubOAuthConfig struct {
	ClientID     string `yaml:"client_id" mapstructure:"client_id"`
	ClientSecret string `yaml:"client_secret" mapstructure:"client_secret"`
}

// CloudflareConfig holds Cloudflare Tunnel settings
type CloudflareConfig struct {
	TunnelToken string `yaml:"tunnel_token" mapstructure:"tunnel_token"`
	TunnelID    string `yaml:"tunnel_id" mapstructure:"tunnel_id"`
	Domain      string `yaml:"domain" mapstructure:"domain"` // e.g., "slats.dev"
}

// DockerConfig holds Docker client settings
type DockerConfig struct {
	Host           string        `yaml:"host" mapstructure:"host"`
	CleanupEnabled bool          `yaml:"cleanup_enabled" mapstructure:"cleanup_enabled"`
	KeepImageCount int           `yaml:"keep_image_count" mapstructure:"keep_image_count"`
	BuildTimeout   time.Duration `yaml:"build_timeout" mapstructure:"build_timeout"`
}

// AppConfig defines an application to deploy
type AppConfig struct {
	Name           string            `yaml:"name" mapstructure:"name"`
	Description    string            `yaml:"description" mapstructure:"description"`
	RepoURL        string            `yaml:"repo_url" mapstructure:"repo_url"`
	Branch         string            `yaml:"branch" mapstructure:"branch"`
	WebhookSecret  string            `yaml:"webhook_secret" mapstructure:"webhook_secret"`
	BuildStrategy  string            `yaml:"build_strategy" mapstructure:"build_strategy"`
	DockerfilePath string            `yaml:"dockerfile_path" mapstructure:"dockerfile_path"`
	ComposeFile    string            `yaml:"compose_file" mapstructure:"compose_file"`
	BuildContext   string            `yaml:"build_context" mapstructure:"build_context"`
	ContainerName  string            `yaml:"container_name" mapstructure:"container_name"`
	ImageName      string            `yaml:"image_name" mapstructure:"image_name"`
	Ports          map[string]string `yaml:"ports" mapstructure:"ports"`
	Volumes        map[string]string `yaml:"volumes" mapstructure:"volumes"`
	EnvVars        map[string]string `yaml:"env_vars" mapstructure:"env_vars"`
	Networks       []string          `yaml:"networks" mapstructure:"networks"`
	AutoDeploy     bool              `yaml:"auto_deploy" mapstructure:"auto_deploy"`
}

// Default returns a Config with default values
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 7123,
		},
		Database: DatabaseConfig{
			Path: "./data/homelab-cd.db",
		},
		Git: GitConfig{
			WorkDir: "./data/repos",
		},
		Docker: DockerConfig{
			CleanupEnabled: true,
			KeepImageCount: 5,
			BuildTimeout:   30 * time.Minute,
		},
	}
}
