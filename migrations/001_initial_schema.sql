-- Homelab CD Initial Schema
-- SQLite database schema

-- Enable WAL mode for better concurrency
PRAGMA journal_mode=WAL;
PRAGMA busy_timeout=5000;
PRAGMA synchronous=NORMAL;
PRAGMA cache_size=1000000000;
PRAGMA foreign_keys=ON;
PRAGMA temp_store=memory;

-- Apps table: Stores application configurations
CREATE TABLE IF NOT EXISTS apps (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT,
    repo_url TEXT NOT NULL,
    branch TEXT NOT NULL DEFAULT 'main',
    webhook_secret TEXT,
    build_strategy TEXT NOT NULL CHECK(build_strategy IN ('dockerfile', 'compose', 'buildpacks')),
    dockerfile_path TEXT DEFAULT 'Dockerfile',
    compose_file TEXT DEFAULT 'docker-compose.yaml',
    build_context TEXT DEFAULT '.',
    container_name TEXT,
    image_name TEXT,
    deploy_config TEXT,
    env_vars TEXT,
    auto_deploy INTEGER NOT NULL DEFAULT 1,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Builds table: Stores build history
CREATE TABLE IF NOT EXISTS builds (
    id TEXT PRIMARY KEY,
    app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK(status IN ('pending', 'cloning', 'building', 'pushing', 'deploying', 'success', 'failed', 'cancelled')),
    trigger TEXT NOT NULL CHECK(trigger IN ('webhook', 'manual', 'rollback')),
    commit_sha TEXT,
    commit_message TEXT,
    commit_author TEXT,
    branch TEXT,
    image_tag TEXT,
    error_message TEXT,
    started_at DATETIME,
    finished_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Build logs table: Stores build log entries
CREATE TABLE IF NOT EXISTS build_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    build_id TEXT NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    level TEXT NOT NULL CHECK(level IN ('debug', 'info', 'warn', 'error')),
    message TEXT NOT NULL,
    source TEXT CHECK(source IN ('git', 'docker', 'deploy', 'system') OR source IS NULL)
);

-- Deployments table: Tracks container deployments
CREATE TABLE IF NOT EXISTS deployments (
    id TEXT PRIMARY KEY,
    app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    build_id TEXT REFERENCES builds(id) ON DELETE SET NULL,
    container_id TEXT,
    container_name TEXT,
    image_tag TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('running', 'stopped', 'failed', 'removed')),
    ports TEXT,
    deployed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    stopped_at DATETIME
);

-- Indexes for common queries
CREATE INDEX IF NOT EXISTS idx_builds_app_id ON builds(app_id);
CREATE INDEX IF NOT EXISTS idx_builds_status ON builds(status);
CREATE INDEX IF NOT EXISTS idx_builds_created_at ON builds(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_build_logs_build_id ON build_logs(build_id);
CREATE INDEX IF NOT EXISTS idx_build_logs_timestamp ON build_logs(build_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_deployments_app_id ON deployments(app_id);
CREATE INDEX IF NOT EXISTS idx_deployments_status ON deployments(status);

-- Trigger to update updated_at on apps
CREATE TRIGGER IF NOT EXISTS update_app_timestamp
    AFTER UPDATE ON apps
    FOR EACH ROW
BEGIN
    UPDATE apps SET updated_at = CURRENT_TIMESTAMP WHERE id = OLD.id;
END;
