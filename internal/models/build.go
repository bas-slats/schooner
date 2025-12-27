package models

import (
	"database/sql"
	"time"
)

// BuildStatus represents the current state of a build
type BuildStatus string

const (
	BuildStatusPending   BuildStatus = "pending"
	BuildStatusCloning   BuildStatus = "cloning"
	BuildStatusBuilding  BuildStatus = "building"
	BuildStatusPushing   BuildStatus = "pushing"
	BuildStatusDeploying BuildStatus = "deploying"
	BuildStatusSuccess   BuildStatus = "success"
	BuildStatusFailed    BuildStatus = "failed"
	BuildStatusCancelled BuildStatus = "cancelled"
)

// BuildTrigger indicates what initiated the build
type BuildTrigger string

const (
	TriggerWebhook  BuildTrigger = "webhook"
	TriggerManual   BuildTrigger = "manual"
	TriggerRollback BuildTrigger = "rollback"
)

// Build represents a build execution
type Build struct {
	ID            string         `db:"id" json:"id"`
	AppID         string         `db:"app_id" json:"app_id"`
	Status        BuildStatus    `db:"status" json:"status"`
	Trigger       BuildTrigger   `db:"trigger" json:"trigger"`
	CommitSHA     sql.NullString `db:"commit_sha" json:"commit_sha"`
	CommitMessage sql.NullString `db:"commit_message" json:"commit_message"`
	CommitAuthor  sql.NullString `db:"commit_author" json:"commit_author"`
	Branch        sql.NullString `db:"branch" json:"branch"`
	ImageTag      sql.NullString `db:"image_tag" json:"image_tag"`
	ErrorMessage  sql.NullString `db:"error_message" json:"error_message,omitempty"`
	StartedAt     sql.NullTime   `db:"started_at" json:"started_at,omitempty"`
	FinishedAt    sql.NullTime   `db:"finished_at" json:"finished_at,omitempty"`
	CreatedAt     time.Time      `db:"created_at" json:"created_at"`

	// Joined fields (not in DB)
	AppName string `db:"app_name" json:"app_name,omitempty"`
}

// GetCommitSHA returns commit SHA or empty string
func (b *Build) GetCommitSHA() string {
	if b.CommitSHA.Valid {
		return b.CommitSHA.String
	}
	return ""
}

// GetShortSHA returns first 8 chars of commit SHA
func (b *Build) GetShortSHA() string {
	sha := b.GetCommitSHA()
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// GetCommitMessage returns commit message or empty string
func (b *Build) GetCommitMessage() string {
	if b.CommitMessage.Valid {
		return b.CommitMessage.String
	}
	return ""
}

// GetBranch returns branch or empty string
func (b *Build) GetBranch() string {
	if b.Branch.Valid {
		return b.Branch.String
	}
	return ""
}

// GetImageTag returns image tag or empty string
func (b *Build) GetImageTag() string {
	if b.ImageTag.Valid {
		return b.ImageTag.String
	}
	return ""
}

// GetErrorMessage returns error message or empty string
func (b *Build) GetErrorMessage() string {
	if b.ErrorMessage.Valid {
		return b.ErrorMessage.String
	}
	return ""
}

// Duration returns build duration if completed
func (b *Build) Duration() time.Duration {
	if !b.StartedAt.Valid {
		return 0
	}
	end := time.Now()
	if b.FinishedAt.Valid {
		end = b.FinishedAt.Time
	}
	return end.Sub(b.StartedAt.Time)
}

// IsRunning returns true if build is in progress
func (b *Build) IsRunning() bool {
	switch b.Status {
	case BuildStatusPending, BuildStatusCloning, BuildStatusBuilding, BuildStatusPushing, BuildStatusDeploying:
		return true
	}
	return false
}

// IsComplete returns true if build has finished
func (b *Build) IsComplete() bool {
	switch b.Status {
	case BuildStatusSuccess, BuildStatusFailed, BuildStatusCancelled:
		return true
	}
	return false
}
