package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"schooner/internal/config"
	"schooner/internal/database"
	"schooner/internal/database/queries"
	"schooner/internal/models"
)

// WebhookHandler handles GitHub webhook requests
type WebhookHandler struct {
	cfg          *config.Config
	appQueries   *queries.AppQueries
	buildQueries *queries.BuildQueries
	logQueries   *queries.LogQueries
}

// NewWebhookHandler creates a new WebhookHandler
func NewWebhookHandler(cfg *config.Config, appQueries *queries.AppQueries, buildQueries *queries.BuildQueries, logQueries *queries.LogQueries) *WebhookHandler {
	return &WebhookHandler{
		cfg:          cfg,
		appQueries:   appQueries,
		buildQueries: buildQueries,
		logQueries:   logQueries,
	}
}

// GitHubPushEvent represents a GitHub push webhook payload
type GitHubPushEvent struct {
	Ref        string              `json:"ref"`
	Before     string              `json:"before"`
	After      string              `json:"after"`
	Repository GitHubRepository    `json:"repository"`
	Commits    []GitHubCommit      `json:"commits"`
	HeadCommit *GitHubCommit       `json:"head_commit"`
	Pusher     GitHubPusher        `json:"pusher"`
}

// GitHubRepository represents repository info in webhook
type GitHubRepository struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	CloneURL string `json:"clone_url"`
	SSHURL   string `json:"ssh_url"`
	HTMLURL  string `json:"html_url"`
}

// GitHubCommit represents commit info in webhook
type GitHubCommit struct {
	ID        string       `json:"id"`
	Message   string       `json:"message"`
	Timestamp string       `json:"timestamp"`
	Author    GitHubAuthor `json:"author"`
}

// GitHubAuthor represents author info in webhook
type GitHubAuthor struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Username string `json:"username"`
}

// GitHubPusher represents pusher info in webhook
type GitHubPusher struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// HandleGitHub handles GitHub webhooks for any matching app
func (h *WebhookHandler) HandleGitHub(w http.ResponseWriter, r *http.Request) {
	h.handleWebhook(w, r, "")
}

// HandleGitHubForApp handles GitHub webhooks for a specific app
func (h *WebhookHandler) HandleGitHubForApp(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appID")
	h.handleWebhook(w, r, appID)
}

func (h *WebhookHandler) handleWebhook(w http.ResponseWriter, r *http.Request, appID string) {
	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("failed to read webhook body", "error", err)
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	// Get event type
	eventType := r.Header.Get("X-GitHub-Event")
	if eventType == "" {
		http.Error(w, "missing X-GitHub-Event header", http.StatusBadRequest)
		return
	}

	// Only handle push events
	if eventType != "push" {
		slog.Debug("ignoring non-push event", "event", eventType)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ignored", "reason": "not a push event"})
		return
	}

	// Parse push event
	var event GitHubPushEvent
	if err := json.Unmarshal(body, &event); err != nil {
		slog.Error("failed to parse webhook payload", "error", err)
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	// Extract branch from ref (refs/heads/main -> main)
	branch := strings.TrimPrefix(event.Ref, "refs/heads/")

	// Find matching apps
	var apps []*models.App
	ctx := r.Context()

	if appID != "" {
		// Specific app requested
		app, err := h.appQueries.GetByID(ctx, appID)
		if err != nil {
			slog.Error("failed to get app", "appID", appID, "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if app == nil {
			http.Error(w, "app not found", http.StatusNotFound)
			return
		}

		// Verify signature for this specific app
		signature := r.Header.Get("X-Hub-Signature-256")
		if app.GetWebhookSecret() != "" {
			if err := verifySignature(body, signature, app.GetWebhookSecret()); err != nil {
				slog.Warn("webhook signature verification failed", "appID", appID, "error", err)
				http.Error(w, "invalid signature", http.StatusUnauthorized)
				return
			}
		}

		// Check if branch matches
		if app.Branch != branch {
			slog.Debug("branch mismatch", "app", app.Name, "expected", app.Branch, "got", branch)
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "ignored", "reason": "branch mismatch"})
			return
		}

		apps = []*models.App{app}
	} else {
		// Find all matching apps
		var err error
		apps, err = h.appQueries.FindByRepoAndBranch(ctx, event.Repository.CloneURL, branch)
		if err != nil {
			slog.Error("failed to find matching apps", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		// Also try SSH URL
		if len(apps) == 0 {
			apps, err = h.appQueries.FindByRepoAndBranch(ctx, event.Repository.SSHURL, branch)
			if err != nil {
				slog.Error("failed to find matching apps", "error", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
		}

		// Verify signature for each app and filter
		signature := r.Header.Get("X-Hub-Signature-256")
		var validApps []*models.App
		for _, app := range apps {
			if app.GetWebhookSecret() == "" {
				validApps = append(validApps, app)
				continue
			}
			if err := verifySignature(body, signature, app.GetWebhookSecret()); err == nil {
				validApps = append(validApps, app)
			} else {
				slog.Warn("webhook signature verification failed for app", "app", app.Name)
			}
		}
		apps = validApps
	}

	if len(apps) == 0 {
		slog.Debug("no matching apps found", "repo", event.Repository.FullName, "branch", branch)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ignored", "reason": "no matching apps"})
		return
	}

	// Get commit info
	var commitSHA, commitMessage, commitAuthor string
	if event.HeadCommit != nil {
		commitSHA = event.HeadCommit.ID
		commitMessage = event.HeadCommit.Message
		commitAuthor = event.HeadCommit.Author.Name
	} else if len(event.Commits) > 0 {
		commitSHA = event.Commits[len(event.Commits)-1].ID
		commitMessage = event.Commits[len(event.Commits)-1].Message
		commitAuthor = event.Commits[len(event.Commits)-1].Author.Name
	} else {
		commitSHA = event.After
	}

	// Queue builds for each matching app
	var buildIDs []string
	for _, app := range apps {
		if !app.Enabled || !app.AutoDeploy {
			slog.Debug("skipping disabled/no-auto-deploy app", "app", app.Name)
			continue
		}

		build := &models.Build{
			ID:            uuid.New().String(),
			AppID:         app.ID,
			Status:        models.BuildStatusPending,
			Trigger:       models.TriggerWebhook,
			CommitSHA:     database.NullString(commitSHA),
			CommitMessage: database.NullString(commitMessage),
			CommitAuthor:  database.NullString(commitAuthor),
			Branch:        database.NullString(branch),
			CreatedAt:     time.Now(),
		}

		if err := h.buildQueries.Create(ctx, build); err != nil {
			slog.Error("failed to create build", "app", app.Name, "error", err)
			continue
		}

		// Add initial log entry
		log := &models.BuildLog{
			BuildID:   build.ID,
			Level:     models.LogLevelInfo,
			Message:   "Build triggered by webhook",
			Source:    models.LogSourceSystem,
			Timestamp: time.Now(),
		}
		h.logQueries.Append(ctx, log)

		slog.Info("build queued", "app", app.Name, "buildID", build.ID, "commit", commitSHA[:8])
		buildIDs = append(buildIDs, build.ID)

		// TODO: Actually trigger build execution via build orchestrator
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "accepted",
		"builds":    len(buildIDs),
		"build_ids": buildIDs,
	})
}

// verifySignature validates GitHub webhook HMAC-SHA256 signature
func verifySignature(payload []byte, signature, secret string) error {
	if signature == "" {
		return nil // No signature required if not provided
	}

	// GitHub sends signature as "sha256=<hex>"
	if !strings.HasPrefix(signature, "sha256=") {
		return &signatureError{"invalid signature format"}
	}

	signatureHex := strings.TrimPrefix(signature, "sha256=")
	receivedMAC, err := hex.DecodeString(signatureHex)
	if err != nil {
		return &signatureError{"invalid signature hex"}
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expectedMAC := mac.Sum(nil)

	if !hmac.Equal(receivedMAC, expectedMAC) {
		return &signatureError{"signature mismatch"}
	}

	return nil
}

type signatureError struct {
	message string
}

func (e *signatureError) Error() string {
	return e.message
}
