package queries

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	"schooner/internal/models"
)

// LogQueries provides database operations for build logs
type LogQueries struct {
	db *sqlx.DB
}

// NewLogQueries creates a new LogQueries instance
func NewLogQueries(db *sqlx.DB) *LogQueries {
	return &LogQueries{db: db}
}

// Append adds a new log entry
func (q *LogQueries) Append(ctx context.Context, log *models.BuildLog) error {
	if log.Timestamp.IsZero() {
		log.Timestamp = time.Now()
	}

	query := `
		INSERT INTO build_logs (build_id, timestamp, level, message, source)
		VALUES (:build_id, :timestamp, :level, :message, :source)`

	result, err := q.db.NamedExecContext(ctx, query, log)
	if err != nil {
		return fmt.Errorf("failed to append log: %w", err)
	}

	id, _ := result.LastInsertId()
	log.ID = id

	return nil
}

// AppendBatch adds multiple log entries efficiently
func (q *LogQueries) AppendBatch(ctx context.Context, logs []*models.BuildLog) error {
	if len(logs) == 0 {
		return nil
	}

	query := `
		INSERT INTO build_logs (build_id, timestamp, level, message, source)
		VALUES (:build_id, :timestamp, :level, :message, :source)`

	_, err := q.db.NamedExecContext(ctx, query, logs)
	if err != nil {
		return fmt.Errorf("failed to append logs: %w", err)
	}

	return nil
}

// GetByBuildID retrieves all logs for a build
func (q *LogQueries) GetByBuildID(ctx context.Context, buildID string) ([]*models.BuildLog, error) {
	var logs []*models.BuildLog
	query := `
		SELECT * FROM build_logs
		WHERE build_id = ?
		ORDER BY timestamp, id`

	err := q.db.SelectContext(ctx, &logs, query, buildID)
	if err != nil {
		return nil, fmt.Errorf("failed to get logs: %w", err)
	}

	return logs, nil
}

// GetByBuildIDSince retrieves logs for a build after a timestamp
func (q *LogQueries) GetByBuildIDSince(ctx context.Context, buildID string, since time.Time) ([]*models.BuildLog, error) {
	var logs []*models.BuildLog
	query := `
		SELECT * FROM build_logs
		WHERE build_id = ? AND timestamp > ?
		ORDER BY timestamp, id`

	err := q.db.SelectContext(ctx, &logs, query, buildID, since)
	if err != nil {
		return nil, fmt.Errorf("failed to get logs: %w", err)
	}

	return logs, nil
}

// GetByBuildIDAfterID retrieves logs for a build after a specific log ID
func (q *LogQueries) GetByBuildIDAfterID(ctx context.Context, buildID string, afterID int64) ([]*models.BuildLog, error) {
	var logs []*models.BuildLog
	query := `
		SELECT * FROM build_logs
		WHERE build_id = ? AND id > ?
		ORDER BY id`

	err := q.db.SelectContext(ctx, &logs, query, buildID, afterID)
	if err != nil {
		return nil, fmt.Errorf("failed to get logs: %w", err)
	}

	return logs, nil
}

// GetRecentByBuildID retrieves the most recent N logs for a build
func (q *LogQueries) GetRecentByBuildID(ctx context.Context, buildID string, limit int) ([]*models.BuildLog, error) {
	var logs []*models.BuildLog
	query := `
		SELECT * FROM (
			SELECT * FROM build_logs
			WHERE build_id = ?
			ORDER BY id DESC
			LIMIT ?
		) ORDER BY id`

	err := q.db.SelectContext(ctx, &logs, query, buildID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get logs: %w", err)
	}

	return logs, nil
}

// DeleteByBuildID removes all logs for a build
func (q *LogQueries) DeleteByBuildID(ctx context.Context, buildID string) error {
	query := `DELETE FROM build_logs WHERE build_id = ?`

	_, err := q.db.ExecContext(ctx, query, buildID)
	if err != nil {
		return fmt.Errorf("failed to delete logs: %w", err)
	}

	return nil
}

// CountByBuildID returns the total number of logs for a build
func (q *LogQueries) CountByBuildID(ctx context.Context, buildID string) (int, error) {
	var count int
	query := `SELECT COUNT(*) FROM build_logs WHERE build_id = ?`

	err := q.db.GetContext(ctx, &count, query, buildID)
	if err != nil {
		return 0, fmt.Errorf("failed to count logs: %w", err)
	}

	return count, nil
}
