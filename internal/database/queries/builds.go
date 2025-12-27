package queries

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jmoiron/sqlx"

	"schooner/internal/models"
)

// BuildQueries provides database operations for builds
type BuildQueries struct {
	db *sqlx.DB
}

// NewBuildQueries creates a new BuildQueries instance
func NewBuildQueries(db *sqlx.DB) *BuildQueries {
	return &BuildQueries{db: db}
}

// Create inserts a new build
func (q *BuildQueries) Create(ctx context.Context, build *models.Build) error {
	query := `
		INSERT INTO builds (
			id, app_id, status, trigger, commit_sha, commit_message,
			commit_author, branch, image_tag, error_message,
			started_at, finished_at, created_at
		) VALUES (
			:id, :app_id, :status, :trigger, :commit_sha, :commit_message,
			:commit_author, :branch, :image_tag, :error_message,
			:started_at, :finished_at, :created_at
		)`

	_, err := q.db.NamedExecContext(ctx, query, build)
	if err != nil {
		return fmt.Errorf("failed to create build: %w", err)
	}
	return nil
}

// GetByID retrieves a build by ID
func (q *BuildQueries) GetByID(ctx context.Context, id string) (*models.Build, error) {
	var build models.Build
	query := `
		SELECT b.*, a.name as app_name
		FROM builds b
		JOIN apps a ON a.id = b.app_id
		WHERE b.id = ?`

	err := q.db.GetContext(ctx, &build, query, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get build: %w", err)
	}

	return &build, nil
}

// ListByAppID retrieves builds for an app
func (q *BuildQueries) ListByAppID(ctx context.Context, appID string, limit, offset int) ([]*models.Build, error) {
	var builds []*models.Build
	query := `
		SELECT b.*, a.name as app_name
		FROM builds b
		JOIN apps a ON a.id = b.app_id
		WHERE b.app_id = ?
		ORDER BY b.created_at DESC
		LIMIT ? OFFSET ?`

	err := q.db.SelectContext(ctx, &builds, query, appID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list builds: %w", err)
	}

	return builds, nil
}

// ListRecent retrieves recent builds across all apps
func (q *BuildQueries) ListRecent(ctx context.Context, limit int) ([]*models.Build, error) {
	var builds []*models.Build
	query := `
		SELECT b.*, a.name as app_name
		FROM builds b
		JOIN apps a ON a.id = b.app_id
		ORDER BY b.created_at DESC
		LIMIT ?`

	err := q.db.SelectContext(ctx, &builds, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list builds: %w", err)
	}

	return builds, nil
}

// GetLatestByAppID retrieves the most recent build for an app
func (q *BuildQueries) GetLatestByAppID(ctx context.Context, appID string) (*models.Build, error) {
	var build models.Build
	query := `
		SELECT b.*, a.name as app_name
		FROM builds b
		JOIN apps a ON a.id = b.app_id
		WHERE b.app_id = ?
		ORDER BY b.created_at DESC
		LIMIT 1`

	err := q.db.GetContext(ctx, &build, query, appID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get latest build: %w", err)
	}

	return &build, nil
}

// GetLatestSuccessfulByAppID retrieves the most recent successful build for an app
func (q *BuildQueries) GetLatestSuccessfulByAppID(ctx context.Context, appID string) (*models.Build, error) {
	var build models.Build
	query := `
		SELECT b.*, a.name as app_name
		FROM builds b
		JOIN apps a ON a.id = b.app_id
		WHERE b.app_id = ? AND b.status = 'success'
		ORDER BY b.created_at DESC
		LIMIT 1`

	err := q.db.GetContext(ctx, &build, query, appID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get latest successful build: %w", err)
	}

	return &build, nil
}

// CountByAppID returns the total number of builds for an app
func (q *BuildQueries) CountByAppID(ctx context.Context, appID string) (int, error) {
	var count int
	query := `SELECT COUNT(*) FROM builds WHERE app_id = ?`

	err := q.db.GetContext(ctx, &count, query, appID)
	if err != nil {
		return 0, fmt.Errorf("failed to count builds: %w", err)
	}

	return count, nil
}

// Update updates an existing build
func (q *BuildQueries) Update(ctx context.Context, build *models.Build) error {
	query := `
		UPDATE builds SET
			status = :status,
			commit_sha = :commit_sha,
			commit_message = :commit_message,
			commit_author = :commit_author,
			branch = :branch,
			image_tag = :image_tag,
			error_message = :error_message,
			started_at = :started_at,
			finished_at = :finished_at
		WHERE id = :id`

	result, err := q.db.NamedExecContext(ctx, query, build)
	if err != nil {
		return fmt.Errorf("failed to update build: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("build not found: %s", build.ID)
	}

	return nil
}

// Delete removes a build
func (q *BuildQueries) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM builds WHERE id = ?`

	result, err := q.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete build: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("build not found: %s", id)
	}

	return nil
}

// GetRunningBuilds retrieves all currently running builds
func (q *BuildQueries) GetRunningBuilds(ctx context.Context) ([]*models.Build, error) {
	var builds []*models.Build
	query := `
		SELECT b.*, a.name as app_name
		FROM builds b
		JOIN apps a ON a.id = b.app_id
		WHERE b.status IN ('pending', 'cloning', 'building', 'pushing', 'deploying')
		ORDER BY b.created_at`

	err := q.db.SelectContext(ctx, &builds, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get running builds: %w", err)
	}

	return builds, nil
}
