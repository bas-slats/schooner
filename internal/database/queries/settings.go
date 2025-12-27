package queries

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// Setting represents a key-value setting
type Setting struct {
	Key       string    `db:"key" json:"key"`
	Value     string    `db:"value" json:"value"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

// SettingsQueries provides database operations for settings
type SettingsQueries struct {
	db *sqlx.DB
}

// NewSettingsQueries creates a new SettingsQueries instance
func NewSettingsQueries(db *sqlx.DB) *SettingsQueries {
	return &SettingsQueries{db: db}
}

// Get retrieves a setting by key
func (q *SettingsQueries) Get(ctx context.Context, key string) (string, error) {
	var value string
	query := `SELECT value FROM settings WHERE key = ?`

	err := q.db.GetContext(ctx, &value, query, key)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("failed to get setting: %w", err)
	}

	return value, nil
}

// Set creates or updates a setting
func (q *SettingsQueries) Set(ctx context.Context, key, value string) error {
	query := `
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			updated_at = excluded.updated_at`

	_, err := q.db.ExecContext(ctx, query, key, value, time.Now())
	if err != nil {
		return fmt.Errorf("failed to set setting: %w", err)
	}

	return nil
}

// Delete removes a setting
func (q *SettingsQueries) Delete(ctx context.Context, key string) error {
	query := `DELETE FROM settings WHERE key = ?`

	_, err := q.db.ExecContext(ctx, query, key)
	if err != nil {
		return fmt.Errorf("failed to delete setting: %w", err)
	}

	return nil
}

// GetAll retrieves all settings
func (q *SettingsQueries) GetAll(ctx context.Context) (map[string]string, error) {
	var settings []Setting
	query := `SELECT * FROM settings ORDER BY key`

	err := q.db.SelectContext(ctx, &settings, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get settings: %w", err)
	}

	result := make(map[string]string)
	for _, s := range settings {
		result[s.Key] = s.Value
	}

	return result, nil
}

// SetMultiple sets multiple settings at once
func (q *SettingsQueries) SetMultiple(ctx context.Context, settings map[string]string) error {
	query := `
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			updated_at = excluded.updated_at`

	now := time.Now()
	for key, value := range settings {
		_, err := q.db.ExecContext(ctx, query, key, value, now)
		if err != nil {
			return fmt.Errorf("failed to set setting %s: %w", key, err)
		}
	}

	return nil
}
