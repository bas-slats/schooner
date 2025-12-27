package models

import (
	"database/sql"
	"testing"
	"time"
)

func TestBuild_GetCommitSHA(t *testing.T) {
	tests := []struct {
		name     string
		build    Build
		expected string
	}{
		{
			name:     "valid SHA",
			build:    Build{CommitSHA: sql.NullString{String: "abc123def456", Valid: true}},
			expected: "abc123def456",
		},
		{
			name:     "null SHA",
			build:    Build{CommitSHA: sql.NullString{Valid: false}},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.build.GetCommitSHA(); got != tt.expected {
				t.Errorf("GetCommitSHA() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBuild_GetShortSHA(t *testing.T) {
	tests := []struct {
		name     string
		build    Build
		expected string
	}{
		{
			name:     "long SHA",
			build:    Build{CommitSHA: sql.NullString{String: "abc123def456789", Valid: true}},
			expected: "abc123de",
		},
		{
			name:     "short SHA",
			build:    Build{CommitSHA: sql.NullString{String: "abc", Valid: true}},
			expected: "abc",
		},
		{
			name:     "null SHA",
			build:    Build{CommitSHA: sql.NullString{Valid: false}},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.build.GetShortSHA(); got != tt.expected {
				t.Errorf("GetShortSHA() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBuild_GetCommitMessage(t *testing.T) {
	tests := []struct {
		name     string
		build    Build
		expected string
	}{
		{
			name:     "valid message",
			build:    Build{CommitMessage: sql.NullString{String: "Fix bug", Valid: true}},
			expected: "Fix bug",
		},
		{
			name:     "null message",
			build:    Build{CommitMessage: sql.NullString{Valid: false}},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.build.GetCommitMessage(); got != tt.expected {
				t.Errorf("GetCommitMessage() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBuild_GetBranch(t *testing.T) {
	tests := []struct {
		name     string
		build    Build
		expected string
	}{
		{
			name:     "valid branch",
			build:    Build{Branch: sql.NullString{String: "main", Valid: true}},
			expected: "main",
		},
		{
			name:     "null branch",
			build:    Build{Branch: sql.NullString{Valid: false}},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.build.GetBranch(); got != tt.expected {
				t.Errorf("GetBranch() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBuild_GetImageTag(t *testing.T) {
	tests := []struct {
		name     string
		build    Build
		expected string
	}{
		{
			name:     "valid tag",
			build:    Build{ImageTag: sql.NullString{String: "v1.0.0", Valid: true}},
			expected: "v1.0.0",
		},
		{
			name:     "null tag",
			build:    Build{ImageTag: sql.NullString{Valid: false}},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.build.GetImageTag(); got != tt.expected {
				t.Errorf("GetImageTag() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBuild_GetErrorMessage(t *testing.T) {
	tests := []struct {
		name     string
		build    Build
		expected string
	}{
		{
			name:     "valid error",
			build:    Build{ErrorMessage: sql.NullString{String: "Build failed", Valid: true}},
			expected: "Build failed",
		},
		{
			name:     "null error",
			build:    Build{ErrorMessage: sql.NullString{Valid: false}},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.build.GetErrorMessage(); got != tt.expected {
				t.Errorf("GetErrorMessage() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBuild_Duration(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		build       Build
		minDuration time.Duration
		maxDuration time.Duration
	}{
		{
			name:        "no start time",
			build:       Build{StartedAt: sql.NullTime{Valid: false}},
			minDuration: 0,
			maxDuration: 0,
		},
		{
			name: "completed build",
			build: Build{
				StartedAt:  sql.NullTime{Time: now.Add(-10 * time.Minute), Valid: true},
				FinishedAt: sql.NullTime{Time: now.Add(-5 * time.Minute), Valid: true},
			},
			minDuration: 5 * time.Minute,
			maxDuration: 5 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.build.Duration()
			if got < tt.minDuration || got > tt.maxDuration {
				t.Errorf("Duration() = %v, want between %v and %v", got, tt.minDuration, tt.maxDuration)
			}
		})
	}
}

func TestBuild_IsRunning(t *testing.T) {
	tests := []struct {
		name     string
		status   BuildStatus
		expected bool
	}{
		{name: "pending", status: BuildStatusPending, expected: true},
		{name: "cloning", status: BuildStatusCloning, expected: true},
		{name: "building", status: BuildStatusBuilding, expected: true},
		{name: "pushing", status: BuildStatusPushing, expected: true},
		{name: "deploying", status: BuildStatusDeploying, expected: true},
		{name: "success", status: BuildStatusSuccess, expected: false},
		{name: "failed", status: BuildStatusFailed, expected: false},
		{name: "cancelled", status: BuildStatusCancelled, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			build := Build{Status: tt.status}
			if got := build.IsRunning(); got != tt.expected {
				t.Errorf("IsRunning() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBuild_IsComplete(t *testing.T) {
	tests := []struct {
		name     string
		status   BuildStatus
		expected bool
	}{
		{name: "pending", status: BuildStatusPending, expected: false},
		{name: "cloning", status: BuildStatusCloning, expected: false},
		{name: "building", status: BuildStatusBuilding, expected: false},
		{name: "pushing", status: BuildStatusPushing, expected: false},
		{name: "deploying", status: BuildStatusDeploying, expected: false},
		{name: "success", status: BuildStatusSuccess, expected: true},
		{name: "failed", status: BuildStatusFailed, expected: true},
		{name: "cancelled", status: BuildStatusCancelled, expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			build := Build{Status: tt.status}
			if got := build.IsComplete(); got != tt.expected {
				t.Errorf("IsComplete() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBuildStatusConstants(t *testing.T) {
	if BuildStatusPending != "pending" {
		t.Errorf("BuildStatusPending = %v, want pending", BuildStatusPending)
	}
	if BuildStatusCloning != "cloning" {
		t.Errorf("BuildStatusCloning = %v, want cloning", BuildStatusCloning)
	}
	if BuildStatusBuilding != "building" {
		t.Errorf("BuildStatusBuilding = %v, want building", BuildStatusBuilding)
	}
	if BuildStatusPushing != "pushing" {
		t.Errorf("BuildStatusPushing = %v, want pushing", BuildStatusPushing)
	}
	if BuildStatusDeploying != "deploying" {
		t.Errorf("BuildStatusDeploying = %v, want deploying", BuildStatusDeploying)
	}
	if BuildStatusSuccess != "success" {
		t.Errorf("BuildStatusSuccess = %v, want success", BuildStatusSuccess)
	}
	if BuildStatusFailed != "failed" {
		t.Errorf("BuildStatusFailed = %v, want failed", BuildStatusFailed)
	}
	if BuildStatusCancelled != "cancelled" {
		t.Errorf("BuildStatusCancelled = %v, want cancelled", BuildStatusCancelled)
	}
}

func TestBuildTriggerConstants(t *testing.T) {
	if TriggerWebhook != "webhook" {
		t.Errorf("TriggerWebhook = %v, want webhook", TriggerWebhook)
	}
	if TriggerManual != "manual" {
		t.Errorf("TriggerManual = %v, want manual", TriggerManual)
	}
	if TriggerRollback != "rollback" {
		t.Errorf("TriggerRollback = %v, want rollback", TriggerRollback)
	}
}
