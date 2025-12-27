package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"schooner/internal/api/handlers"
	"schooner/internal/config"
	"schooner/internal/database"
	"schooner/internal/database/queries"
	"schooner/internal/docker"
	"schooner/internal/github"
)

// NewRouter creates and configures the HTTP router
func NewRouter(cfg *config.Config, db *database.DB) *chi.Mux {
	r := chi.NewRouter()

	// Middleware stack
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(middleware.Compress(5))

	// Initialize queries
	appQueries := queries.NewAppQueries(db.DB)
	buildQueries := queries.NewBuildQueries(db.DB)
	logQueries := queries.NewLogQueries(db.DB)
	settingsQueries := queries.NewSettingsQueries(db.DB)

	// Initialize GitHub client (token loaded from settings if available)
	githubClient := github.NewClient("")

	// Initialize Docker client
	dockerClient, err := docker.NewClient()
	if err != nil {
		slog.Warn("failed to create Docker client, container management disabled", "error", err)
	}

	// Initialize handlers
	healthHandler := handlers.NewHealthHandler()
	webhookHandler := handlers.NewWebhookHandler(cfg, appQueries, buildQueries, logQueries)
	appHandler := handlers.NewAppHandler(appQueries, buildQueries, dockerClient)
	buildHandler := handlers.NewBuildHandler(buildQueries, logQueries)
	pageHandler := handlers.NewPageHandler(cfg, appQueries, buildQueries, dockerClient)
	settingsHandler := handlers.NewSettingsHandler(settingsQueries, githubClient)
	importHandler := handlers.NewImportHandler(githubClient, appQueries)
	oauthHandler := handlers.NewOAuthHandler(cfg, settingsQueries, githubClient)

	// Static files
	fileServer := http.FileServer(http.Dir("ui/static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	// Health check
	r.Get("/health", healthHandler.Check)

	// Webhook endpoints (no auth - uses signature verification)
	r.Post("/webhook/github", webhookHandler.HandleGitHub)
	r.Post("/webhook/github/{appID}", webhookHandler.HandleGitHubForApp)

	// OAuth endpoints
	r.Get("/oauth/github/login", oauthHandler.Login)
	r.Get("/oauth/github/callback", oauthHandler.Callback)
	r.Get("/oauth/github/status", oauthHandler.Status)

	// UI Pages (HTML responses)
	r.Group(func(r chi.Router) {
		r.Get("/", pageHandler.Dashboard)
		r.Get("/apps/{appID}", pageHandler.AppDetail)
		r.Get("/builds/{buildID}", pageHandler.BuildDetail)
		r.Get("/settings", pageHandler.Settings)
	})

	// API Routes (JSON/HTMX responses)
	r.Route("/api", func(r chi.Router) {
		// Apps
		r.Route("/apps", func(r chi.Router) {
			r.Get("/", appHandler.List)
			r.Post("/", appHandler.Create)
			r.Get("/statuses", appHandler.AllStatuses)
			r.Get("/{appID}", appHandler.Get)
			r.Put("/{appID}", appHandler.Update)
			r.Delete("/{appID}", appHandler.Delete)

			// App-specific actions
			r.Get("/{appID}/status", appHandler.Status)
			r.Post("/{appID}/deploy", appHandler.TriggerDeploy)
			r.Post("/{appID}/stop", appHandler.Stop)
			r.Post("/{appID}/start", appHandler.Start)
			r.Post("/{appID}/restart", appHandler.Restart)
		})

		// Builds
		r.Route("/builds", func(r chi.Router) {
			r.Get("/", buildHandler.List)
			r.Get("/{buildID}", buildHandler.Get)
			r.Post("/{buildID}/cancel", buildHandler.Cancel)
			r.Post("/{buildID}/retry", buildHandler.Retry)

			// Build logs
			r.Get("/{buildID}/logs", buildHandler.GetLogs)
			r.Get("/{buildID}/logs/stream", buildHandler.StreamLogs)
		})

		// Settings
		r.Route("/settings", func(r chi.Router) {
			r.Get("/", settingsHandler.GetAll)
			r.Post("/github-token", settingsHandler.SetGitHubToken)
			r.Delete("/github-token", settingsHandler.DeleteGitHubToken)
			r.Get("/github-status", settingsHandler.GetGitHubStatus)
			r.Get("/clone-directory", settingsHandler.GetCloneDirectory)
			r.Post("/clone-directory", settingsHandler.SetCloneDirectory)
		})

		// GitHub import
		r.Route("/github", func(r chi.Router) {
			r.Get("/repos", importHandler.ListRepos)
			r.Post("/import", importHandler.ImportRepo)
		})
	})

	return r
}
