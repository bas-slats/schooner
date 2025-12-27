package handlers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"schooner/internal/config"
	"schooner/internal/database/queries"
	"schooner/internal/github"
	"schooner/internal/models"
)

// ImportHandler handles GitHub import requests
type ImportHandler struct {
	cfg          *config.Config
	githubClient *github.Client
	appQueries   *queries.AppQueries
}

// NewImportHandler creates a new ImportHandler
func NewImportHandler(cfg *config.Config, githubClient *github.Client, appQueries *queries.AppQueries) *ImportHandler {
	return &ImportHandler{
		cfg:          cfg,
		githubClient: githubClient,
		appQueries:   appQueries,
	}
}

// ListRepos handles GET /api/github/repos - lists user's GitHub repositories
func (h *ImportHandler) ListRepos(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if !h.githubClient.HasToken() {
		http.Error(w, "GitHub token not configured", http.StatusBadRequest)
		return
	}

	// Parse pagination
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if page <= 0 {
		page = 1
	}
	if perPage <= 0 {
		perPage = 30
	}

	repos, err := h.githubClient.ListUserRepos(ctx, page, perPage)
	if err != nil {
		slog.Error("failed to list GitHub repos", "error", err)
		http.Error(w, "failed to list repositories: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get existing apps to mark which repos are already imported
	existingApps, err := h.appQueries.List(ctx)
	if err != nil {
		slog.Error("failed to list apps", "error", err)
	}

	// Create a map of repo URLs to check for duplicates
	importedRepos := make(map[string]bool)
	for _, app := range existingApps {
		// Normalize the URL for comparison
		normalizedURL := normalizeRepoURL(app.RepoURL)
		importedRepos[normalizedURL] = true
	}

	// Enhance repo info with import status
	type RepoWithStatus struct {
		github.Repository
		AlreadyImported bool   `json:"already_imported"`
		HasDockerfile   bool   `json:"has_dockerfile"`
		HasCompose      bool   `json:"has_compose"`
		ComposeFile     string `json:"compose_file,omitempty"`
	}

	result := make([]RepoWithStatus, len(repos))
	for i, repo := range repos {
		result[i] = RepoWithStatus{
			Repository:      repo,
			AlreadyImported: importedRepos[normalizeRepoURL(repo.CloneURL)] || importedRepos[normalizeRepoURL(repo.HTMLURL)],
		}

		// Check for Dockerfile and docker-compose (do this in parallel for better performance in future)
		if hasDockerfile, _ := h.githubClient.CheckRepoHasDockerfile(ctx, strings.Split(repo.FullName, "/")[0], strings.Split(repo.FullName, "/")[1]); hasDockerfile {
			result[i].HasDockerfile = true
		}

		if hasCompose, composeFile, _ := h.githubClient.CheckRepoHasDockerCompose(ctx, strings.Split(repo.FullName, "/")[0], strings.Split(repo.FullName, "/")[1]); hasCompose {
			result[i].HasCompose = true
			result[i].ComposeFile = composeFile
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// ImportRepo handles POST /api/github/import - imports a GitHub repository as an app
func (h *ImportHandler) ImportRepo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req struct {
		RepoFullName  string `json:"repo_full_name"` // e.g., "owner/repo"
		BuildStrategy string `json:"build_strategy"` // dockerfile, compose
		AutoDeploy    bool   `json:"auto_deploy"`
		Branch        string `json:"branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.RepoFullName == "" {
		http.Error(w, "repo_full_name is required", http.StatusBadRequest)
		return
	}

	parts := strings.Split(req.RepoFullName, "/")
	if len(parts) != 2 {
		http.Error(w, "invalid repo_full_name format, expected owner/repo", http.StatusBadRequest)
		return
	}

	owner, repoName := parts[0], parts[1]

	// Fetch repo details from GitHub
	repo, err := h.githubClient.GetRepo(ctx, owner, repoName)
	if err != nil {
		slog.Error("failed to get repo from GitHub", "repo", req.RepoFullName, "error", err)
		http.Error(w, "failed to get repository: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Check if already imported
	existingApps, _ := h.appQueries.List(ctx)
	for _, app := range existingApps {
		if normalizeRepoURL(app.RepoURL) == normalizeRepoURL(repo.CloneURL) ||
			normalizeRepoURL(app.RepoURL) == normalizeRepoURL(repo.HTMLURL) {
			http.Error(w, "repository is already imported as app: "+app.Name, http.StatusConflict)
			return
		}
	}

	// Determine build strategy if not specified
	buildStrategy := req.BuildStrategy
	composeFile := "docker-compose.yaml"

	if buildStrategy == "" {
		// Auto-detect: prefer compose if available, otherwise dockerfile
		if hasCompose, file, _ := h.githubClient.CheckRepoHasDockerCompose(ctx, owner, repoName); hasCompose {
			buildStrategy = "compose"
			composeFile = file
		} else if hasDockerfile, _ := h.githubClient.CheckRepoHasDockerfile(ctx, owner, repoName); hasDockerfile {
			buildStrategy = "dockerfile"
		} else {
			buildStrategy = "dockerfile" // Default
		}
	}

	// Determine branch
	branch := req.Branch
	if branch == "" {
		branch = repo.DefaultBranch
	}

	// Create the app
	app := &models.App{
		ID:             uuid.New().String(),
		Name:           repo.Name,
		Description:    sql.NullString{String: repo.Description, Valid: repo.Description != ""},
		RepoURL:        repo.CloneURL,
		Branch:         branch,
		BuildStrategy:  models.BuildStrategy(buildStrategy),
		DockerfilePath: "Dockerfile",
		ComposeFile:    composeFile,
		BuildContext:   ".",
		ContainerName:  sql.NullString{String: repo.Name, Valid: true},
		ImageName:      sql.NullString{String: repo.Name, Valid: true},
		AutoDeploy:     req.AutoDeploy,
		Enabled:        true,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := h.appQueries.Create(ctx, app); err != nil {
		slog.Error("failed to create app from import", "error", err)
		http.Error(w, "failed to create app: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Auto-install GitHub webhook
	webhookInstalled := false
	hasToken := h.githubClient.HasToken()
	baseURL := h.cfg.Server.BaseURL
	slog.Info("webhook install check", "hasToken", hasToken, "baseURL", baseURL)
	if hasToken && baseURL != "" {
		webhookInstalled = h.installWebhook(ctx, app, owner, repoName)
	} else {
		slog.Warn("skipping webhook install", "hasToken", hasToken, "hasBaseURL", baseURL != "")
	}

	slog.Info("app imported from GitHub", "id", app.ID, "name", app.Name, "repo", req.RepoFullName, "webhookInstalled", webhookInstalled)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(app)
}

// installWebhook attempts to install a GitHub webhook for the app
func (h *ImportHandler) installWebhook(ctx context.Context, app *models.App, owner, repo string) bool {
	// Generate webhook secret
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		slog.Warn("failed to generate webhook secret", "error", err)
		return false
	}
	secret := hex.EncodeToString(secretBytes)

	// Save secret to app
	app.WebhookSecret = sql.NullString{String: secret, Valid: true}
	if err := h.appQueries.Update(ctx, app); err != nil {
		slog.Warn("failed to save webhook secret", "error", err)
	}

	// Build webhook URL
	webhookURL := h.cfg.Server.BaseURL + "/webhook/github/" + app.ID

	// Create webhook
	webhook, created, err := h.githubClient.EnsureWebhook(ctx, owner, repo, webhookURL, secret)
	if err != nil {
		slog.Warn("failed to install webhook", "app", app.Name, "error", err)
		return false
	}

	if created {
		slog.Info("webhook installed", "app", app.Name, "webhookID", webhook.ID, "url", webhookURL)
	} else {
		slog.Debug("webhook already exists", "app", app.Name, "webhookID", webhook.ID)
	}

	return true
}

// normalizeRepoURL normalizes a repository URL for comparison
func normalizeRepoURL(url string) string {
	url = strings.TrimSuffix(url, ".git")
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "git@github.com:")
	url = strings.ToLower(url)
	return url
}
