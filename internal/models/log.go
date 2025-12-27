package models

import "time"

// LogLevel represents the severity of a log entry
type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

// LogSource indicates what generated the log
type LogSource string

const (
	LogSourceGit    LogSource = "git"
	LogSourceDocker LogSource = "docker"
	LogSourceDeploy LogSource = "deploy"
	LogSourceSystem LogSource = "system"
)

// BuildLog represents a single log entry for a build
type BuildLog struct {
	ID        int64     `db:"id" json:"id"`
	BuildID   string    `db:"build_id" json:"build_id"`
	Timestamp time.Time `db:"timestamp" json:"timestamp"`
	Level     LogLevel  `db:"level" json:"level"`
	Message   string    `db:"message" json:"message"`
	Source    LogSource `db:"source" json:"source,omitempty"`
}

// Deployment represents a container deployment
type Deployment struct {
	ID            string    `db:"id" json:"id"`
	AppID         string    `db:"app_id" json:"app_id"`
	BuildID       string    `db:"build_id" json:"build_id,omitempty"`
	ContainerID   string    `db:"container_id" json:"container_id,omitempty"`
	ContainerName string    `db:"container_name" json:"container_name"`
	ImageTag      string    `db:"image_tag" json:"image_tag"`
	Status        string    `db:"status" json:"status"`
	Ports         string    `db:"ports" json:"ports,omitempty"`
	DeployedAt    time.Time `db:"deployed_at" json:"deployed_at"`
	StoppedAt     *time.Time `db:"stopped_at" json:"stopped_at,omitempty"`
}

// DeploymentStatus constants
const (
	DeploymentStatusRunning = "running"
	DeploymentStatusStopped = "stopped"
	DeploymentStatusFailed  = "failed"
	DeploymentStatusRemoved = "removed"
)
