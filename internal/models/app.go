package models

import (
	"database/sql"
	"encoding/json"
	"time"
)

// BuildStrategy represents the build method for an app
type BuildStrategy string

const (
	BuildStrategyDockerfile BuildStrategy = "dockerfile"
	BuildStrategyCompose    BuildStrategy = "compose"
	BuildStrategyBuildpacks BuildStrategy = "buildpacks"
)

// App represents an application configured for deployment
type App struct {
	ID             string            `db:"id" json:"id"`
	Name           string            `db:"name" json:"name"`
	Description    sql.NullString    `db:"description" json:"description"`
	RepoURL        string            `db:"repo_url" json:"repo_url"`
	Branch         string            `db:"branch" json:"branch"`
	WebhookSecret  sql.NullString    `db:"webhook_secret" json:"-"`
	BuildStrategy  BuildStrategy     `db:"build_strategy" json:"build_strategy"`
	DockerfilePath string            `db:"dockerfile_path" json:"dockerfile_path"`
	ComposeFile    string            `db:"compose_file" json:"compose_file"`
	BuildContext   string            `db:"build_context" json:"build_context"`
	ContainerName  sql.NullString    `db:"container_name" json:"container_name"`
	ImageName      sql.NullString    `db:"image_name" json:"image_name"`
	DeployConfig   json.RawMessage   `db:"deploy_config" json:"deploy_config,omitempty"`
	EnvVarsJSON    sql.NullString    `db:"env_vars" json:"-"`
	EnvVars        map[string]string `db:"-" json:"env_vars,omitempty"`
	AutoDeploy     bool              `db:"auto_deploy" json:"auto_deploy"`
	Enabled        bool              `db:"enabled" json:"enabled"`
	CreatedAt      time.Time         `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time         `db:"updated_at" json:"updated_at"`
}

// GetDescription returns description or empty string
func (a *App) GetDescription() string {
	if a.Description.Valid {
		return a.Description.String
	}
	return ""
}

// GetContainerName returns container name or app name as fallback
func (a *App) GetContainerName() string {
	if a.ContainerName.Valid && a.ContainerName.String != "" {
		return a.ContainerName.String
	}
	return a.Name
}

// GetImageName returns image name or app name as fallback
func (a *App) GetImageName() string {
	if a.ImageName.Valid && a.ImageName.String != "" {
		return a.ImageName.String
	}
	return a.Name
}

// GetWebhookSecret returns webhook secret or empty string
func (a *App) GetWebhookSecret() string {
	if a.WebhookSecret.Valid {
		return a.WebhookSecret.String
	}
	return ""
}

// LoadEnvVars parses the JSON env vars into the map
func (a *App) LoadEnvVars() error {
	if !a.EnvVarsJSON.Valid || a.EnvVarsJSON.String == "" {
		a.EnvVars = make(map[string]string)
		return nil
	}
	return json.Unmarshal([]byte(a.EnvVarsJSON.String), &a.EnvVars)
}

// SaveEnvVars serializes env vars map to JSON
func (a *App) SaveEnvVars() error {
	if len(a.EnvVars) == 0 {
		a.EnvVarsJSON = sql.NullString{Valid: false}
		return nil
	}
	b, err := json.Marshal(a.EnvVars)
	if err != nil {
		return err
	}
	a.EnvVarsJSON = sql.NullString{String: string(b), Valid: true}
	return nil
}
