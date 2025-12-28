package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"schooner/internal/api/handlers"
	"schooner/internal/auth"
	"schooner/internal/build"
	"schooner/internal/build/strategies"
	"schooner/internal/cloudflare"
	"schooner/internal/config"
	"schooner/internal/database"
	"schooner/internal/database/queries"
	"schooner/internal/docker"
	"schooner/internal/git"
	"schooner/internal/github"
	"schooner/internal/observability"
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
	r.Use(securityHeaders)

	// Initialize queries
	appQueries := queries.NewAppQueries(db.DB)
	buildQueries := queries.NewBuildQueries(db.DB)
	logQueries := queries.NewLogQueries(db.DB)
	settingsQueries := queries.NewSettingsQueries(db.DB)

	// Initialize session store (24 hour TTL)
	sessionStore := auth.NewSessionStore(24 * time.Hour)

	// Initialize auth middleware
	authMiddleware := auth.NewMiddleware(sessionStore, "/oauth/github/login")

	// Initialize GitHub client and load token from settings if available
	githubClient := github.NewClient("")
	if token, err := settingsQueries.Get(context.Background(), "github_token"); err == nil && token != "" {
		githubClient.SetToken(token)
		slog.Info("GitHub token loaded from settings")
	}

	// Initialize Docker client
	dockerClient, err := docker.NewClient()
	if err != nil {
		slog.Warn("failed to create Docker client, container management disabled", "error", err)
	}

	// Initialize Git client
	var gitOpts []git.ClientOption
	if cfg.Git.SSHKeyPath != "" {
		gitOpts = append(gitOpts, git.WithSSHKey(cfg.Git.SSHKeyPath))
	}
	// Use GitHub token for HTTPS auth if available
	if githubClient.HasToken() {
		gitOpts = append(gitOpts, git.WithHTTPAuth("x-access-token", githubClient.GetToken()))
		slog.Info("Git client configured with GitHub token for HTTPS auth")
	}
	gitClient, err := git.NewClient(cfg.Git.WorkDir, gitOpts...)
	if err != nil {
		slog.Warn("failed to create Git client", "error", err)
	}

	// Cancel any stale builds from previous run
	if cancelled, err := buildQueries.CancelStaleBuilds(context.Background()); err != nil {
		slog.Error("failed to cancel stale builds", "error", err)
	} else if cancelled > 0 {
		slog.Info("cancelled stale builds from previous run", "count", cancelled)
	}

	// Initialize build orchestrator
	var orchestrator *build.Orchestrator
	if gitClient != nil && dockerClient != nil {
		orchestrator = build.NewOrchestrator(gitClient, dockerClient, appQueries, buildQueries, logQueries)
		orchestrator.RegisterStrategy(strategies.NewDockerfileStrategy(dockerClient))
		orchestrator.RegisterStrategy(strategies.NewComposeStrategy(dockerClient))
		orchestrator.Start(2) // 2 concurrent build workers
	}

	// Initialize Cloudflare tunnel manager
	var tunnelManager *cloudflare.Manager
	if dockerClient != nil {
		tunnelManager = cloudflare.NewManager(cfg, dockerClient)
		tunnelManager.SetSettingsQueries(settingsQueries)
	}

	// Initialize observability manager (Loki + Grafana)
	var observabilityManager *observability.Manager
	if dockerClient != nil {
		observabilityManager = observability.NewManager(cfg, dockerClient)
		observabilityManager.SetSettingsQueries(settingsQueries)
	}

	// Initialize handlers
	healthHandler := handlers.NewHealthHandler()
	webhookHandler := handlers.NewWebhookHandler(cfg, appQueries, buildQueries, logQueries, orchestrator)
	appHandler := handlers.NewAppHandler(cfg, appQueries, buildQueries, dockerClient, tunnelManager, orchestrator, githubClient)
	buildHandler := handlers.NewBuildHandler(buildQueries, logQueries)
	pageHandler := handlers.NewPageHandler(cfg, appQueries, buildQueries, dockerClient, tunnelManager, observabilityManager)
	settingsHandler := handlers.NewSettingsHandler(settingsQueries, githubClient, gitClient, tunnelManager, observabilityManager)
	logsHandler := handlers.NewLogsHandler(observabilityManager, appQueries)
	importHandler := handlers.NewImportHandler(cfg, githubClient, appQueries)
	oauthHandler := handlers.NewOAuthHandler(cfg, settingsQueries, githubClient, gitClient, sessionStore)

	// Static files (public)
	fileServer := http.FileServer(http.Dir("ui/static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	// Health check (public)
	r.Get("/health", healthHandler.Check)

	// Webhook endpoints (public - uses signature verification)
	r.Post("/webhook/github", webhookHandler.HandleGitHub)
	r.Post("/webhook/github/{appID}", webhookHandler.HandleGitHubForApp)

	// OAuth endpoints (public)
	r.Get("/oauth/github/login", oauthHandler.Login)
	r.Get("/oauth/github/callback", oauthHandler.Callback)
	r.Get("/oauth/github/status", oauthHandler.Status)

	// Logout endpoint (public - clears session)
	r.Get("/logout", oauthHandler.Logout)

	// Protected routes - require authentication
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware.RequireAuth)

		// UI Pages (HTML responses)
		r.Get("/", pageHandler.Dashboard)
		r.Get("/apps/{appID}", pageHandler.AppDetail)
		r.Get("/builds/{buildID}", pageHandler.BuildDetail)
		r.Get("/settings", pageHandler.Settings)
	})

	// API Routes (JSON/HTMX responses) - protected
	r.Route("/api", func(r chi.Router) {
		r.Use(authMiddleware.RequireAuth)
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
			r.Post("/{appID}/webhook", appHandler.ConfigureWebhook)
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

			// Cloudflare Tunnel
			r.Get("/tunnel-status", settingsHandler.GetTunnelStatus)
			r.Post("/tunnel", settingsHandler.SetTunnelConfig)
			r.Post("/tunnel/start", settingsHandler.StartTunnel)
			r.Post("/tunnel/stop", settingsHandler.StopTunnel)

			// Observability (Loki + Grafana)
			r.Get("/observability-status", settingsHandler.GetObservabilityStatus)
			r.Post("/observability", settingsHandler.SetObservabilityConfig)
			r.Post("/observability/start", settingsHandler.StartObservability)
			r.Post("/observability/stop", settingsHandler.StopObservability)
		})

		// Container logs (via Loki)
		r.Route("/logs", func(r chi.Router) {
			r.Get("/", logsHandler.ListSources)
			r.Get("/{appID}", logsHandler.GetLogs)
			r.Get("/{appID}/stream", logsHandler.StreamLogs)
		})

		// GitHub import
		r.Route("/github", func(r chi.Router) {
			r.Get("/repos", importHandler.ListRepos)
			r.Post("/import", importHandler.ImportRepo)
		})

		// System health
		r.Get("/health/system", healthHandler.GetSystemHealth)

		// Container stats
		r.Get("/containers/stats", appHandler.ContainerStats)
	})

	return r
}

// securityHeaders adds security-related HTTP headers to all responses
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent MIME type sniffing
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// Prevent clickjacking
		w.Header().Set("X-Frame-Options", "DENY")
		// Enable XSS filter in browsers
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		// Prevent caching of sensitive data
		w.Header().Set("Cache-Control", "no-store")
		// Referrer policy
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		next.ServeHTTP(w, r)
	})
}
