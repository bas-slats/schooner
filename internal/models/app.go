package models

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"time"
)

// NullRawMessage is a json.RawMessage that handles NULL values from the database
type NullRawMessage json.RawMessage

// Scan implements the sql.Scanner interface
func (n *NullRawMessage) Scan(value interface{}) error {
	if value == nil {
		*n = nil
		return nil
	}
	switch v := value.(type) {
	case []byte:
		*n = NullRawMessage(v)
	case string:
		*n = NullRawMessage(v)
	}
	return nil
}

// Value implements the driver.Valuer interface
func (n NullRawMessage) Value() (driver.Value, error) {
	if n == nil {
		return nil, nil
	}
	return []byte(n), nil
}

// MarshalJSON implements json.Marshaler
func (n NullRawMessage) MarshalJSON() ([]byte, error) {
	if n == nil {
		return []byte("null"), nil
	}
	return json.RawMessage(n).MarshalJSON()
}

// UnmarshalJSON implements json.Unmarshaler
func (n *NullRawMessage) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*n = nil
		return nil
	}
	*n = NullRawMessage(data)
	return nil
}

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
	DeployConfig   NullRawMessage    `db:"deploy_config" json:"deploy_config,omitempty"`
	EnvVarsJSON    sql.NullString    `db:"env_vars" json:"-"`
	EnvVars        map[string]string `db:"-" json:"env_vars,omitempty"`
	AutoDeploy     bool              `db:"auto_deploy" json:"auto_deploy"`
	Enabled        bool              `db:"enabled" json:"enabled"`
	Subdomain      sql.NullString    `db:"subdomain" json:"subdomain"`      // e.g., "myapp" for myapp.slats.dev
	PublicPort     sql.NullInt64     `db:"public_port" json:"public_port"` // Port to expose via tunnel
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

// SetWebhookSecret sets the webhook secret
func (a *App) SetWebhookSecret(secret string) {
	a.WebhookSecret = sql.NullString{String: secret, Valid: secret != ""}
}

// GetSubdomain returns subdomain or empty string
func (a *App) GetSubdomain() string {
	if a.Subdomain.Valid {
		return a.Subdomain.String
	}
	return ""
}

// GetPublicPort returns public port or 0
func (a *App) GetPublicPort() int {
	if a.PublicPort.Valid {
		return int(a.PublicPort.Int64)
	}
	return 0
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

// GetEnvVarsAsString returns env vars as KEY=value lines
func (a *App) GetEnvVarsAsString() string {
	if len(a.EnvVars) == 0 {
		return ""
	}
	var lines string
	for k, v := range a.EnvVars {
		if lines != "" {
			lines += "\n"
		}
		lines += k + "=" + v
	}
	return lines
}

// ParseEnvVarsFromString parses KEY=value lines into env vars map
func (a *App) ParseEnvVarsFromString(s string) {
	a.EnvVars = make(map[string]string)
	if s == "" {
		return
	}
	lines := splitLines(s)
	for _, line := range lines {
		line = trimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		idx := indexOf(line, '=')
		if idx > 0 {
			key := trimSpace(line[:idx])
			value := ""
			if idx < len(line)-1 {
				value = line[idx+1:]
			}
			a.EnvVars[key] = value
		}
	}
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

func trimSpace(s string) string {
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

func indexOf(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
