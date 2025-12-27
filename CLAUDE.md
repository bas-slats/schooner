# Schooner - Claude Development Guidelines

## Project Overview
Schooner is a self-hosted continuous deployment tool for Docker-based homelabs. It provides GitHub webhook integration, container building, and automatic deployments.

## Development Rules

### Testing Requirements
1. **Add tests for every error found in logs**: When an error is found in application logs, add a corresponding unit test to prevent regression.
2. **Test coverage**: All new code should have associated unit tests.
3. **Run tests before committing**: Always run `go test ./...` before committing changes.

### Code Quality
1. Keep functions small and focused
2. Use meaningful variable and function names
3. Handle errors explicitly - don't ignore them
4. Use structured logging with `slog`

### Database
- Use `sql.NullString`, `sql.NullInt64`, `sql.NullTime` for nullable columns
- Use `NullRawMessage` for nullable JSON columns (see `internal/models/app.go`)
- Always run migrations on startup

### API Conventions
- Return JSON for API endpoints
- Use proper HTTP status codes
- Log errors with `slog.Error` before returning error responses

### File Structure
```
cmd/schooner/       - Application entry point
internal/
  api/              - HTTP handlers and routes
  cloudflare/       - Cloudflare Tunnel management
  config/           - Configuration types and loading
  database/         - Database connection and migrations
  docker/           - Docker client wrapper
  git/              - Git operations
  github/           - GitHub API client
  models/           - Data models
  build/            - Build orchestration
  deploy/           - Deployment logic
ui/                 - Frontend templates and static files
```

### Running the Application
```bash
# Build
go build -o schooner ./cmd/schooner

# Run (default port 7123)
./schooner

# Run tests
go test ./...
```

### Configuration
- Default port: 7123
- Database: SQLite at `./data/homelab-cd.db`
- Repos cloned to: `./data/repos`

## Common Issues

### NULL JSON columns
Use `NullRawMessage` instead of `json.RawMessage` for database columns that can be NULL. This prevents scan errors when reading NULL values.
