# Schooner - Development Guidelines

## Project Overview

Schooner is a homelab continuous deployment tool written in Go. It provides a web UI for managing Docker container deployments from GitHub repositories.

## Code Style

### Go Guidelines

- **Early returns**: Always use early returns to reduce nesting. Check errors immediately and return.
  ```go
  // Good
  if err != nil {
      return nil, err
  }

  // Bad
  if err == nil {
      // lots of code
  } else {
      return nil, err
  }
  ```

- **Error handling**: Wrap errors with context using `fmt.Errorf("action: %w", err)`.

- **File size limit**: Keep Go files under 500 lines. Split large files into logical components.

- **Function length**: Functions should be under 50 lines. Extract helper functions for complex logic.

- **Naming**: Use descriptive names. Avoid abbreviations except for common ones (ctx, err, cfg).

### Directory Structure

```
cmd/schooner/       - Main entry point
internal/
  api/              - HTTP handlers and routing
    handlers/       - Request handlers by domain
  auth/             - Authentication logic
  build/            - Build orchestration
    strategies/     - Build strategy implementations
  cloudflare/       - Cloudflare tunnel management
  config/           - Configuration types and loading
  database/         - Database connection
    queries/        - SQL query wrappers
  deploy/           - Deployment logic
  docker/           - Docker client wrapper
  git/              - Git client wrapper
  github/           - GitHub API client
  health/           - System health checks
  models/           - Data models
  observability/    - Loki/Grafana integration
ui/
  components/       - Reusable UI components
  pages/            - Page templates
  static/           - Static assets
```

## Testing Requirements

### Required Tests

1. **Unit tests**: All packages should have `*_test.go` files
2. **Model tests**: Test all model methods and validations
3. **Handler tests**: Test HTTP handlers with mock dependencies
4. **Integration tests**: Test database operations

### Running Tests

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run tests for a specific package
go test ./internal/models/...
```

### Test Naming

- Test functions: `TestFunctionName_Scenario`
- Table-driven tests preferred for multiple cases
- Use `testify/assert` for assertions when beneficial

## Pre-commit Hooks

Run before committing:

```bash
# Format code
go fmt ./...

# Vet for issues
go vet ./...

# Run tests
go test ./...

# Build to ensure compilation
go build ./...
```

## Database

- SQLite with foreign keys enabled
- Cascading deletes for related records
- Use parameterized queries to prevent SQL injection
- Migrations in `internal/database/migrations/`

## Security Considerations

- Never log secrets or tokens
- Validate webhook signatures
- Use CSRF protection for forms
- Sanitize HTML output with `html.EscapeString()`
- OAuth tokens stored encrypted in database

## Configuration

- Config file: `config.yaml`
- Environment variables override config file values
- Sensitive values (tokens, secrets) via env vars or settings DB table

## Building

```bash
# Build the binary
go build -o schooner ./cmd/schooner

# Build Docker image
docker build -t schooner .

# Run locally
./schooner
```

## Common Tasks

### Adding a New API Endpoint

1. Add handler method in appropriate `internal/api/handlers/*.go`
2. Add route in `internal/api/routes.go`
3. Add tests in `internal/api/handlers/*_test.go`

### Adding a New Setting

1. Add constant to settings package
2. Add getter/setter methods if needed
3. Update UI in `internal/api/handlers/pages.go`

### Adding a Build Strategy

1. Create new strategy in `internal/build/strategies/`
2. Implement `Strategy` interface
3. Register in `internal/api/routes.go`

## Debugging

- Logs use `log/slog` structured logging
- Set `LOG_LEVEL=debug` for verbose output
- Container logs available via Loki when observability stack is running

## Dependencies

Key dependencies:
- `github.com/go-chi/chi/v5` - HTTP router
- `github.com/docker/docker` - Docker API client
- `github.com/go-git/go-git/v5` - Git operations
- `github.com/mattn/go-sqlite3` - SQLite driver
