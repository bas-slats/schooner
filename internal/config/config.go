package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Load reads configuration from file and environment variables
func Load() (*Config, error) {
	v := viper.New()

	// Set defaults
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8080)
	v.SetDefault("database.path", "./data/schooner.db")
	v.SetDefault("git.work_dir", "./data/repos")
	v.SetDefault("docker.cleanup_enabled", true)
	v.SetDefault("docker.keep_image_count", 5)
	v.SetDefault("docker.build_timeout", "30m")

	// Config file settings
	v.SetConfigName("config")
	v.SetConfigType("yaml")

	// Search paths for config
	v.AddConfigPath(".")
	v.AddConfigPath("./config")
	v.AddConfigPath("/app/config")
	v.AddConfigPath("/etc/homelab-cd")

	// Also check HOMELAB_CD_CONFIG env var
	if configPath := os.Getenv("HOMELAB_CD_CONFIG"); configPath != "" {
		v.SetConfigFile(configPath)
	}

	// Environment variable settings
	v.SetEnvPrefix("HOMELAB_CD")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Read config file (optional)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
		// Config file not found is okay, we'll use defaults and env vars
	}

	// Parse config into struct
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Expand environment variables in sensitive fields
	cfg.Server.SecretKey = expandEnv(cfg.Server.SecretKey)
	cfg.Git.Token = expandEnv(cfg.Git.Token)
	cfg.Git.SSHKeyPath = expandEnv(cfg.Git.SSHKeyPath)

	for i := range cfg.Apps {
		cfg.Apps[i].WebhookSecret = expandEnv(cfg.Apps[i].WebhookSecret)
		for k, v := range cfg.Apps[i].EnvVars {
			cfg.Apps[i].EnvVars[k] = expandEnv(v)
		}
	}

	// Parse duration if string
	if cfg.Docker.BuildTimeout == 0 {
		if timeout := v.GetString("docker.build_timeout"); timeout != "" {
			d, err := time.ParseDuration(timeout)
			if err != nil {
				return nil, fmt.Errorf("invalid build_timeout: %w", err)
			}
			cfg.Docker.BuildTimeout = d
		}
	}

	// Set app defaults
	for i := range cfg.Apps {
		if cfg.Apps[i].Branch == "" {
			cfg.Apps[i].Branch = "main"
		}
		if cfg.Apps[i].BuildStrategy == "" {
			cfg.Apps[i].BuildStrategy = "dockerfile"
		}
		if cfg.Apps[i].DockerfilePath == "" {
			cfg.Apps[i].DockerfilePath = "Dockerfile"
		}
		if cfg.Apps[i].ComposeFile == "" {
			cfg.Apps[i].ComposeFile = "docker-compose.yaml"
		}
		if cfg.Apps[i].BuildContext == "" {
			cfg.Apps[i].BuildContext = "."
		}
	}

	// Validate config
	if err := validate(&cfg); err != nil {
		return nil, err
	}

	// Ensure directories exist
	if err := ensureDirs(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// expandEnv expands ${VAR} or $VAR in string
func expandEnv(s string) string {
	return os.ExpandEnv(s)
}

// validate checks config for required fields and valid values
func validate(cfg *Config) error {
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", cfg.Server.Port)
	}

	for i, app := range cfg.Apps {
		if app.Name == "" {
			return fmt.Errorf("app[%d]: name is required", i)
		}
		if app.RepoURL == "" {
			return fmt.Errorf("app[%d] %q: repo_url is required", i, app.Name)
		}
		switch app.BuildStrategy {
		case "dockerfile", "compose", "autodetect":
			// valid
		default:
			return fmt.Errorf("app[%d] %q: invalid build_strategy %q", i, app.Name, app.BuildStrategy)
		}
	}

	return nil
}

// ensureDirs creates necessary directories
func ensureDirs(cfg *Config) error {
	dirs := []string{
		filepath.Dir(cfg.Database.Path),
		cfg.Git.WorkDir,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}
