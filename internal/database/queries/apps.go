package queries

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	"schooner/internal/models"
)

// AppQueries provides database operations for apps
type AppQueries struct {
	db *sqlx.DB
}

// NewAppQueries creates a new AppQueries instance
func NewAppQueries(db *sqlx.DB) *AppQueries {
	return &AppQueries{db: db}
}

// Create inserts a new app
func (q *AppQueries) Create(ctx context.Context, app *models.App) error {
	query := `
		INSERT INTO apps (
			id, name, description, repo_url, branch, webhook_secret,
			build_strategy, dockerfile_path, compose_file, build_context,
			container_name, image_name, deploy_config, env_vars,
			auto_deploy, enabled, subdomain, public_port, created_at, updated_at
		) VALUES (
			:id, :name, :description, :repo_url, :branch, :webhook_secret,
			:build_strategy, :dockerfile_path, :compose_file, :build_context,
			:container_name, :image_name, :deploy_config, :env_vars,
			:auto_deploy, :enabled, :subdomain, :public_port, :created_at, :updated_at
		)`

	_, err := q.db.NamedExecContext(ctx, query, app)
	if err != nil {
		return fmt.Errorf("failed to create app: %w", err)
	}
	return nil
}

// GetByID retrieves an app by ID
func (q *AppQueries) GetByID(ctx context.Context, id string) (*models.App, error) {
	var app models.App
	query := `SELECT * FROM apps WHERE id = ?`

	err := q.db.GetContext(ctx, &app, query, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get app: %w", err)
	}

	if err := app.LoadEnvVars(); err != nil {
		return nil, fmt.Errorf("failed to load env vars: %w", err)
	}

	return &app, nil
}

// GetByName retrieves an app by name
func (q *AppQueries) GetByName(ctx context.Context, name string) (*models.App, error) {
	var app models.App
	query := `SELECT * FROM apps WHERE name = ?`

	err := q.db.GetContext(ctx, &app, query, name)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get app: %w", err)
	}

	if err := app.LoadEnvVars(); err != nil {
		return nil, fmt.Errorf("failed to load env vars: %w", err)
	}

	return &app, nil
}

// List retrieves all apps
func (q *AppQueries) List(ctx context.Context) ([]*models.App, error) {
	var apps []*models.App
	query := `SELECT * FROM apps ORDER BY name`

	err := q.db.SelectContext(ctx, &apps, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list apps: %w", err)
	}

	for _, app := range apps {
		if err := app.LoadEnvVars(); err != nil {
			return nil, fmt.Errorf("failed to load env vars: %w", err)
		}
	}

	return apps, nil
}

// ListEnabled retrieves all enabled apps
func (q *AppQueries) ListEnabled(ctx context.Context) ([]*models.App, error) {
	var apps []*models.App
	query := `SELECT * FROM apps WHERE enabled = 1 ORDER BY name`

	err := q.db.SelectContext(ctx, &apps, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list enabled apps: %w", err)
	}

	for _, app := range apps {
		if err := app.LoadEnvVars(); err != nil {
			return nil, fmt.Errorf("failed to load env vars: %w", err)
		}
	}

	return apps, nil
}

// FindByRepoAndBranch finds apps matching a repo URL and branch
func (q *AppQueries) FindByRepoAndBranch(ctx context.Context, repoURL, branch string) ([]*models.App, error) {
	var apps []*models.App
	query := `
		SELECT * FROM apps
		WHERE enabled = 1
		AND auto_deploy = 1
		AND (repo_url = ? OR repo_url = ?)
		AND branch = ?`

	// Try both HTTPS and SSH URL formats
	httpsURL := repoURL
	sshURL := repoURL

	err := q.db.SelectContext(ctx, &apps, query, httpsURL, sshURL, branch)
	if err != nil {
		return nil, fmt.Errorf("failed to find apps: %w", err)
	}

	for _, app := range apps {
		if err := app.LoadEnvVars(); err != nil {
			return nil, fmt.Errorf("failed to load env vars: %w", err)
		}
	}

	return apps, nil
}

// Update updates an existing app
func (q *AppQueries) Update(ctx context.Context, app *models.App) error {
	app.UpdatedAt = time.Now()

	query := `
		UPDATE apps SET
			name = :name,
			description = :description,
			repo_url = :repo_url,
			branch = :branch,
			webhook_secret = :webhook_secret,
			build_strategy = :build_strategy,
			dockerfile_path = :dockerfile_path,
			compose_file = :compose_file,
			build_context = :build_context,
			container_name = :container_name,
			image_name = :image_name,
			deploy_config = :deploy_config,
			env_vars = :env_vars,
			auto_deploy = :auto_deploy,
			enabled = :enabled,
			subdomain = :subdomain,
			public_port = :public_port,
			updated_at = :updated_at
		WHERE id = :id`

	result, err := q.db.NamedExecContext(ctx, query, app)
	if err != nil {
		return fmt.Errorf("failed to update app: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("app not found: %s", app.ID)
	}

	return nil
}

// Delete removes an app
func (q *AppQueries) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM apps WHERE id = ?`

	result, err := q.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete app: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("app not found: %s", id)
	}

	return nil
}

