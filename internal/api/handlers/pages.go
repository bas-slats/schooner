package handlers

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"homelab-cd/internal/config"
	"homelab-cd/internal/database/queries"
)

// PageHandler handles page rendering
type PageHandler struct {
	cfg          *config.Config
	appQueries   *queries.AppQueries
	buildQueries *queries.BuildQueries
}

// NewPageHandler creates a new PageHandler
func NewPageHandler(cfg *config.Config, appQueries *queries.AppQueries, buildQueries *queries.BuildQueries) *PageHandler {
	return &PageHandler{
		cfg:          cfg,
		appQueries:   appQueries,
		buildQueries: buildQueries,
	}
}

// Dashboard handles GET /
func (h *PageHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	apps, err := h.appQueries.List(ctx)
	if err != nil {
		slog.Error("failed to list apps", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	builds, err := h.buildQueries.ListRecent(ctx, 10)
	if err != nil {
		slog.Error("failed to list builds", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// TODO: Render templ template
	// For now, return a simple HTML placeholder
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Dashboard | Homelab CD</title>
    <script src="/static/js/htmx.min.js"></script>
    <link href="/static/css/styles.css" rel="stylesheet">
</head>
<body class="bg-gray-900 text-gray-100 min-h-screen">
    <nav class="bg-gray-800 border-b border-gray-700 px-6 py-4">
        <div class="flex items-center justify-between max-w-7xl mx-auto">
            <a href="/" class="text-xl font-bold text-blue-400">Homelab CD</a>
            <div class="flex space-x-4">
                <a href="/" class="text-gray-300 hover:text-white">Dashboard</a>
                <a href="/settings" class="text-gray-300 hover:text-white">Settings</a>
            </div>
        </div>
    </nav>
    <main class="max-w-7xl mx-auto px-6 py-8">
        <h1 class="text-2xl font-bold mb-6">Applications</h1>
        <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6" id="apps">
`))

	// Render apps
	for _, app := range apps {
		latestBuild, _ := h.buildQueries.GetLatestByAppID(ctx, app.ID)
		buildStatus := "no builds"
		if latestBuild != nil {
			buildStatus = string(latestBuild.Status)
		}

		w.Write([]byte(`
            <div class="bg-gray-800 rounded-lg p-6 border border-gray-700">
                <div class="flex items-center justify-between mb-4">
                    <h3 class="text-lg font-semibold">` + app.Name + `</h3>
                    <span class="px-2 py-1 text-xs rounded-full bg-gray-700">` + buildStatus + `</span>
                </div>
                <p class="text-sm text-gray-400 mb-4">` + app.GetDescription() + `</p>
                <div class="flex justify-between text-sm text-gray-500 mb-4">
                    <span>Branch: ` + app.Branch + `</span>
                    <span>` + string(app.BuildStrategy) + `</span>
                </div>
                <div class="flex space-x-2">
                    <button
                        class="px-3 py-1 bg-blue-600 hover:bg-blue-700 rounded text-sm"
                        hx-post="/api/apps/` + app.ID + `/deploy"
                        hx-swap="none">
                        Deploy
                    </button>
                    <a href="/apps/` + app.ID + `" class="px-3 py-1 bg-gray-700 hover:bg-gray-600 rounded text-sm">
                        Details
                    </a>
                </div>
            </div>
`))
	}

	w.Write([]byte(`
        </div>
        <h2 class="text-xl font-bold mt-10 mb-4">Recent Builds</h2>
        <div class="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden">
            <table class="w-full">
                <thead class="bg-gray-700">
                    <tr>
                        <th class="px-4 py-3 text-left text-sm">App</th>
                        <th class="px-4 py-3 text-left text-sm">Status</th>
                        <th class="px-4 py-3 text-left text-sm">Commit</th>
                        <th class="px-4 py-3 text-left text-sm">Trigger</th>
                        <th class="px-4 py-3 text-left text-sm">Actions</th>
                    </tr>
                </thead>
                <tbody>
`))

	for _, build := range builds {
		commitSHA := build.GetShortSHA()
		if commitSHA == "" {
			commitSHA = "-"
		}
		w.Write([]byte(`
                    <tr class="border-t border-gray-700">
                        <td class="px-4 py-3 text-sm">` + build.AppName + `</td>
                        <td class="px-4 py-3 text-sm">` + string(build.Status) + `</td>
                        <td class="px-4 py-3 text-sm font-mono">` + commitSHA + `</td>
                        <td class="px-4 py-3 text-sm">` + string(build.Trigger) + `</td>
                        <td class="px-4 py-3 text-sm">
                            <a href="/builds/` + build.ID + `" class="text-blue-400 hover:text-blue-300">View</a>
                        </td>
                    </tr>
`))
	}

	w.Write([]byte(`
                </tbody>
            </table>
        </div>
    </main>
</body>
</html>`))
}

// AppDetail handles GET /apps/{appID}
func (h *PageHandler) AppDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	appID := chi.URLParam(r, "appID")

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

	builds, err := h.buildQueries.ListByAppID(ctx, appID, 20, 0)
	if err != nil {
		slog.Error("failed to list builds", "error", err)
	}

	// TODO: Render templ template
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>` + app.Name + ` | Homelab CD</title>
    <script src="/static/js/htmx.min.js"></script>
    <link href="/static/css/styles.css" rel="stylesheet">
</head>
<body class="bg-gray-900 text-gray-100 min-h-screen">
    <nav class="bg-gray-800 border-b border-gray-700 px-6 py-4">
        <div class="flex items-center justify-between max-w-7xl mx-auto">
            <a href="/" class="text-xl font-bold text-blue-400">Homelab CD</a>
        </div>
    </nav>
    <main class="max-w-7xl mx-auto px-6 py-8">
        <div class="flex items-center justify-between mb-6">
            <h1 class="text-2xl font-bold">` + app.Name + `</h1>
            <button
                class="px-4 py-2 bg-blue-600 hover:bg-blue-700 rounded"
                hx-post="/api/apps/` + app.ID + `/deploy"
                hx-swap="none">
                Deploy Now
            </button>
        </div>
        <div class="bg-gray-800 rounded-lg p-6 border border-gray-700 mb-8">
            <div class="grid grid-cols-2 gap-4">
                <div><span class="text-gray-400">Repository:</span> <span class="ml-2">` + app.RepoURL + `</span></div>
                <div><span class="text-gray-400">Branch:</span> <span class="ml-2">` + app.Branch + `</span></div>
                <div><span class="text-gray-400">Build Strategy:</span> <span class="ml-2">` + string(app.BuildStrategy) + `</span></div>
                <div><span class="text-gray-400">Auto Deploy:</span> <span class="ml-2">` + boolToStr(app.AutoDeploy) + `</span></div>
            </div>
        </div>
        <h2 class="text-xl font-bold mb-4">Build History</h2>
        <div class="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden">
            <table class="w-full">
                <thead class="bg-gray-700">
                    <tr>
                        <th class="px-4 py-3 text-left text-sm">Status</th>
                        <th class="px-4 py-3 text-left text-sm">Commit</th>
                        <th class="px-4 py-3 text-left text-sm">Message</th>
                        <th class="px-4 py-3 text-left text-sm">Trigger</th>
                        <th class="px-4 py-3 text-left text-sm">Actions</th>
                    </tr>
                </thead>
                <tbody>
`))

	for _, build := range builds {
		commitSHA := build.GetShortSHA()
		if commitSHA == "" {
			commitSHA = "-"
		}
		commitMsg := build.GetCommitMessage()
		if len(commitMsg) > 50 {
			commitMsg = commitMsg[:50] + "..."
		}
		w.Write([]byte(`
                    <tr class="border-t border-gray-700">
                        <td class="px-4 py-3 text-sm">` + string(build.Status) + `</td>
                        <td class="px-4 py-3 text-sm font-mono">` + commitSHA + `</td>
                        <td class="px-4 py-3 text-sm">` + commitMsg + `</td>
                        <td class="px-4 py-3 text-sm">` + string(build.Trigger) + `</td>
                        <td class="px-4 py-3 text-sm">
                            <a href="/builds/` + build.ID + `" class="text-blue-400 hover:text-blue-300">View Logs</a>
                        </td>
                    </tr>
`))
	}

	w.Write([]byte(`
                </tbody>
            </table>
        </div>
    </main>
</body>
</html>`))
}

// BuildDetail handles GET /builds/{buildID}
func (h *PageHandler) BuildDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	buildID := chi.URLParam(r, "buildID")

	build, err := h.buildQueries.GetByID(ctx, buildID)
	if err != nil {
		slog.Error("failed to get build", "buildID", buildID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if build == nil {
		http.Error(w, "build not found", http.StatusNotFound)
		return
	}

	// TODO: Render templ template with SSE log viewer
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Build ` + build.ID[:8] + ` | Homelab CD</title>
    <script src="/static/js/htmx.min.js"></script>
    <script src="/static/js/sse.js"></script>
    <link href="/static/css/styles.css" rel="stylesheet">
</head>
<body class="bg-gray-900 text-gray-100 min-h-screen">
    <nav class="bg-gray-800 border-b border-gray-700 px-6 py-4">
        <div class="flex items-center justify-between max-w-7xl mx-auto">
            <a href="/" class="text-xl font-bold text-blue-400">Homelab CD</a>
        </div>
    </nav>
    <main class="max-w-7xl mx-auto px-6 py-8">
        <div class="flex items-center mb-6">
            <a href="/apps/` + build.AppID + `" class="text-gray-400 hover:text-white mr-4">&larr; Back</a>
            <h1 class="text-2xl font-bold">Build ` + build.ID[:8] + `</h1>
        </div>
        <div class="bg-gray-800 rounded-lg p-6 border border-gray-700 mb-8">
            <div class="grid grid-cols-2 gap-4">
                <div><span class="text-gray-400">App:</span> <span class="ml-2">` + build.AppName + `</span></div>
                <div><span class="text-gray-400">Status:</span> <span class="ml-2">` + string(build.Status) + `</span></div>
                <div><span class="text-gray-400">Commit:</span> <span class="ml-2 font-mono">` + build.GetShortSHA() + `</span></div>
                <div><span class="text-gray-400">Trigger:</span> <span class="ml-2">` + string(build.Trigger) + `</span></div>
            </div>
        </div>
        <h2 class="text-xl font-bold mb-4">Build Logs</h2>
        <div class="bg-gray-900 rounded-lg border border-gray-700 overflow-hidden">
            <div class="bg-gray-800 px-4 py-2 border-b border-gray-700 flex justify-between items-center">
                <h4 class="text-sm font-medium text-gray-300">Output</h4>
                <button class="text-xs text-gray-500 hover:text-gray-300" onclick="scrollToBottom()">Scroll to bottom</button>
            </div>
            <div id="log-content" class="p-4 h-96 overflow-y-auto font-mono text-sm whitespace-pre-wrap">
                Loading logs...
            </div>
        </div>
    </main>
    <script>
        const logContent = document.getElementById('log-content');
        const buildID = '` + build.ID + `';

        function scrollToBottom() {
            logContent.scrollTop = logContent.scrollHeight;
        }

        // Connect to SSE stream
        const eventSource = new EventSource('/api/builds/' + buildID + '/logs/stream');

        logContent.innerHTML = '';

        eventSource.addEventListener('log', function(e) {
            const log = JSON.parse(e.data);
            const line = document.createElement('div');
            line.className = 'log-line ' + log.level;
            const timestamp = new Date(log.timestamp).toLocaleTimeString();
            line.innerHTML = '<span class="text-gray-600">' + timestamp + '</span> <span class="ml-2">' + log.message + '</span>';
            logContent.appendChild(line);
            scrollToBottom();
        });

        eventSource.addEventListener('complete', function(e) {
            const data = JSON.parse(e.data);
            const line = document.createElement('div');
            line.className = 'log-line text-blue-400 font-bold mt-4';
            line.textContent = 'Build ' + data.status;
            logContent.appendChild(line);
            eventSource.close();
        });

        eventSource.onerror = function() {
            eventSource.close();
        };
    </script>
</body>
</html>`))
}

// Settings handles GET /settings
func (h *PageHandler) Settings(w http.ResponseWriter, r *http.Request) {
	// TODO: Render settings page
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Settings | Homelab CD</title>
    <link href="/static/css/styles.css" rel="stylesheet">
</head>
<body class="bg-gray-900 text-gray-100 min-h-screen">
    <nav class="bg-gray-800 border-b border-gray-700 px-6 py-4">
        <div class="flex items-center justify-between max-w-7xl mx-auto">
            <a href="/" class="text-xl font-bold text-blue-400">Homelab CD</a>
        </div>
    </nav>
    <main class="max-w-7xl mx-auto px-6 py-8">
        <h1 class="text-2xl font-bold mb-6">Settings</h1>
        <div class="bg-gray-800 rounded-lg p-6 border border-gray-700">
            <p class="text-gray-400">Settings are configured via config.yaml</p>
        </div>
    </main>
</body>
</html>`))
}

func boolToStr(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}
