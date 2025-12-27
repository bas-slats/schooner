package handlers

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"schooner/internal/auth"
	"schooner/internal/cloudflare"
	"schooner/internal/config"
	"schooner/internal/database/queries"
	"schooner/internal/docker"
	"schooner/internal/models"
	"schooner/internal/observability"
)

// PageHandler handles page rendering
type PageHandler struct {
	cfg                  *config.Config
	appQueries           *queries.AppQueries
	buildQueries         *queries.BuildQueries
	dockerClient         *docker.Client
	tunnelManager        *cloudflare.Manager
	observabilityManager *observability.Manager
}

// NewPageHandler creates a new PageHandler
func NewPageHandler(cfg *config.Config, appQueries *queries.AppQueries, buildQueries *queries.BuildQueries, dockerClient *docker.Client, tunnelManager *cloudflare.Manager, observabilityManager *observability.Manager) *PageHandler {
	return &PageHandler{
		cfg:                  cfg,
		appQueries:           appQueries,
		buildQueries:         buildQueries,
		dockerClient:         dockerClient,
		tunnelManager:        tunnelManager,
		observabilityManager: observabilityManager,
	}
}

func (h *PageHandler) writeHeader(w http.ResponseWriter, r *http.Request, title string) {
	// Get session for username display
	username := ""
	if session := auth.GetSession(r.Context()); session != nil {
		username = session.Username
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%s | Schooner</title>
    <link rel="icon" type="image/svg+xml" href="/static/img/logo.svg">
    <script src="https://cdn.tailwindcss.com"></script>
    <script src="https://unpkg.com/htmx.org@1.9.12"></script>
    <link href="/static/css/styles.css" rel="stylesheet">
    <style>
        .gradient-text {
            background: linear-gradient(135deg, #8b5cf6 0%%, #3b82f6 100%%);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            background-clip: text;
        }
    </style>
</head>
<body class="bg-gray-50 text-gray-900 min-h-screen">
    <nav class="bg-white border-b border-gray-200">
        <div class="max-w-7xl mx-auto px-6 py-4 flex items-center justify-between">
            <a href="/" class="flex items-center space-x-2">
                <img src="/static/img/logo.svg" alt="Schooner" class="h-8 w-8">
                <span class="text-xl font-bold gradient-text">Schooner</span>
            </a>
            <div class="flex items-center space-x-4">
                <a href="/" class="text-gray-600 hover:text-gray-900">Dashboard</a>
                <a href="/settings" class="text-gray-600 hover:text-gray-900">Settings</a>
                <span class="text-gray-400">|</span>
                <span class="text-gray-600 text-sm">%s</span>
                <a href="/logout" class="text-gray-500 hover:text-gray-700 text-sm">Logout</a>
            </div>
        </div>
    </nav>
    <main class="max-w-7xl mx-auto px-6 py-8">
`, html.EscapeString(title), html.EscapeString(username))
}

func (h *PageHandler) writeFooter(w http.ResponseWriter) {
	fmt.Fprint(w, `
    </main>
    <script>
        // Handle HTMX requests
        document.body.addEventListener('htmx:afterRequest', function(evt) {
            if (evt.detail.successful) {
                // Refresh page on successful form submission
                if (evt.detail.elt.tagName === 'FORM') {
                    window.location.reload();
                }
                // Handle deploy/start/stop/restart buttons
                if (evt.detail.elt.tagName === 'BUTTON') {
                    const action = evt.detail.pathInfo.requestPath;
                    if (action.includes('/deploy')) {
                        showToast('Build queued successfully', 'success');
                        setTimeout(() => window.location.reload(), 1500);
                    } else if (action.includes('/start') || action.includes('/stop') || action.includes('/restart')) {
                        showToast('Container action completed', 'success');
                        setTimeout(() => window.location.reload(), 1000);
                    }
                }
            } else if (evt.detail.failed) {
                showToast('Action failed: ' + (evt.detail.xhr.responseText || 'Unknown error'), 'error');
            }
        });

        // Toast notification
        function showToast(message, type) {
            const toast = document.createElement('div');
            toast.className = 'fixed bottom-4 right-4 px-4 py-2 rounded shadow-lg text-white z-50 ' +
                (type === 'error' ? 'bg-red-600' : 'bg-green-600');
            toast.textContent = message;
            document.body.appendChild(toast);
            setTimeout(() => toast.remove(), 3000);
        }

        // Confirm delete
        function confirmDelete(appId, appName) {
            if (confirm('Are you sure you want to delete "' + appName + '"?')) {
                fetch('/api/apps/' + appId, { method: 'DELETE' })
                    .then(response => {
                        if (response.ok) {
                            window.location.reload();
                        } else {
                            alert('Failed to delete app');
                        }
                    });
            }
        }

        // Configure webhook for app
        function configureWebhook(appId, appName) {
            if (confirm('Configure GitHub webhook for "' + appName + '"?')) {
                fetch('/api/apps/' + appId + '/webhook', { method: 'POST' })
                    .then(response => response.json())
                    .then(data => {
                        if (data.success) {
                            const msg = data.created ? 'Webhook created successfully!' : 'Webhook already configured.';
                            showToast(msg, 'success');
                        } else {
                            showToast('Failed to configure webhook: ' + (data.message || 'Unknown error'), 'error');
                        }
                    })
                    .catch(err => {
                        showToast('Failed to configure webhook: ' + err.message, 'error');
                    });
            }
        }

        // GitHub import functions
        function showGitHubTokenForm() {
            document.getElementById('github-token-form').classList.remove('hidden');
        }

        function hideGitHubTokenForm() {
            document.getElementById('github-token-form').classList.add('hidden');
        }

        function submitGitHubToken(event) {
            event.preventDefault();
            const form = event.target;
            const token = form.querySelector('input[name="github_token"]').value;

            fetch('/api/settings/github-token', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ token: token })
            })
            .then(response => {
                if (response.ok) {
                    window.location.reload();
                } else {
                    response.text().then(text => alert('Failed to save token: ' + text));
                }
            });
        }

        function removeGitHubToken() {
            if (confirm('Are you sure you want to remove the GitHub token?')) {
                fetch('/api/settings/github-token', { method: 'DELETE' })
                    .then(response => {
                        if (response.ok) {
                            window.location.reload();
                        } else {
                            alert('Failed to remove token');
                        }
                    });
            }
        }

        function showImportModal() {
            document.getElementById('import-modal').classList.remove('hidden');
            loadGitHubRepos();
        }

        function hideImportModal() {
            document.getElementById('import-modal').classList.add('hidden');
        }

        // Store repos globally for filtering
        let allRepos = [];

        function loadGitHubRepos(page = 1) {
            const container = document.getElementById('github-repos-list');
            container.innerHTML = '<div class="text-center py-8 text-gray-500">Loading repositories...</div>';

            fetch('/api/github/repos?page=' + page + '&per_page=100')
                .then(response => {
                    if (!response.ok) {
                        throw new Error('Failed to fetch repositories');
                    }
                    return response.json();
                })
                .then(repos => {
                    allRepos = repos;
                    renderRepos(repos);
                })
                .catch(error => {
                    container.innerHTML = '<div class="text-center py-8 text-red-400">' + error.message + '</div>';
                });
        }

        function filterRepos(query) {
            query = query.toLowerCase().trim();
            if (!query) {
                renderRepos(allRepos);
                return;
            }
            const filtered = allRepos.filter(repo =>
                repo.name.toLowerCase().includes(query) ||
                repo.full_name.toLowerCase().includes(query) ||
                (repo.description && repo.description.toLowerCase().includes(query))
            );
            renderRepos(filtered);
        }

        function renderRepos(repos) {
            const container = document.getElementById('github-repos-list');
            if (repos.length === 0) {
                container.innerHTML = '<div class="text-center py-8 text-gray-500">No repositories found</div>';
                return;
            }

            let html = '';
            repos.forEach(repo => {
                const disabled = repo.already_imported ? 'opacity-50 cursor-not-allowed' : 'hover:bg-gray-100 cursor-pointer';
                const imported = repo.already_imported ? '<span class="text-xs text-green-600 ml-2">Already imported</span>' : '';
                const badges = [];
                if (repo.has_dockerfile) badges.push('<span class="text-xs bg-blue-100 text-blue-700 px-2 py-1 rounded">Dockerfile</span>');
                if (repo.has_compose) badges.push('<span class="text-xs bg-purple-100 text-purple-700 px-2 py-1 rounded">Compose</span>');

                html += '<div class="p-4 border-b border-gray-200 ' + disabled + '" ' +
                    (repo.already_imported ? '' : 'onclick="selectRepo(\'' + repo.full_name + '\', \'' + repo.default_branch + '\', ' + repo.has_dockerfile + ', ' + repo.has_compose + ', \'' + (repo.compose_file || '') + '\')"') + '>' +
                    '<div class="flex items-center justify-between">' +
                    '<div>' +
                    '<div class="font-semibold">' + escapeHtml(repo.name) + imported + '</div>' +
                    '<div class="text-sm text-gray-500">' + escapeHtml(repo.description || 'No description') + '</div>' +
                    '</div>' +
                    '<div class="flex items-center space-x-2">' + badges.join('') + '</div>' +
                    '</div>' +
                    '</div>';
            });

            container.innerHTML = html;
        }

        function selectRepo(fullName, defaultBranch, hasDockerfile, hasCompose, composeFile) {
            document.getElementById('import-repo-name').textContent = fullName;
            document.getElementById('import-repo-fullname').value = fullName;
            document.getElementById('import-branch').value = defaultBranch;

            // Auto-select build strategy
            const strategySelect = document.getElementById('import-build-strategy');
            if (hasCompose) {
                strategySelect.value = 'compose';
            } else {
                strategySelect.value = 'dockerfile';
            }

            document.getElementById('repo-selection').classList.add('hidden');
            document.getElementById('import-config').classList.remove('hidden');
        }

        function backToRepoList() {
            document.getElementById('import-config').classList.add('hidden');
            document.getElementById('repo-selection').classList.remove('hidden');
        }

        function submitImport(event) {
            event.preventDefault();
            const form = event.target;
            const formData = new FormData(form);
            const data = {
                repo_full_name: formData.get('repo_full_name'),
                branch: formData.get('branch'),
                build_strategy: formData.get('build_strategy'),
                auto_deploy: formData.get('auto_deploy') === 'on'
            };

            const btn = form.querySelector('button[type="submit"]');
            btn.disabled = true;
            btn.textContent = 'Importing...';

            fetch('/api/github/import', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(data)
            })
            .then(response => {
                if (response.ok) {
                    window.location.reload();
                } else {
                    response.text().then(text => {
                        alert('Failed to import: ' + text);
                        btn.disabled = false;
                        btn.textContent = 'Import & Deploy';
                    });
                }
            });
        }

        function escapeHtml(text) {
            if (!text) return '';
            const div = document.createElement('div');
            div.textContent = text;
            return div.innerHTML;
        }

        // Toggle edit form
        function toggleEditForm(appId) {
            const form = document.getElementById('edit-form-' + appId);
            form.classList.toggle('hidden');
        }

        // Show add app form
        function showAddForm() {
            document.getElementById('add-app-form').classList.remove('hidden');
            document.getElementById('add-app-btn').classList.add('hidden');
        }

        function hideAddForm() {
            document.getElementById('add-app-form').classList.add('hidden');
            document.getElementById('add-app-btn').classList.remove('hidden');
        }

        // Parse env vars string to object
        function parseEnvVars(str) {
            const result = {};
            if (!str) return result;
            str.split('\n').forEach(line => {
                line = line.trim();
                if (!line || line.startsWith('#')) return;
                const idx = line.indexOf('=');
                if (idx > 0) {
                    const key = line.substring(0, idx).trim();
                    const value = line.substring(idx + 1);
                    result[key] = value;
                }
            });
            return result;
        }

        // Submit add app form
        function submitAddApp(event) {
            event.preventDefault();
            const form = event.target;
            const formData = new FormData(form);
            const data = {
                name: formData.get('name'),
                description: formData.get('description'),
                repo_url: formData.get('repo_url'),
                branch: formData.get('branch') || 'main',
                webhook_secret: formData.get('webhook_secret'),
                build_strategy: formData.get('build_strategy') || 'dockerfile',
                dockerfile_path: formData.get('dockerfile_path') || 'Dockerfile',
                compose_file: formData.get('compose_file') || 'docker-compose.yaml',
                build_context: formData.get('build_context') || '.',
                container_name: formData.get('container_name'),
                image_name: formData.get('image_name'),
                env_vars: parseEnvVars(formData.get('env_vars')),
                auto_deploy: formData.get('auto_deploy') === 'on',
                enabled: formData.get('enabled') === 'on',
                subdomain: formData.get('subdomain') || '',
                public_port: parseInt(formData.get('public_port')) || 0
            };

            fetch('/api/apps', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(data)
            })
            .then(response => {
                if (response.ok) {
                    window.location.reload();
                } else {
                    response.text().then(text => alert('Failed to add app: ' + text));
                }
            });
        }

        // Submit edit app form
        function submitEditApp(event, appId) {
            event.preventDefault();
            const form = event.target;
            const formData = new FormData(form);
            const data = {
                name: formData.get('name'),
                description: formData.get('description'),
                repo_url: formData.get('repo_url'),
                branch: formData.get('branch'),
                webhook_secret: formData.get('webhook_secret'),
                build_strategy: formData.get('build_strategy'),
                dockerfile_path: formData.get('dockerfile_path'),
                compose_file: formData.get('compose_file'),
                build_context: formData.get('build_context'),
                container_name: formData.get('container_name'),
                image_name: formData.get('image_name'),
                env_vars: parseEnvVars(formData.get('env_vars')),
                auto_deploy: formData.get('auto_deploy') === 'on',
                enabled: formData.get('enabled') === 'on',
                subdomain: formData.get('subdomain') || '',
                public_port: parseInt(formData.get('public_port')) || 0
            };

            fetch('/api/apps/' + appId, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(data)
            })
            .then(response => {
                if (response.ok) {
                    window.location.reload();
                } else {
                    response.text().then(text => alert('Failed to update app: ' + text));
                }
            });
        }
    </script>
</body>
</html>`)
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
	}

	h.writeHeader(w, r, "Dashboard")

	// System Health Section
	h.renderSystemHealth(w)

	fmt.Fprint(w, `<h1 class="text-2xl font-bold mb-6">Applications</h1>`)

	if len(apps) == 0 {
		fmt.Fprint(w, `
        <div class="bg-white shadow-sm rounded-lg p-8 border border-gray-200 text-center">
            <p class="text-gray-500 mb-4">No applications configured yet.</p>
            <a href="/settings" class="px-4 py-2 bg-blue-600 hover:bg-blue-700 rounded inline-block text-white">Add Your First App</a>
        </div>`)
	} else {
		fmt.Fprint(w, `<div class="grid grid-cols-1 lg:grid-cols-2 gap-6" id="apps">`)
		for _, app := range apps {
			latestBuild, _ := h.buildQueries.GetLatestByAppID(ctx, app.ID)
			var containerStatus *docker.ContainerStatus
			if h.dockerClient != nil {
				containerStatus, _ = h.dockerClient.GetContainerStatus(ctx, app.GetContainerName())
			}
			h.renderAppCard(w, app, latestBuild, containerStatus)
		}
		fmt.Fprint(w, `</div>`)
	}

	// Recent builds
	fmt.Fprint(w, `
        <h2 class="text-xl font-bold mt-10 mb-4">Recent Builds</h2>
        <div class="bg-white shadow-sm rounded-lg border border-gray-200 overflow-hidden">
            <table class="w-full">
                <thead class="bg-gray-50">
                    <tr>
                        <th class="px-4 py-3 text-left text-sm">App</th>
                        <th class="px-4 py-3 text-left text-sm">Status</th>
                        <th class="px-4 py-3 text-left text-sm">Commit</th>
                        <th class="px-4 py-3 text-left text-sm">Trigger</th>
                        <th class="px-4 py-3 text-left text-sm">Actions</th>
                    </tr>
                </thead>
                <tbody>`)

	if len(builds) == 0 {
		fmt.Fprint(w, `<tr><td colspan="5" class="px-4 py-8 text-center text-gray-500">No builds yet</td></tr>`)
	} else {
		for _, build := range builds {
			commitSHA := build.GetShortSHA()
			if commitSHA == "" {
				commitSHA = "-"
			}
			fmt.Fprintf(w, `
                    <tr class="border-t border-gray-200">
                        <td class="px-4 py-3 text-sm">%s</td>
                        <td class="px-4 py-3 text-sm">%s</td>
                        <td class="px-4 py-3 text-sm font-mono">%s</td>
                        <td class="px-4 py-3 text-sm">%s</td>
                        <td class="px-4 py-3 text-sm">
                            <a href="/builds/%s" class="text-purple-600 hover:text-purple-700">View</a>
                        </td>
                    </tr>`,
				html.EscapeString(build.AppName),
				html.EscapeString(string(build.Status)),
				html.EscapeString(commitSHA),
				html.EscapeString(string(build.Trigger)),
				html.EscapeString(build.ID))
		}
	}

	fmt.Fprint(w, `
                </tbody>
            </table>
        </div>`)

	// Docker containers section
	h.renderDockerContainers(w, ctx)

	h.writeFooter(w)
}

func (h *PageHandler) renderSystemHealth(w http.ResponseWriter) {
	fmt.Fprint(w, `
        <div class="mb-8">
            <h2 class="text-xl font-bold mb-4">System Health</h2>
            <div id="system-health" class="grid grid-cols-1 md:grid-cols-3 gap-4">
                <!-- CPU -->
                <div class="bg-white shadow-sm rounded-lg p-4 border border-gray-200">
                    <div class="flex items-center justify-between mb-2">
                        <span class="text-gray-500 text-sm">CPU</span>
                        <span id="cpu-cores" class="text-xs text-gray-400"></span>
                    </div>
                    <div class="text-2xl font-bold" id="cpu-usage">--%</div>
                    <div class="mt-2 h-2 bg-gray-100 rounded-full overflow-hidden">
                        <div id="cpu-bar" class="h-full bg-blue-500 rounded-full transition-all" style="width: 0%"></div>
                    </div>
                    <div class="text-xs text-gray-400 mt-1">Load: <span id="cpu-load">-</span></div>
                </div>

                <!-- Memory -->
                <div class="bg-white shadow-sm rounded-lg p-4 border border-gray-200">
                    <div class="flex items-center justify-between mb-2">
                        <span class="text-gray-500 text-sm">Memory</span>
                        <span id="mem-total" class="text-xs text-gray-400"></span>
                    </div>
                    <div class="text-2xl font-bold" id="mem-usage">--%</div>
                    <div class="mt-2 h-2 bg-gray-100 rounded-full overflow-hidden">
                        <div id="mem-bar" class="h-full bg-purple-500 rounded-full transition-all" style="width: 0%"></div>
                    </div>
                    <div class="text-xs text-gray-400 mt-1"><span id="mem-used">-</span> / <span id="mem-total-val">-</span></div>
                </div>

                <!-- Disk -->
                <div class="bg-white shadow-sm rounded-lg p-4 border border-gray-200">
                    <div class="flex items-center justify-between mb-2">
                        <span class="text-gray-500 text-sm">Disk</span>
                        <span id="disk-path" class="text-xs text-gray-400">/</span>
                    </div>
                    <div class="text-2xl font-bold" id="disk-usage">--%</div>
                    <div class="mt-2 h-2 bg-gray-100 rounded-full overflow-hidden">
                        <div id="disk-bar" class="h-full bg-green-500 rounded-full transition-all" style="width: 0%"></div>
                    </div>
                    <div class="text-xs text-gray-400 mt-1"><span id="disk-used">-</span> / <span id="disk-total">-</span></div>
                </div>
            </div>
        </div>
        <script>
            function loadSystemHealth() {
                fetch('/api/health/system')
                    .then(response => response.json())
                    .then(data => {
                        // CPU
                        const cpuPercent = data.cpu.usage_percent.toFixed(0);
                        document.getElementById('cpu-usage').textContent = cpuPercent + '%';
                        document.getElementById('cpu-bar').style.width = cpuPercent + '%';
                        document.getElementById('cpu-cores').textContent = data.cpu.num_cores + ' cores';
                        document.getElementById('cpu-load').textContent =
                            data.cpu.load_avg_1.toFixed(2) + ' / ' +
                            data.cpu.load_avg_5.toFixed(2) + ' / ' +
                            data.cpu.load_avg_15.toFixed(2);

                        // Color CPU bar based on usage
                        const cpuBar = document.getElementById('cpu-bar');
                        if (cpuPercent > 80) cpuBar.className = 'h-full bg-red-500 rounded-full transition-all';
                        else if (cpuPercent > 60) cpuBar.className = 'h-full bg-yellow-500 rounded-full transition-all';
                        else cpuBar.className = 'h-full bg-blue-500 rounded-full transition-all';

                        // Memory
                        const memPercent = data.memory.used_percent.toFixed(0);
                        document.getElementById('mem-usage').textContent = memPercent + '%';
                        document.getElementById('mem-bar').style.width = memPercent + '%';
                        document.getElementById('mem-total').textContent = data.memory.total_display;
                        document.getElementById('mem-used').textContent = data.memory.used_display;
                        document.getElementById('mem-total-val').textContent = data.memory.total_display;

                        // Color memory bar
                        const memBar = document.getElementById('mem-bar');
                        if (memPercent > 85) memBar.className = 'h-full bg-red-500 rounded-full transition-all';
                        else if (memPercent > 70) memBar.className = 'h-full bg-yellow-500 rounded-full transition-all';
                        else memBar.className = 'h-full bg-purple-500 rounded-full transition-all';

                        // Disk
                        const diskPercent = data.disk.used_percent.toFixed(0);
                        document.getElementById('disk-usage').textContent = diskPercent + '%';
                        document.getElementById('disk-bar').style.width = diskPercent + '%';
                        document.getElementById('disk-path').textContent = data.disk.path;
                        document.getElementById('disk-used').textContent = data.disk.used_display;
                        document.getElementById('disk-total').textContent = data.disk.total_display;

                        // Color disk bar
                        const diskBar = document.getElementById('disk-bar');
                        if (diskPercent > 90) diskBar.className = 'h-full bg-red-500 rounded-full transition-all';
                        else if (diskPercent > 75) diskBar.className = 'h-full bg-yellow-500 rounded-full transition-all';
                        else diskBar.className = 'h-full bg-green-500 rounded-full transition-all';
                    })
                    .catch(err => console.error('Failed to load system health:', err));
            }
            loadSystemHealth();
            // Refresh every 10 seconds
            setInterval(loadSystemHealth, 10000);
        </script>`)
}

func (h *PageHandler) renderDockerContainers(w http.ResponseWriter, ctx context.Context) {
	if h.dockerClient == nil {
		return
	}

	containers, err := h.dockerClient.ListContainers(ctx, true, nil)
	if err != nil {
		slog.Error("failed to list containers", "error", err)
		return
	}

	fmt.Fprint(w, `
        <h2 class="text-xl font-bold mt-10 mb-4">Docker Containers</h2>
        <div class="bg-white shadow-sm rounded-lg border border-gray-200 overflow-hidden">
            <table class="w-full" id="containers-table">
                <thead class="bg-gray-50">
                    <tr>
                        <th class="px-4 py-3 text-left text-sm">Name</th>
                        <th class="px-4 py-3 text-left text-sm">Image</th>
                        <th class="px-4 py-3 text-left text-sm">Status</th>
                        <th class="px-4 py-3 text-left text-sm">Health</th>
                        <th class="px-4 py-3 text-left text-sm">CPU</th>
                        <th class="px-4 py-3 text-left text-sm">Memory</th>
                        <th class="px-4 py-3 text-left text-sm">Ports</th>
                    </tr>
                </thead>
                <tbody>`)

	if len(containers) == 0 {
		fmt.Fprint(w, `<tr><td colspan="7" class="px-4 py-8 text-center text-gray-500">No containers running</td></tr>`)
	} else {
		for _, c := range containers {
			name := ""
			if len(c.Names) > 0 {
				name = c.Names[0]
				if len(name) > 0 && name[0] == '/' {
					name = name[1:]
				}
			}

			// Build ports string
			ports := ""
			for _, p := range c.Ports {
				if p.PublicPort > 0 {
					if ports != "" {
						ports += ", "
					}
					ports += fmt.Sprintf("%d:%d", p.PublicPort, p.PrivatePort)
				}
			}
			if ports == "" {
				ports = "-"
			}

			// Status badge color
			statusClass := "bg-gray-100 text-gray-700"
			if c.State == "running" {
				statusClass = "bg-green-100 text-green-700"
			} else if c.State == "exited" {
				statusClass = "bg-red-100 text-red-700"
			} else if c.State == "paused" {
				statusClass = "bg-yellow-100 text-yellow-700"
			}

			// Parse health status from Status string (e.g., "Up 2 hours (healthy)")
			healthStatus := "-"
			healthClass := "text-gray-400"
			statusStr := c.Status
			if strings.Contains(statusStr, "(healthy)") {
				healthStatus = "healthy"
				healthClass = "text-green-600"
			} else if strings.Contains(statusStr, "(unhealthy)") {
				healthStatus = "unhealthy"
				healthClass = "text-red-600"
			} else if strings.Contains(statusStr, "(health: starting)") {
				healthStatus = "starting"
				healthClass = "text-yellow-600"
			} else if c.State == "running" {
				healthStatus = "no check"
				healthClass = "text-gray-400"
			}

			// Truncate image name if too long
			image := c.Image
			if len(image) > 40 {
				image = image[:37] + "..."
			}

			fmt.Fprintf(w, `
                    <tr class="border-t border-gray-200" data-container="%s">
                        <td class="px-4 py-3 text-sm font-medium">%s</td>
                        <td class="px-4 py-3 text-sm font-mono text-gray-600">%s</td>
                        <td class="px-4 py-3 text-sm">
                            <span class="px-2 py-1 text-xs rounded-full %s">%s</span>
                        </td>
                        <td class="px-4 py-3 text-sm %s">%s</td>
                        <td class="px-4 py-3 text-sm text-gray-500 cpu-stat" data-container="%s">-</td>
                        <td class="px-4 py-3 text-sm text-gray-500 mem-stat" data-container="%s">-</td>
                        <td class="px-4 py-3 text-sm font-mono">%s</td>
                    </tr>`,
				html.EscapeString(name),
				html.EscapeString(name),
				html.EscapeString(image),
				statusClass,
				html.EscapeString(c.State),
				healthClass,
				healthStatus,
				html.EscapeString(name),
				html.EscapeString(name),
				html.EscapeString(ports))
		}
	}

	fmt.Fprint(w, `
                </tbody>
            </table>
        </div>
        <script>
            function loadContainerStats() {
                fetch('/api/containers/stats')
                    .then(response => response.json())
                    .then(stats => {
                        stats.forEach(stat => {
                            const cpuCell = document.querySelector('.cpu-stat[data-container="' + stat.name + '"]');
                            const memCell = document.querySelector('.mem-stat[data-container="' + stat.name + '"]');
                            if (cpuCell) {
                                cpuCell.textContent = stat.cpu_percent.toFixed(1) + '%';
                                if (stat.cpu_percent > 80) cpuCell.className = 'px-4 py-3 text-sm text-red-600 cpu-stat';
                                else if (stat.cpu_percent > 50) cpuCell.className = 'px-4 py-3 text-sm text-yellow-600 cpu-stat';
                                else cpuCell.className = 'px-4 py-3 text-sm text-gray-600 cpu-stat';
                                cpuCell.setAttribute('data-container', stat.name);
                            }
                            if (memCell) {
                                memCell.textContent = stat.memory_display;
                                if (stat.memory_percent > 80) memCell.className = 'px-4 py-3 text-sm text-red-600 mem-stat';
                                else if (stat.memory_percent > 60) memCell.className = 'px-4 py-3 text-sm text-yellow-600 mem-stat';
                                else memCell.className = 'px-4 py-3 text-sm text-gray-600 mem-stat';
                                memCell.setAttribute('data-container', stat.name);
                            }
                        });
                    })
                    .catch(err => console.error('Failed to load container stats:', err));
            }
            loadContainerStats();
        </script>`)
}

func (h *PageHandler) renderAppCard(w http.ResponseWriter, app *models.App, latestBuild *models.Build, containerStatus *docker.ContainerStatus) {
	buildStatus := "no builds"
	statusClass := "bg-gray-50"
	if latestBuild != nil {
		buildStatus = string(latestBuild.Status)
		switch latestBuild.Status {
		case models.BuildStatusSuccess:
			statusClass = "bg-green-100 text-green-700"
		case models.BuildStatusFailed:
			statusClass = "bg-red-100 text-red-700"
		case models.BuildStatusBuilding, models.BuildStatusCloning, models.BuildStatusDeploying:
			statusClass = "bg-blue-100 text-blue-700"
		}
	}

	// Status circle - based on container status if available, otherwise build status
	statusCircle := `<span class="w-3 h-3 rounded-full bg-gray-300 mr-3"></span>` // default gray
	if containerStatus != nil {
		switch containerStatus.State {
		case "running":
			statusCircle = `<span class="w-3 h-3 rounded-full bg-green-500 mr-3"></span>`
		case "exited":
			statusCircle = `<span class="w-3 h-3 rounded-full bg-gray-400 mr-3"></span>`
		case "restarting":
			statusCircle = `<span class="w-3 h-3 rounded-full bg-yellow-500 mr-3 animate-pulse"></span>`
		default:
			statusCircle = `<span class="w-3 h-3 rounded-full bg-gray-400 mr-3"></span>`
		}
	} else if latestBuild != nil {
		switch latestBuild.Status {
		case models.BuildStatusSuccess:
			statusCircle = `<span class="w-3 h-3 rounded-full bg-green-500 mr-3"></span>`
		case models.BuildStatusFailed:
			statusCircle = `<span class="w-3 h-3 rounded-full bg-red-500 mr-3"></span>`
		case models.BuildStatusBuilding, models.BuildStatusCloning, models.BuildStatusDeploying:
			statusCircle = `<span class="w-3 h-3 rounded-full bg-blue-500 mr-3 animate-pulse"></span>`
		}
	}

	enabledBadge := ""
	if !app.Enabled {
		enabledBadge = `<span class="px-2 py-1 text-xs rounded-full bg-red-100 text-red-700 ml-2">Disabled</span>`
	}

	// Container status indicator
	containerBadge := ""
	if containerStatus != nil {
		switch containerStatus.State {
		case "running":
			containerBadge = `<span class="px-2 py-1 text-xs rounded-full bg-green-100 text-green-700 ml-2">Running</span>`
		case "exited":
			containerBadge = `<span class="px-2 py-1 text-xs rounded-full bg-gray-100 text-gray-700 ml-2">Stopped</span>`
		case "paused":
			containerBadge = `<span class="px-2 py-1 text-xs rounded-full bg-yellow-100 text-yellow-700 ml-2">Paused</span>`
		case "restarting":
			containerBadge = `<span class="px-2 py-1 text-xs rounded-full bg-blue-100 text-blue-700 ml-2">Restarting</span>`
		default:
			containerBadge = fmt.Sprintf(`<span class="px-2 py-1 text-xs rounded-full bg-gray-100 text-gray-700 ml-2">%s</span>`, html.EscapeString(containerStatus.State))
		}
	}

	// Container control buttons
	containerControls := ""
	if h.dockerClient != nil && containerStatus != nil {
		if containerStatus.State == "running" {
			containerControls = fmt.Sprintf(`
                    <button
                        class="px-3 py-1 bg-gray-50 hover:bg-gray-100 rounded text-sm border border-gray-200"
                        hx-post="/api/apps/%s/stop"
                        hx-swap="none"
                        hx-confirm="Stop container?">
                        Stop
                    </button>
                    <button
                        class="px-3 py-1 bg-gray-50 hover:bg-gray-100 rounded text-sm border border-gray-200"
                        hx-post="/api/apps/%s/restart"
                        hx-swap="none">
                        Restart
                    </button>`,
				html.EscapeString(app.ID),
				html.EscapeString(app.ID))
		} else if containerStatus.State == "exited" {
			containerControls = fmt.Sprintf(`
                    <button
                        class="px-3 py-1 bg-gray-50 hover:bg-gray-100 rounded text-sm border border-gray-200"
                        hx-post="/api/apps/%s/start"
                        hx-swap="none">
                        Start
                    </button>`,
				html.EscapeString(app.ID))
		}
	}

	fmt.Fprintf(w, `
            <div class="bg-white shadow-sm rounded-lg p-6 border border-gray-200">
                <div class="flex items-center justify-between mb-4">
                    <div class="flex items-center">
                        %s
                        <h3 class="text-lg font-semibold">%s</h3>
                    </div>
                    <div class="flex items-center">
                        <span class="px-2 py-1 text-xs rounded-full %s">%s</span>
                        %s
                        %s
                    </div>
                </div>
                <p class="text-sm text-gray-500 mb-4">%s</p>
                <div class="flex justify-between text-sm text-gray-500 mb-4">
                    <span>Branch: %s</span>
                    <span>%s</span>
                </div>
                <div class="flex space-x-2">
                    <button
                        class="px-3 py-1 bg-blue-600 hover:bg-blue-700 rounded text-sm text-white"
                        hx-post="/api/apps/%s/deploy"
                        hx-swap="none">
                        Deploy
                    </button>
                    <a href="/apps/%s" class="px-3 py-1 bg-gray-50 hover:bg-gray-100 rounded text-sm border border-gray-200 text-gray-700">
                        Details
                    </a>
                    %s
                </div>
            </div>`,
		statusCircle,
		html.EscapeString(app.Name),
		statusClass,
		html.EscapeString(buildStatus),
		enabledBadge,
		containerBadge,
		html.EscapeString(app.GetDescription()),
		html.EscapeString(app.Branch),
		html.EscapeString(string(app.BuildStrategy)),
		html.EscapeString(app.ID),
		html.EscapeString(app.ID),
		containerControls)
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

	builds, _ := h.buildQueries.ListByAppID(ctx, appID, 20, 0)

	h.writeHeader(w, r, app.Name)

	fmt.Fprintf(w, `
        <div class="flex items-center justify-between mb-6">
            <div class="flex items-center">
                <a href="/" class="text-gray-500 hover:text-gray-900 mr-4">&larr; Back</a>
                <h1 class="text-2xl font-bold">%s</h1>
            </div>
            <button
                class="px-4 py-2 bg-blue-600 hover:bg-blue-700 rounded text-white"
                hx-post="/api/apps/%s/deploy"
                hx-swap="none">
                Deploy Now
            </button>
        </div>
        <div class="bg-white shadow-sm rounded-lg p-6 border border-gray-200 mb-8">
            <div class="grid grid-cols-2 gap-4">
                <div><span class="text-gray-500">Repository:</span> <span class="ml-2">%s</span></div>
                <div><span class="text-gray-500">Branch:</span> <span class="ml-2">%s</span></div>
                <div><span class="text-gray-500">Build Strategy:</span> <span class="ml-2">%s</span></div>
                <div><span class="text-gray-500">Auto Deploy:</span> <span class="ml-2">%s</span></div>
            </div>
        </div>`,
		html.EscapeString(app.Name),
		html.EscapeString(app.ID),
		html.EscapeString(app.RepoURL),
		html.EscapeString(app.Branch),
		html.EscapeString(string(app.BuildStrategy)),
		boolToYesNo(app.AutoDeploy))

	fmt.Fprint(w, `
        <h2 class="text-xl font-bold mb-4">Build History</h2>
        <div class="bg-white shadow-sm rounded-lg border border-gray-200 overflow-hidden">
            <table class="w-full">
                <thead class="bg-gray-50">
                    <tr>
                        <th class="px-4 py-3 text-left text-sm">Status</th>
                        <th class="px-4 py-3 text-left text-sm">Commit</th>
                        <th class="px-4 py-3 text-left text-sm">Message</th>
                        <th class="px-4 py-3 text-left text-sm">Trigger</th>
                        <th class="px-4 py-3 text-left text-sm">Actions</th>
                    </tr>
                </thead>
                <tbody>`)

	for _, build := range builds {
		commitSHA := build.GetShortSHA()
		if commitSHA == "" {
			commitSHA = "-"
		}
		commitMsg := build.GetCommitMessage()
		if len(commitMsg) > 50 {
			commitMsg = commitMsg[:50] + "..."
		}
		fmt.Fprintf(w, `
                    <tr class="border-t border-gray-200">
                        <td class="px-4 py-3 text-sm">%s</td>
                        <td class="px-4 py-3 text-sm font-mono">%s</td>
                        <td class="px-4 py-3 text-sm">%s</td>
                        <td class="px-4 py-3 text-sm">%s</td>
                        <td class="px-4 py-3 text-sm">
                            <a href="/builds/%s" class="text-purple-600 hover:text-purple-700">View Logs</a>
                        </td>
                    </tr>`,
			html.EscapeString(string(build.Status)),
			html.EscapeString(commitSHA),
			html.EscapeString(commitMsg),
			html.EscapeString(string(build.Trigger)),
			html.EscapeString(build.ID))
	}

	fmt.Fprint(w, `
                </tbody>
            </table>
        </div>`)

	h.writeFooter(w)
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

	h.writeHeader(w, r, "Build "+build.ID[:8])

	fmt.Fprintf(w, `
        <div class="flex items-center mb-6">
            <a href="/apps/%s" class="text-gray-500 hover:text-gray-900 mr-4">&larr; Back</a>
            <h1 class="text-2xl font-bold">Build %s</h1>
        </div>
        <div class="bg-white shadow-sm rounded-lg p-6 border border-gray-200 mb-8">
            <div class="grid grid-cols-2 gap-4">
                <div><span class="text-gray-500">App:</span> <span class="ml-2">%s</span></div>
                <div><span class="text-gray-500">Status:</span> <span class="ml-2">%s</span></div>
                <div><span class="text-gray-500">Commit:</span> <span class="ml-2 font-mono">%s</span></div>
                <div><span class="text-gray-500">Trigger:</span> <span class="ml-2">%s</span></div>
            </div>
        </div>
        <h2 class="text-xl font-bold mb-4">Build Logs</h2>
        <div class="bg-gray-50 rounded-lg border border-gray-200 overflow-hidden">
            <div class="bg-white shadow-sm px-4 py-2 border-b border-gray-200 flex justify-between items-center">
                <h4 class="text-sm font-medium text-gray-300">Output</h4>
                <button class="text-xs text-gray-500 hover:text-gray-300" onclick="scrollToBottom()">Scroll to bottom</button>
            </div>
            <div id="log-content" class="p-4 h-96 overflow-y-auto font-mono text-sm whitespace-pre-wrap">
                Loading logs...
            </div>
        </div>
    <script>
        const logContent = document.getElementById('log-content');
        const buildID = '%s';

        function scrollToBottom() {
            logContent.scrollTop = logContent.scrollHeight;
        }

        const eventSource = new EventSource('/api/builds/' + buildID + '/logs/stream');
        logContent.innerHTML = '';

        eventSource.addEventListener('log', function(e) {
            const log = JSON.parse(e.data);
            const line = document.createElement('div');
            line.className = 'log-line ' + log.level;
            const timestamp = new Date(log.timestamp).toLocaleTimeString();
            line.innerHTML = '<span class="text-gray-600">' + timestamp + '</span> <span class="ml-2">' + escapeHtml(log.message) + '</span>';
            logContent.appendChild(line);
            scrollToBottom();
        });

        eventSource.addEventListener('complete', function(e) {
            const data = JSON.parse(e.data);
            const line = document.createElement('div');
            line.className = 'log-line text-purple-600 font-bold mt-4';
            line.textContent = 'Build ' + data.status;
            logContent.appendChild(line);
            eventSource.close();
        });

        eventSource.onerror = function() {
            eventSource.close();
        };

        function escapeHtml(text) {
            const div = document.createElement('div');
            div.textContent = text;
            return div.innerHTML;
        }
    </script>`,
		html.EscapeString(build.AppID),
		html.EscapeString(build.ID[:8]),
		html.EscapeString(build.AppName),
		html.EscapeString(string(build.Status)),
		html.EscapeString(build.GetShortSHA()),
		html.EscapeString(string(build.Trigger)),
		html.EscapeString(build.ID))

	h.writeFooter(w)
}

// Settings handles GET /settings
func (h *PageHandler) Settings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	apps, err := h.appQueries.List(ctx)
	if err != nil {
		slog.Error("failed to list apps", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.writeHeader(w, r, "Settings")

	fmt.Fprint(w, `
        <h1 class="text-2xl font-bold mb-6">Settings</h1>

        <div class="mb-8">
            <div class="flex items-center justify-between mb-4">
                <h2 class="text-xl font-bold">Applications</h2>
                <div class="flex space-x-2">
                    <button id="import-github-btn" onclick="showImportModal()" class="px-4 py-2 bg-gray-50 hover:bg-gray-100 rounded flex items-center">
                        <svg class="w-5 h-5 mr-2" fill="currentColor" viewBox="0 0 24 24"><path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z"/></svg>
                        Import from GitHub
                    </button>
                    <button id="add-app-btn" onclick="showAddForm()" class="px-4 py-2 bg-blue-600 hover:bg-blue-700 rounded text-white">
                        Add Application
                    </button>
                </div>
            </div>`)

	// Add app form (hidden by default)
	h.renderAddAppForm(w)

	// List existing apps
	if len(apps) == 0 {
		fmt.Fprint(w, `
            <div class="bg-white shadow-sm rounded-lg p-8 border border-gray-200 text-center">
                <p class="text-gray-500">No applications configured. Click "Add Application" to get started.</p>
            </div>`)
	} else {
		fmt.Fprint(w, `<div class="space-y-4">`)
		for _, app := range apps {
			h.renderAppSettings(w, app)
		}
		fmt.Fprint(w, `</div>`)
	}

	fmt.Fprint(w, `</div>`)

	// GitHub Integration
	h.renderGitHubIntegration(w)

	// Cloudflare Tunnel
	h.renderTunnelSettings(w)

	// Observability (Loki + Grafana)
	h.renderObservabilitySettings(w)

	// Import modal
	h.renderImportModal(w)

	h.writeFooter(w)
}

func (h *PageHandler) renderGitHubIntegration(w http.ResponseWriter) {
	fmt.Fprint(w, `
        <div class="mt-8">
            <h2 class="text-xl font-bold mb-4">GitHub Integration</h2>
            <div class="bg-white shadow-sm rounded-lg p-6 border border-gray-200">
                <div id="github-status">
                    <p class="text-gray-500 mb-4">Connect your GitHub account to import repositories directly.</p>
                    <div id="github-connected" class="hidden">
                        <div class="flex items-center justify-between">
                            <div class="flex items-center">
                                <svg class="w-5 h-5 text-green-600 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"></path>
                                </svg>
                                <span class="text-green-600">Connected as </span>
                                <span id="github-username" class="text-green-600 font-semibold ml-1"></span>
                            </div>
                            <button onclick="removeGitHubToken()" class="px-3 py-1 bg-red-600 hover:bg-red-700 rounded text-sm text-white">Disconnect</button>
                        </div>
                    </div>
                    <div id="github-not-connected">
                        <div id="oauth-available" class="hidden">
                            <a href="/oauth/github/login" class="inline-flex items-center px-4 py-2 bg-gray-900 hover:bg-gray-800 rounded text-white">
                                <svg class="w-5 h-5 mr-2" fill="currentColor" viewBox="0 0 24 24"><path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z"/></svg>
                                Login with GitHub
                            </a>
                        </div>
                        <div id="oauth-not-available">
                            <p class="text-gray-500">To enable GitHub login, configure OAuth in your config file:</p>
                            <pre class="mt-2 p-3 bg-gray-50 rounded text-sm font-mono text-gray-700">github_oauth:
  client_id: "your-client-id"
  client_secret: "your-secret"</pre>
                            <p class="text-xs text-gray-400 mt-2">Create an OAuth App at <a href="https://github.com/settings/developers" target="_blank" class="text-purple-600 hover:text-purple-700">github.com/settings/developers</a></p>
                        </div>
                    </div>
                </div>
            </div>
        </div>
        <script>
            // Check GitHub status on page load
            Promise.all([
                fetch('/api/settings/github-status').then(r => r.json()),
                fetch('/oauth/github/status').then(r => r.json())
            ]).then(([githubStatus, oauthStatus]) => {
                if (githubStatus.configured) {
                    document.getElementById('github-connected').classList.remove('hidden');
                    document.getElementById('github-not-connected').classList.add('hidden');
                    document.getElementById('github-username').textContent = githubStatus.username;
                } else {
                    if (oauthStatus.oauth_configured) {
                        document.getElementById('oauth-available').classList.remove('hidden');
                        document.getElementById('oauth-not-available').classList.add('hidden');
                    }
                }
            });
        </script>`)
}

func (h *PageHandler) renderTunnelSettings(w http.ResponseWriter) {
	fmt.Fprint(w, `
        <div class="mt-8">
            <h2 class="text-xl font-bold mb-4">Cloudflare Tunnel</h2>
            <div class="bg-white shadow-sm rounded-lg p-6 border border-gray-200">
                <p class="text-gray-500 mb-4">Configure a Cloudflare Tunnel to expose your apps to the internet securely.</p>

                <div id="tunnel-status-display" class="mb-4 hidden">
                    <div class="flex items-center justify-between p-3 bg-gray-50 rounded">
                        <div class="flex items-center">
                            <span id="tunnel-status-indicator" class="w-3 h-3 rounded-full mr-3"></span>
                            <span id="tunnel-status-text" class="text-sm"></span>
                        </div>
                        <div class="flex space-x-2">
                            <button id="tunnel-start-btn" onclick="startTunnel()" class="hidden px-3 py-1 bg-green-600 hover:bg-green-700 rounded text-sm text-white">Start</button>
                            <button id="tunnel-stop-btn" onclick="stopTunnel()" class="hidden px-3 py-1 bg-red-600 hover:bg-red-700 rounded text-sm text-white">Stop</button>
                        </div>
                    </div>
                </div>

                <form onsubmit="submitTunnelConfig(event)">
                    <div class="grid grid-cols-1 md:grid-cols-2 gap-4 mb-4">
                        <div class="md:col-span-2">
                            <label class="block text-sm text-gray-500 mb-1">Tunnel Token</label>
                            <input type="password" name="tunnel_token" id="tunnel-token-input"
                                placeholder="eyJhIjoiNTg2NjA..."
                                class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                            <p class="text-xs text-gray-400 mt-1">Get your tunnel token from the Cloudflare Zero Trust dashboard</p>
                        </div>
                        <div>
                            <label class="block text-sm text-gray-500 mb-1">Domain</label>
                            <input type="text" name="domain" id="tunnel-domain-input"
                                placeholder="example.com"
                                class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                            <p class="text-xs text-gray-400 mt-1">Your domain managed by Cloudflare</p>
                        </div>
                        <div>
                            <label class="block text-sm text-gray-500 mb-1">Tunnel ID (optional)</label>
                            <input type="text" name="tunnel_id" id="tunnel-id-input"
                                placeholder="abc123..."
                                class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                        </div>
                    </div>
                    <button type="submit" class="px-4 py-2 bg-blue-600 hover:bg-blue-700 rounded text-white">Save Tunnel Config</button>
                </form>

                <div class="mt-6 pt-6 border-t border-gray-200">
                    <h4 class="text-sm font-semibold mb-2">How it works</h4>
                    <ul class="text-sm text-gray-500 space-y-1 list-disc list-inside">
                        <li>Schooner will run cloudflared as a sidecar container</li>
                        <li>Configure subdomain and port in each app's settings</li>
                        <li>Apps will be exposed at subdomain.yourdomain.com</li>
                    </ul>
                </div>
            </div>
        </div>
        <script>
            // Load tunnel status on page load
            fetch('/api/settings/tunnel-status')
                .then(response => response.json())
                .then(data => {
                    const statusDisplay = document.getElementById('tunnel-status-display');
                    const indicator = document.getElementById('tunnel-status-indicator');
                    const statusText = document.getElementById('tunnel-status-text');
                    const startBtn = document.getElementById('tunnel-start-btn');
                    const stopBtn = document.getElementById('tunnel-stop-btn');
                    const domainInput = document.getElementById('tunnel-domain-input');

                    if (data.domain) {
                        domainInput.value = data.domain;
                    }

                    if (data.configured) {
                        statusDisplay.classList.remove('hidden');
                        if (data.running) {
                            indicator.className = 'w-3 h-3 rounded-full mr-3 bg-green-500';
                            statusText.textContent = 'Tunnel is running';
                            stopBtn.classList.remove('hidden');
                        } else {
                            indicator.className = 'w-3 h-3 rounded-full mr-3 bg-gray-400';
                            statusText.textContent = 'Tunnel is stopped';
                            startBtn.classList.remove('hidden');
                        }
                    }
                });

            function submitTunnelConfig(event) {
                event.preventDefault();
                const form = event.target;
                const data = {
                    tunnel_token: form.querySelector('input[name="tunnel_token"]').value,
                    domain: form.querySelector('input[name="domain"]').value,
                    tunnel_id: form.querySelector('input[name="tunnel_id"]').value
                };

                fetch('/api/settings/tunnel', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(data)
                })
                .then(response => {
                    if (response.ok) {
                        alert('Tunnel configuration saved');
                        window.location.reload();
                    } else {
                        response.text().then(text => alert('Failed to save: ' + text));
                    }
                });
            }

            function startTunnel() {
                fetch('/api/settings/tunnel/start', { method: 'POST' })
                    .then(response => {
                        if (response.ok) {
                            window.location.reload();
                        } else {
                            response.text().then(text => alert('Failed to start tunnel: ' + text));
                        }
                    });
            }

            function stopTunnel() {
                fetch('/api/settings/tunnel/stop', { method: 'POST' })
                    .then(response => {
                        if (response.ok) {
                            window.location.reload();
                        } else {
                            response.text().then(text => alert('Failed to stop tunnel: ' + text));
                        }
                    });
            }
        </script>`)
}

func (h *PageHandler) renderObservabilitySettings(w http.ResponseWriter) {
	fmt.Fprint(w, `
        <div class="mt-8">
            <h2 class="text-xl font-bold mb-4">Log Aggregation (Loki + Grafana)</h2>
            <div class="bg-white shadow-sm rounded-lg p-6 border border-gray-200">
                <p class="text-gray-500 mb-4">Deploy a managed Loki + Grafana stack to aggregate logs from all Schooner-managed containers.</p>

                <div id="observability-status-display" class="mb-4 hidden">
                    <div class="flex items-center justify-between p-3 bg-gray-50 rounded">
                        <div class="flex items-center">
                            <span id="observability-status-indicator" class="w-3 h-3 rounded-full mr-3"></span>
                            <span id="observability-status-text" class="text-sm"></span>
                        </div>
                        <div class="flex space-x-2">
                            <a id="grafana-link" href="#" target="_blank" class="hidden px-3 py-1 bg-purple-600 hover:bg-purple-700 rounded text-sm text-white">Open Grafana</a>
                            <button id="observability-start-btn" onclick="startObservability()" class="hidden px-3 py-1 bg-green-600 hover:bg-green-700 rounded text-sm text-white">Start</button>
                            <button id="observability-stop-btn" onclick="stopObservability()" class="hidden px-3 py-1 bg-red-600 hover:bg-red-700 rounded text-sm text-white">Stop</button>
                        </div>
                    </div>
                </div>

                <div id="observability-services" class="mb-4 hidden">
                    <div class="grid grid-cols-3 gap-2">
                        <div class="p-2 bg-gray-50 rounded text-center">
                            <span class="text-xs text-gray-500">Loki</span>
                            <div id="loki-status" class="text-sm font-medium">-</div>
                        </div>
                        <div class="p-2 bg-gray-50 rounded text-center">
                            <span class="text-xs text-gray-500">Promtail</span>
                            <div id="promtail-status" class="text-sm font-medium">-</div>
                        </div>
                        <div class="p-2 bg-gray-50 rounded text-center">
                            <span class="text-xs text-gray-500">Grafana</span>
                            <div id="grafana-status" class="text-sm font-medium">-</div>
                        </div>
                    </div>
                </div>

                <form onsubmit="submitObservabilityConfig(event)">
                    <div class="grid grid-cols-1 md:grid-cols-2 gap-4 mb-4">
                        <div>
                            <label class="block text-sm text-gray-500 mb-1">Grafana Port</label>
                            <input type="number" name="grafana_port" id="grafana-port-input" value="3000"
                                class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                        </div>
                        <div>
                            <label class="block text-sm text-gray-500 mb-1">Log Retention</label>
                            <select name="loki_retention" id="loki-retention-input" class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                                <option value="72h">3 days</option>
                                <option value="168h" selected>7 days</option>
                                <option value="336h">14 days</option>
                                <option value="720h">30 days</option>
                            </select>
                        </div>
                    </div>
                    <div class="flex space-x-2">
                        <button type="submit" class="px-4 py-2 bg-blue-600 hover:bg-blue-700 rounded text-white">Save & Start</button>
                    </div>
                </form>

                <div class="mt-6 pt-6 border-t border-gray-200">
                    <h4 class="text-sm font-semibold mb-2">What gets deployed</h4>
                    <ul class="text-sm text-gray-500 space-y-1 list-disc list-inside">
                        <li><strong>Loki</strong> - Log aggregation database</li>
                        <li><strong>Promtail</strong> - Log collector (reads from Docker)</li>
                        <li><strong>Grafana</strong> - Log visualization dashboard</li>
                    </ul>
                    <p class="text-xs text-gray-400 mt-2">Only logs from containers with the <code class="bg-gray-100 px-1 rounded">schooner.managed=true</code> label will be collected.</p>
                </div>
            </div>
        </div>
        <script>
            // Load observability status on page load
            function loadObservabilityStatus() {
                fetch('/api/settings/observability-status')
                    .then(response => response.json())
                    .then(data => {
                        const statusDisplay = document.getElementById('observability-status-display');
                        const servicesDisplay = document.getElementById('observability-services');
                        const statusIndicator = document.getElementById('observability-status-indicator');
                        const statusText = document.getElementById('observability-status-text');
                        const startBtn = document.getElementById('observability-start-btn');
                        const stopBtn = document.getElementById('observability-stop-btn');
                        const grafanaLink = document.getElementById('grafana-link');

                        if (data.available) {
                            statusDisplay.classList.remove('hidden');

                            if (data.running) {
                                statusIndicator.classList.add('bg-green-500');
                                statusIndicator.classList.remove('bg-gray-300', 'bg-yellow-500');
                                statusText.textContent = 'Stack running';
                                startBtn.classList.add('hidden');
                                stopBtn.classList.remove('hidden');

                                if (data.grafana_url) {
                                    grafanaLink.href = data.grafana_url;
                                    grafanaLink.classList.remove('hidden');
                                }

                                servicesDisplay.classList.remove('hidden');
                                document.getElementById('loki-status').textContent = data.loki_status || '-';
                                document.getElementById('promtail-status').textContent = data.promtail_status || '-';
                                document.getElementById('grafana-status').textContent = data.grafana_status || '-';
                            } else if (data.enabled) {
                                statusIndicator.classList.add('bg-yellow-500');
                                statusIndicator.classList.remove('bg-gray-300', 'bg-green-500');
                                statusText.textContent = 'Enabled but not running';
                                startBtn.classList.remove('hidden');
                                stopBtn.classList.add('hidden');
                                grafanaLink.classList.add('hidden');
                                servicesDisplay.classList.add('hidden');
                            } else {
                                statusIndicator.classList.add('bg-gray-300');
                                statusIndicator.classList.remove('bg-green-500', 'bg-yellow-500');
                                statusText.textContent = 'Not configured';
                                startBtn.classList.remove('hidden');
                                stopBtn.classList.add('hidden');
                                grafanaLink.classList.add('hidden');
                                servicesDisplay.classList.add('hidden');
                            }
                        }
                    });
            }
            loadObservabilityStatus();

            function submitObservabilityConfig(event) {
                event.preventDefault();
                const port = document.getElementById('grafana-port-input').value;
                const retention = document.getElementById('loki-retention-input').value;

                fetch('/api/settings/observability', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        enabled: true,
                        grafana_port: parseInt(port),
                        loki_retention: retention
                    })
                })
                .then(response => {
                    if (response.ok) {
                        // Start the stack after saving config
                        startObservability();
                    } else {
                        response.text().then(text => alert('Failed to save: ' + text));
                    }
                });
            }

            function startObservability() {
                fetch('/api/settings/observability/start', { method: 'POST' })
                    .then(response => {
                        if (response.ok) {
                            window.location.reload();
                        } else {
                            response.text().then(text => alert('Failed to start: ' + text));
                        }
                    });
            }

            function stopObservability() {
                fetch('/api/settings/observability/stop', { method: 'POST' })
                    .then(response => {
                        if (response.ok) {
                            window.location.reload();
                        } else {
                            response.text().then(text => alert('Failed to stop: ' + text));
                        }
                    });
            }
        </script>`)
}

func (h *PageHandler) renderImportModal(w http.ResponseWriter) {
	fmt.Fprint(w, `
        <div id="import-modal" class="hidden fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
            <div class="bg-white shadow-sm rounded-lg w-full max-w-2xl max-h-[80vh] overflow-hidden">
                <div class="flex items-center justify-between p-4 border-b border-gray-200">
                    <h3 class="text-lg font-semibold">Import from GitHub</h3>
                    <button onclick="hideImportModal()" class="text-gray-500 hover:text-gray-900 text-2xl">&times;</button>
                </div>

                <div id="repo-selection">
                    <div class="p-4 border-b border-gray-200">
                        <input type="text" id="repo-search" placeholder="Search repositories..."
                               class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900"
                               oninput="filterRepos(this.value)">
                    </div>
                    <div id="github-repos-list" class="overflow-y-auto max-h-80">
                        <div class="text-center py-8 text-gray-500">Loading repositories...</div>
                    </div>
                </div>

                <div id="import-config" class="hidden p-4">
                    <div class="mb-4">
                        <button onclick="backToRepoList()" class="text-gray-500 hover:text-gray-900 text-sm">&larr; Back to repository list</button>
                    </div>
                    <div class="mb-4">
                        <span class="text-gray-500">Selected repository:</span>
                        <span id="import-repo-name" class="font-semibold ml-2"></span>
                    </div>
                    <form onsubmit="submitImport(event)">
                        <input type="hidden" name="repo_full_name" id="import-repo-fullname">
                        <div class="grid grid-cols-2 gap-4 mb-4">
                            <div>
                                <label class="block text-sm text-gray-500 mb-1">Branch</label>
                                <input type="text" name="branch" id="import-branch" class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                            </div>
                            <div>
                                <label class="block text-sm text-gray-500 mb-1">Build Strategy</label>
                                <select name="build_strategy" id="import-build-strategy" class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                                    <option value="dockerfile">Dockerfile</option>
                                    <option value="compose">Docker Compose</option>
                                </select>
                            </div>
                        </div>
                        <div class="mb-4">
                            <label class="flex items-center">
                                <input type="checkbox" name="auto_deploy" checked class="mr-2">
                                <span class="text-sm text-gray-500">Auto deploy on push</span>
                            </label>
                        </div>
                        <div class="flex justify-end space-x-2">
                            <button type="button" onclick="hideImportModal()" class="px-4 py-2 bg-gray-50 hover:bg-gray-100 rounded text-gray-700 border border-gray-200">Cancel</button>
                            <button type="submit" class="px-4 py-2 bg-blue-600 hover:bg-blue-700 rounded text-white">Import & Deploy</button>
                        </div>
                    </form>
                </div>
            </div>
        </div>`)
}

func (h *PageHandler) renderAddAppForm(w http.ResponseWriter) {
	fmt.Fprint(w, `
            <div id="add-app-form" class="hidden bg-white shadow-sm rounded-lg p-6 border border-gray-200 mb-4">
                <div class="flex items-center justify-between mb-4">
                    <h3 class="text-lg font-semibold">Add New Application</h3>
                    <button onclick="hideAddForm()" class="text-gray-500 hover:text-gray-900">&times;</button>
                </div>
                <form onsubmit="submitAddApp(event)">
                    <div class="grid grid-cols-2 gap-4">
                        <div>
                            <label class="block text-sm text-gray-500 mb-1">Name *</label>
                            <input type="text" name="name" required class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                        </div>
                        <div>
                            <label class="block text-sm text-gray-500 mb-1">Description</label>
                            <input type="text" name="description" class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                        </div>
                        <div>
                            <label class="block text-sm text-gray-500 mb-1">Repository URL *</label>
                            <input type="text" name="repo_url" required placeholder="https://github.com/user/repo.git" class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                        </div>
                        <div>
                            <label class="block text-sm text-gray-500 mb-1">Branch</label>
                            <input type="text" name="branch" placeholder="main" class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                        </div>
                        <div>
                            <label class="block text-sm text-gray-500 mb-1">Build Strategy</label>
                            <select name="build_strategy" class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                                <option value="dockerfile">Dockerfile</option>
                                <option value="compose">Docker Compose</option>
                                <option value="buildpacks">Buildpacks</option>
                            </select>
                        </div>
                        <div>
                            <label class="block text-sm text-gray-500 mb-1">Webhook Secret</label>
                            <input type="text" name="webhook_secret" class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                        </div>
                        <div>
                            <label class="block text-sm text-gray-500 mb-1">Dockerfile Path</label>
                            <input type="text" name="dockerfile_path" placeholder="Dockerfile" class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                        </div>
                        <div>
                            <label class="block text-sm text-gray-500 mb-1">Build Context</label>
                            <input type="text" name="build_context" placeholder="." class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                        </div>
                        <div>
                            <label class="block text-sm text-gray-500 mb-1">Container Name</label>
                            <input type="text" name="container_name" class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                        </div>
                        <div>
                            <label class="block text-sm text-gray-500 mb-1">Image Name</label>
                            <input type="text" name="image_name" class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                        </div>
                        <div class="col-span-2 border-t border-gray-200 pt-4 mt-2">
                            <h4 class="text-sm font-semibold text-gray-600 mb-3">Cloudflare Tunnel (Optional)</h4>
                            <div class="grid grid-cols-2 gap-4">
                                <div>
                                    <label class="block text-sm text-gray-500 mb-1">Subdomain</label>
                                    <input type="text" name="subdomain" placeholder="myapp" class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                                    <p class="text-xs text-gray-400 mt-1">e.g., myapp for myapp.yourdomain.com</p>
                                </div>
                                <div>
                                    <label class="block text-sm text-gray-500 mb-1">Public Port</label>
                                    <input type="number" name="public_port" placeholder="8080" class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                                    <p class="text-xs text-gray-400 mt-1">Container port to expose via tunnel</p>
                                </div>
                            </div>
                        </div>
                        <div class="col-span-2">
                            <label class="block text-sm text-gray-500 mb-1">Environment Variables</label>
                            <textarea name="env_vars" rows="3" placeholder="KEY=value&#10;ANOTHER_KEY=another_value" class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900 font-mono text-sm"></textarea>
                            <p class="text-xs text-gray-400 mt-1">One per line: KEY=value</p>
                        </div>
                        <div class="flex items-center space-x-4 col-span-2">
                            <label class="flex items-center">
                                <input type="checkbox" name="auto_deploy" checked class="mr-2">
                                <span class="text-sm text-gray-500">Auto Deploy on Push</span>
                            </label>
                            <label class="flex items-center">
                                <input type="checkbox" name="enabled" checked class="mr-2">
                                <span class="text-sm text-gray-500">Enabled</span>
                            </label>
                        </div>
                    </div>
                    <div class="flex justify-end space-x-2 mt-4">
                        <button type="button" onclick="hideAddForm()" class="px-4 py-2 bg-gray-50 hover:bg-gray-100 rounded border border-gray-200">Cancel</button>
                        <button type="submit" class="px-4 py-2 bg-blue-600 hover:bg-blue-700 rounded text-white">Add Application</button>
                    </div>
                </form>
            </div>`)
}

func (h *PageHandler) renderAppSettings(w http.ResponseWriter, app *models.App) {
	enabledClass := "bg-green-100 text-green-700"
	enabledText := "Enabled"
	if !app.Enabled {
		enabledClass = "bg-red-100 text-red-700"
		enabledText = "Disabled"
	}

	fmt.Fprintf(w, `
                <div class="bg-white shadow-sm rounded-lg border border-gray-200">
                    <div class="p-4 flex items-center justify-between cursor-pointer" onclick="toggleEditForm('%s')">
                        <div class="flex items-center space-x-4">
                            <h3 class="font-semibold">%s</h3>
                            <span class="px-2 py-1 text-xs rounded-full %s">%s</span>
                            <span class="text-gray-500 text-sm">%s</span>
                        </div>
                        <div class="flex items-center space-x-2">
                            <span class="text-gray-500 text-sm">%s</span>
                            <svg class="w-5 h-5 text-gray-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"></path>
                            </svg>
                        </div>
                    </div>
                    <div id="edit-form-%s" class="hidden border-t border-gray-200 p-4">
                        <form onsubmit="submitEditApp(event, '%s')">
                            <div class="grid grid-cols-2 gap-4">
                                <div>
                                    <label class="block text-sm text-gray-500 mb-1">Name</label>
                                    <input type="text" name="name" value="%s" class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                                </div>
                                <div>
                                    <label class="block text-sm text-gray-500 mb-1">Description</label>
                                    <input type="text" name="description" value="%s" class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                                </div>
                                <div>
                                    <label class="block text-sm text-gray-500 mb-1">Repository URL</label>
                                    <input type="text" name="repo_url" value="%s" class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                                </div>
                                <div>
                                    <label class="block text-sm text-gray-500 mb-1">Branch</label>
                                    <input type="text" name="branch" value="%s" class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                                </div>
                                <div>
                                    <label class="block text-sm text-gray-500 mb-1">Build Strategy</label>
                                    <select name="build_strategy" class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                                        <option value="dockerfile" %s>Dockerfile</option>
                                        <option value="compose" %s>Docker Compose</option>
                                        <option value="buildpacks" %s>Buildpacks</option>
                                    </select>
                                </div>
                                <div>
                                    <label class="block text-sm text-gray-500 mb-1">Webhook Secret</label>
                                    <input type="text" name="webhook_secret" value="%s" class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                                </div>
                                <div>
                                    <label class="block text-sm text-gray-500 mb-1">Dockerfile Path</label>
                                    <input type="text" name="dockerfile_path" value="%s" class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                                </div>
                                <div>
                                    <label class="block text-sm text-gray-500 mb-1">Build Context</label>
                                    <input type="text" name="build_context" value="%s" class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                                </div>
                                <div>
                                    <label class="block text-sm text-gray-500 mb-1">Container Name</label>
                                    <input type="text" name="container_name" value="%s" class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                                </div>
                                <div>
                                    <label class="block text-sm text-gray-500 mb-1">Image Name</label>
                                    <input type="text" name="image_name" value="%s" class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                                </div>
                                <div class="col-span-2 border-t border-gray-200 pt-4 mt-2">
                                    <h4 class="text-sm font-semibold text-gray-600 mb-3">Cloudflare Tunnel (Optional)</h4>
                                    <div class="grid grid-cols-2 gap-4">
                                        <div>
                                            <label class="block text-sm text-gray-500 mb-1">Subdomain</label>
                                            <input type="text" name="subdomain" value="%s" placeholder="myapp" class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                                            <p class="text-xs text-gray-400 mt-1">e.g., myapp for myapp.yourdomain.com</p>
                                        </div>
                                        <div>
                                            <label class="block text-sm text-gray-500 mb-1">Public Port</label>
                                            <input type="number" name="public_port" value="%s" placeholder="8080" class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                                            <p class="text-xs text-gray-400 mt-1">Container port to expose via tunnel</p>
                                        </div>
                                    </div>
                                </div>
                                <div class="col-span-2">
                                    <label class="block text-sm text-gray-500 mb-1">Environment Variables</label>
                                    <textarea name="env_vars" rows="3" placeholder="KEY=value&#10;ANOTHER_KEY=another_value" class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900 font-mono text-sm">%s</textarea>
                                    <p class="text-xs text-gray-400 mt-1">One per line: KEY=value</p>
                                </div>
                                <div class="flex items-center space-x-4 col-span-2">
                                    <label class="flex items-center">
                                        <input type="checkbox" name="auto_deploy" %s class="mr-2">
                                        <span class="text-sm text-gray-500">Auto Deploy</span>
                                    </label>
                                    <label class="flex items-center">
                                        <input type="checkbox" name="enabled" %s class="mr-2">
                                        <span class="text-sm text-gray-500">Enabled</span>
                                    </label>
                                </div>
                            </div>
                            <div class="flex justify-between mt-4">
                                <div class="flex space-x-2">
                                    <button type="button" onclick="confirmDelete('%s', '%s')" class="px-4 py-2 bg-red-600 hover:bg-red-700 rounded text-white">Delete</button>
                                    <button type="button" onclick="configureWebhook('%s', '%s')" class="px-4 py-2 bg-purple-600 hover:bg-purple-700 rounded text-white">Configure Webhook</button>
                                </div>
                                <div class="flex space-x-2">
                                    <button type="button" onclick="toggleEditForm('%s')" class="px-4 py-2 bg-gray-50 hover:bg-gray-100 rounded border border-gray-200 text-gray-700">Cancel</button>
                                    <button type="submit" class="px-4 py-2 bg-blue-600 hover:bg-blue-700 rounded text-white">Save Changes</button>
                                </div>
                            </div>
                        </form>
                    </div>
                </div>`,
		app.ID,
		html.EscapeString(app.Name),
		enabledClass,
		enabledText,
		html.EscapeString(string(app.BuildStrategy)),
		html.EscapeString(app.Branch),
		app.ID,
		app.ID,
		html.EscapeString(app.Name),
		html.EscapeString(app.GetDescription()),
		html.EscapeString(app.RepoURL),
		html.EscapeString(app.Branch),
		selected(app.BuildStrategy == models.BuildStrategyDockerfile),
		selected(app.BuildStrategy == models.BuildStrategyCompose),
		selected(app.BuildStrategy == models.BuildStrategyBuildpacks),
		html.EscapeString(app.GetWebhookSecret()),
		html.EscapeString(app.DockerfilePath),
		html.EscapeString(app.BuildContext),
		html.EscapeString(app.GetContainerName()),
		html.EscapeString(app.GetImageName()),
		html.EscapeString(app.GetSubdomain()),
		formatPort(app.GetPublicPort()),
		html.EscapeString(app.GetEnvVarsAsString()),
		checked(app.AutoDeploy),
		checked(app.Enabled),
		app.ID,
		html.EscapeString(app.Name),
		app.ID,
		html.EscapeString(app.Name),
		app.ID)
}

func boolToYesNo(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

func checked(b bool) string {
	if b {
		return "checked"
	}
	return ""
}

func selected(b bool) string {
	if b {
		return "selected"
	}
	return ""
}

func formatPort(port int) string {
	if port == 0 {
		return ""
	}
	return fmt.Sprintf("%d", port)
}
