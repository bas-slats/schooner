package handlers

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/go-chi/chi/v5"

	"schooner/internal/auth"
	"schooner/internal/cloudflare"
	"schooner/internal/config"
	"schooner/internal/database/queries"
	"schooner/internal/docker"
	"schooner/internal/models"
	"schooner/internal/observability"
	"schooner/internal/version"
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
	// Get session for user display
	username := ""
	avatarURL := ""
	if session := auth.GetSession(r.Context()); session != nil {
		username = session.Username
		avatarURL = session.AvatarURL
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
            <div class="flex items-center space-x-6">
                <a href="/" class="text-gray-600 hover:text-gray-900 text-sm font-medium">Dashboard</a>
                <a href="/settings" class="text-gray-600 hover:text-gray-900 text-sm font-medium">Settings</a>
                <div class="flex items-center space-x-3 pl-6 border-l border-gray-200">
                    <a href="https://github.com/%s" target="_blank" class="flex items-center space-x-2 group">
                        <img src="%s" alt="%s" class="h-8 w-8 rounded-full ring-2 ring-gray-100 group-hover:ring-gray-200 transition-all">
                        <span class="text-gray-700 text-sm font-medium group-hover:text-gray-900">%s</span>
                    </a>
                    <a href="/logout" class="text-gray-400 hover:text-gray-600 transition-colors" title="Logout">
                        <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1"></path>
                        </svg>
                    </a>
                </div>
            </div>
        </div>
    </nav>
    <main class="max-w-7xl mx-auto px-6 py-8">
`, html.EscapeString(title), html.EscapeString(username), html.EscapeString(avatarURL), html.EscapeString(username), html.EscapeString(username))
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
    <footer class="border-t border-gray-200 mt-12">
        <div class="max-w-7xl mx-auto px-6 py-6">
            <div class="flex flex-col sm:flex-row items-center justify-between gap-4">
                <div class="flex items-center space-x-2">
                    <svg class="w-5 h-5 text-gray-400" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
                        <path stroke-linecap="round" stroke-linejoin="round" d="M12 21a9.004 9.004 0 008.716-6.747M12 21a9.004 9.004 0 01-8.716-6.747M12 21c2.485 0 4.5-4.03 4.5-9S14.485 3 12 3m0 18c-2.485 0-4.5-4.03-4.5-9S9.515 3 12 3m0 0a8.997 8.997 0 017.843 4.582M12 3a8.997 8.997 0 00-7.843 4.582m15.686 0A11.953 11.953 0 0112 10.5c-2.998 0-5.74-1.1-7.843-2.918m15.686 0A8.959 8.959 0 0121 12c0 .778-.099 1.533-.284 2.253m0 0A17.919 17.919 0 0112 16.5c-3.162 0-6.133-.815-8.716-2.247m0 0A9.015 9.015 0 013 12c0-1.605.42-3.113 1.157-4.418"/>
                    </svg>
                    <span class="text-sm font-medium text-gray-600">Schooner</span>
                </div>
                <div class="flex items-center space-x-4 text-xs text-gray-400">
                    <a href="https://github.com/bas-slats/schooner/commit/` + version.Commit + `"
                       target="_blank"
                       class="font-mono hover:text-gray-600 transition-colors">
                        ` + version.GetShortCommit() + `
                    </a>
                    <span class="text-gray-300">|</span>
                    <a href="https://github.com/bas-slats/schooner"
                       target="_blank"
                       class="hover:text-gray-600 transition-colors flex items-center gap-1">
                        <svg class="w-4 h-4" fill="currentColor" viewBox="0 0 24 24"><path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z"/></svg>
                        GitHub
                    </a>
                </div>
            </div>
        </div>
    </footer>
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
				buildStatusBadge(build.Status),
				commitLink(build.AppRepoURL, build.GetCommitSHA()),
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

// containerGroup represents a group of related containers
type containerGroup struct {
	Name       string
	Icon       string
	Containers []types.Container
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

	// Group containers by type
	groups := h.groupContainers(containers)

	fmt.Fprint(w, `
        <h2 class="text-xl font-bold mt-10 mb-4">Docker Containers</h2>
        <div class="space-y-4" id="container-groups">`)

	if len(containers) == 0 {
		fmt.Fprint(w, `<div class="bg-white shadow-sm rounded-lg border border-gray-200 p-8 text-center text-gray-500">No containers found</div>`)
	} else {
		for _, group := range groups {
			if len(group.Containers) == 0 {
				continue
			}
			h.renderContainerGroup(w, group)
		}
	}

	fmt.Fprint(w, `
        </div>
        <script>
            function toggleGroup(groupId) {
                const content = document.getElementById('group-content-' + groupId);
                const arrow = document.getElementById('group-arrow-' + groupId);
                if (content.classList.contains('hidden')) {
                    content.classList.remove('hidden');
                    arrow.classList.remove('-rotate-90');
                } else {
                    content.classList.add('hidden');
                    arrow.classList.add('-rotate-90');
                }
            }
            function formatBytes(bytes) {
                if (bytes >= 1073741824) return (bytes / 1073741824).toFixed(1) + ' GB';
                if (bytes >= 1048576) return (bytes / 1048576).toFixed(1) + ' MB';
                if (bytes >= 1024) return (bytes / 1024).toFixed(1) + ' KB';
                return bytes + ' B';
            }
            function loadContainerStats() {
                fetch('/api/containers/stats')
                    .then(response => response.json())
                    .then(stats => {
                        const groupStats = {};
                        stats.forEach(stat => {
                            const cpuCell = document.querySelector('.cpu-stat[data-container="' + stat.name + '"]');
                            const memCell = document.querySelector('.mem-stat[data-container="' + stat.name + '"]');
                            if (cpuCell) {
                                cpuCell.textContent = stat.cpu_percent.toFixed(1) + '%';
                                if (stat.cpu_percent > 80) cpuCell.className = 'px-4 py-2 text-xs text-red-600 cpu-stat';
                                else if (stat.cpu_percent > 50) cpuCell.className = 'px-4 py-2 text-xs text-yellow-600 cpu-stat';
                                else cpuCell.className = 'px-4 py-2 text-xs text-gray-600 cpu-stat';
                                cpuCell.setAttribute('data-container', stat.name);
                                // Track group stats
                                const row = cpuCell.closest('tr');
                                const groupContent = row.closest('[id^="group-content-"]');
                                if (groupContent) {
                                    const groupId = groupContent.id.replace('group-content-', '');
                                    if (!groupStats[groupId]) groupStats[groupId] = { cpu: 0, mem: 0 };
                                    groupStats[groupId].cpu += stat.cpu_percent;
                                    groupStats[groupId].mem += stat.memory_usage;
                                }
                            }
                            if (memCell) {
                                memCell.textContent = stat.memory_display;
                                if (stat.memory_percent > 80) memCell.className = 'px-4 py-2 text-xs text-red-600 mem-stat';
                                else if (stat.memory_percent > 60) memCell.className = 'px-4 py-2 text-xs text-yellow-600 mem-stat';
                                else memCell.className = 'px-4 py-2 text-xs text-gray-600 mem-stat';
                                memCell.setAttribute('data-container', stat.name);
                            }
                        });
                        // Update group totals
                        Object.keys(groupStats).forEach(groupId => {
                            const cpuTotal = document.getElementById('group-cpu-' + groupId);
                            const memTotal = document.getElementById('group-mem-' + groupId);
                            if (cpuTotal) cpuTotal.textContent = groupStats[groupId].cpu.toFixed(1) + '%';
                            if (memTotal) memTotal.textContent = formatBytes(groupStats[groupId].mem);
                        });
                    })
                    .catch(err => console.error('Failed to load container stats:', err));
            }
            loadContainerStats();
            setInterval(loadContainerStats, 5000);
        </script>`)
}

// groupContainers groups containers by their schooner labels
func (h *PageHandler) groupContainers(containers []types.Container) []containerGroup {
	appContainers := make(map[string][]types.Container) // grouped by app name
	serviceContainers := []types.Container{}
	otherContainers := []types.Container{}

	for _, c := range containers {
		if appName, ok := c.Labels["schooner.app"]; ok {
			appContainers[appName] = append(appContainers[appName], c)
		} else if _, ok := c.Labels["schooner.service"]; ok {
			serviceContainers = append(serviceContainers, c)
		} else {
			otherContainers = append(otherContainers, c)
		}
	}

	var groups []containerGroup

	// Add app groups
	for appName, containers := range appContainers {
		groups = append(groups, containerGroup{
			Name:       appName,
			Icon:       "📦",
			Containers: containers,
		})
	}

	// Add schooner services group
	if len(serviceContainers) > 0 {
		groups = append(groups, containerGroup{
			Name:       "Schooner Services",
			Icon:       "⚙️",
			Containers: serviceContainers,
		})
	}

	// Add other containers group
	if len(otherContainers) > 0 {
		groups = append(groups, containerGroup{
			Name:       "Other Containers",
			Icon:       "🐳",
			Containers: otherContainers,
		})
	}

	return groups
}

// renderContainerGroup renders a collapsible group of containers
func (h *PageHandler) renderContainerGroup(w http.ResponseWriter, group containerGroup) {
	// Count running containers
	runningCount := 0
	for _, c := range group.Containers {
		if c.State == "running" {
			runningCount++
		}
	}

	// Group status color
	statusColor := "bg-gray-400"
	if runningCount == len(group.Containers) {
		statusColor = "bg-green-500"
	} else if runningCount > 0 {
		statusColor = "bg-yellow-500"
	} else {
		statusColor = "bg-red-500"
	}

	groupID := strings.ReplaceAll(group.Name, " ", "-")

	fmt.Fprintf(w, `
        <div class="bg-white shadow-sm rounded-lg border border-gray-200 overflow-hidden">
            <div class="px-4 py-3 bg-gray-50 border-b border-gray-200 cursor-pointer flex items-center justify-between hover:bg-gray-100 transition-colors" onclick="toggleGroup('%s')">
                <div class="flex items-center gap-3">
                    <svg id="group-arrow-%s" class="w-4 h-4 text-gray-500 transition-transform" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"/>
                    </svg>
                    <span class="w-2 h-2 rounded-full %s"></span>
                    <span class="text-sm font-medium">%s %s</span>
                    <span class="text-xs text-gray-500">(%d container%s)</span>
                </div>
                <div class="flex items-center gap-4 text-xs text-gray-500">
                    <span>CPU: <span id="group-cpu-%s" class="font-medium">-</span></span>
                    <span>Mem: <span id="group-mem-%s" class="font-medium">-</span></span>
                    <span>%d/%d running</span>
                </div>
            </div>
            <div id="group-content-%s">
                <table class="w-full">
                    <thead class="bg-gray-50 text-xs text-gray-500">
                        <tr>
                            <th class="px-4 py-2 text-left font-medium">Name</th>
                            <th class="px-4 py-2 text-left font-medium">Image</th>
                            <th class="px-4 py-2 text-left font-medium">Status</th>
                            <th class="px-4 py-2 text-left font-medium">CPU</th>
                            <th class="px-4 py-2 text-left font-medium">Memory</th>
                            <th class="px-4 py-2 text-left font-medium">Ports</th>
                        </tr>
                    </thead>
                    <tbody class="text-sm">`,
		html.EscapeString(groupID),
		html.EscapeString(groupID),
		statusColor,
		group.Icon,
		html.EscapeString(group.Name),
		len(group.Containers),
		pluralize(len(group.Containers)),
		html.EscapeString(groupID),
		html.EscapeString(groupID),
		runningCount,
		len(group.Containers),
		html.EscapeString(groupID))

	for _, c := range group.Containers {
		h.renderContainerRow(w, c)
	}

	fmt.Fprint(w, `
                    </tbody>
                </table>
            </div>
        </div>`)
}

// renderContainerRow renders a single container row
func (h *PageHandler) renderContainerRow(w http.ResponseWriter, c types.Container) {
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

	// Truncate image name if too long
	image := c.Image
	if len(image) > 35 {
		image = image[:32] + "..."
	}

	fmt.Fprintf(w, `
                        <tr class="border-t border-gray-100 hover:bg-gray-50" data-container="%s">
                            <td class="px-4 py-2 text-sm font-medium text-gray-900">%s</td>
                            <td class="px-4 py-2 text-xs font-mono text-gray-500">%s</td>
                            <td class="px-4 py-2">
                                <span class="px-2 py-0.5 text-xs rounded-full %s">%s</span>
                            </td>
                            <td class="px-4 py-2 text-xs text-gray-500 cpu-stat" data-container="%s">-</td>
                            <td class="px-4 py-2 text-xs text-gray-500 mem-stat" data-container="%s">-</td>
                            <td class="px-4 py-2 text-xs font-mono text-gray-500">%s</td>
                        </tr>`,
		html.EscapeString(name),
		html.EscapeString(name),
		html.EscapeString(image),
		statusClass,
		html.EscapeString(c.State),
		html.EscapeString(name),
		html.EscapeString(name),
		html.EscapeString(ports))
}

func pluralize(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
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

	builds, _ := h.buildQueries.ListByAppID(ctx, appID, 10, 0)

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
			buildStatusBadge(build.Status),
			commitLink(build.AppRepoURL, build.GetCommitSHA()),
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

	// Prepare duration info
	var startedAtJS, finishedAtJS string
	isRunning := build.IsRunning()
	if build.StartedAt.Valid {
		startedAtJS = build.StartedAt.Time.Format(time.RFC3339)
	}
	if build.FinishedAt.Valid {
		finishedAtJS = build.FinishedAt.Time.Format(time.RFC3339)
	}

	fmt.Fprintf(w, `
        <div class="flex items-center mb-6">
            <a href="/apps/%s" class="text-gray-500 hover:text-gray-900 mr-4">&larr; Back</a>
            <h1 class="text-2xl font-bold">Build %s</h1>
        </div>
        <div class="bg-white shadow-sm rounded-lg p-6 border border-gray-200 mb-8">
            <div class="grid grid-cols-2 gap-4 mb-4">
                <div><span class="text-gray-500">App:</span> <span class="ml-2">%s</span></div>
                <div><span class="text-gray-500">Status:</span> <span class="ml-2">%s</span></div>
                <div><span class="text-gray-500">Commit:</span> <span class="ml-2 font-mono">%s</span></div>
                <div><span class="text-gray-500">Trigger:</span> <span class="ml-2">%s</span></div>
            </div>
            <div id="duration-bar" class="pt-4 border-t border-gray-200 text-sm font-medium"></div>
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
        const durationBar = document.getElementById('duration-bar');
        const buildID = '%s';
        const startedAt = '%s';
        const finishedAt = '%s';
        let isRunning = %t;
        let durationInterval;

        function formatDuration(ms) {
            const seconds = Math.floor(ms / 1000);
            const minutes = Math.floor(seconds / 60);
            const remainingSeconds = seconds %% 60;
            if (minutes > 0) {
                return minutes + ' min, ' + remainingSeconds + ' sec';
            }
            return remainingSeconds + ' sec';
        }

        function updateDuration() {
            if (!startedAt) {
                durationBar.innerHTML = '<span class="text-gray-500">Waiting to start...</span>';
                return;
            }
            const start = new Date(startedAt);
            if (isRunning) {
                const elapsed = Date.now() - start.getTime();
                durationBar.innerHTML = '<span class="text-blue-600"><svg class="inline w-4 h-4 mr-1 animate-spin" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path></svg>Running for ' + formatDuration(elapsed) + '</span>';
            } else if (finishedAt) {
                const end = new Date(finishedAt);
                const duration = end.getTime() - start.getTime();
                durationBar.innerHTML = '<span class="text-green-600">Completed in ' + formatDuration(duration) + '</span>';
            }
        }

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
            isRunning = false;
            if (durationInterval) clearInterval(durationInterval);
            // Update duration with final time
            if (data.started_at && data.finished_at) {
                const start = new Date(data.started_at);
                const end = new Date(data.finished_at);
                const duration = end.getTime() - start.getTime();
                const statusColor = data.status === 'success' ? 'text-green-600' : 'text-red-600';
                const statusText = data.status === 'success' ? 'Completed' : 'Failed';
                durationBar.innerHTML = '<span class="' + statusColor + '">' + statusText + ' in ' + formatDuration(duration) + '</span>';
            }
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

        // Start duration updates
        updateDuration();
        if (isRunning) {
            durationInterval = setInterval(updateDuration, 1000);
        }
    </script>`,
		html.EscapeString(build.AppID),
		html.EscapeString(build.ID[:8]),
		html.EscapeString(build.AppName),
		buildStatusBadge(build.Status),
		html.EscapeString(build.GetShortSHA()),
		html.EscapeString(string(build.Trigger)),
		html.EscapeString(build.ID),
		startedAtJS,
		finishedAtJS,
		isRunning)

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
                        <div class="md:col-span-2">
                            <label class="block text-sm text-gray-500 mb-1">API Token (optional)</label>
                            <input type="password" name="api_token" id="tunnel-api-token-input"
                                placeholder="your-cloudflare-api-token"
                                class="w-full bg-gray-50 border border-gray-200 rounded px-3 py-2 text-gray-900">
                            <p class="text-xs text-gray-400 mt-1">For automatic DNS management. Create at <a href="https://dash.cloudflare.com/profile/api-tokens" target="_blank" class="text-purple-600 hover:text-purple-700">Cloudflare API Tokens</a> with Zone:DNS:Edit permission</p>
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
                    tunnel_id: form.querySelector('input[name="tunnel_id"]').value,
                    api_token: form.querySelector('input[name="api_token"]').value
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
                                <option value="autodetect">Autodetect</option>
                                <option value="dockerfile">Dockerfile</option>
                                <option value="compose">Docker Compose</option>
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
                                        <option value="autodetect" %s>Autodetect</option>
                                        <option value="dockerfile" %s>Dockerfile</option>
                                        <option value="compose" %s>Docker Compose</option>
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
                                    %s
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
		selected(app.BuildStrategy == models.BuildStrategyAutodetect),
		selected(app.BuildStrategy == models.BuildStrategyDockerfile),
		selected(app.BuildStrategy == models.BuildStrategyCompose),
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
		webhookButton(app),
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

func webhookButton(app *models.App) string {
	if app.GetWebhookSecret() != "" {
		return ""
	}
	return fmt.Sprintf(`<button type="button" onclick="configureWebhook('%s', '%s')" class="px-4 py-2 bg-purple-600 hover:bg-purple-700 rounded text-white">Configure Webhook</button>`,
		app.ID, html.EscapeString(app.Name))
}

func commitLink(repoURL, sha string) string {
	if sha == "" || sha == "-" {
		return "-"
	}
	// Convert repo URL to GitHub web URL
	// https://github.com/user/repo.git -> https://github.com/user/repo/commit/SHA
	webURL := strings.TrimSuffix(repoURL, ".git")
	webURL = strings.Replace(webURL, "git@github.com:", "https://github.com/", 1)
	if !strings.HasPrefix(webURL, "https://") {
		webURL = "https://" + webURL
	}
	shortSHA := sha
	if len(sha) > 8 {
		shortSHA = sha[:8]
	}
	return fmt.Sprintf(`<a href="%s/commit/%s" target="_blank" class="text-purple-600 hover:text-purple-700 hover:underline">%s</a>`,
		html.EscapeString(webURL), html.EscapeString(sha), html.EscapeString(shortSHA))
}

func buildStatusBadge(status models.BuildStatus) string {
	var bgClass, textClass, icon string
	switch status {
	case models.BuildStatusSuccess:
		bgClass = "bg-green-100"
		textClass = "text-green-700"
		icon = `<svg class="w-3 h-3 mr-1" fill="currentColor" viewBox="0 0 20 20"><path fill-rule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clip-rule="evenodd"></path></svg>`
	case models.BuildStatusFailed:
		bgClass = "bg-red-100"
		textClass = "text-red-700"
		icon = `<svg class="w-3 h-3 mr-1" fill="currentColor" viewBox="0 0 20 20"><path fill-rule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clip-rule="evenodd"></path></svg>`
	case models.BuildStatusBuilding, models.BuildStatusCloning, models.BuildStatusDeploying:
		bgClass = "bg-blue-100"
		textClass = "text-blue-700"
		icon = `<svg class="w-3 h-3 mr-1 animate-spin" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg>`
	case models.BuildStatusPending:
		bgClass = "bg-yellow-100"
		textClass = "text-yellow-700"
		icon = `<svg class="w-3 h-3 mr-1" fill="currentColor" viewBox="0 0 20 20"><path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm1-12a1 1 0 10-2 0v4a1 1 0 00.293.707l2.828 2.829a1 1 0 101.415-1.415L11 9.586V6z" clip-rule="evenodd"></path></svg>`
	case models.BuildStatusCancelled:
		bgClass = "bg-gray-100"
		textClass = "text-gray-700"
		icon = `<svg class="w-3 h-3 mr-1" fill="currentColor" viewBox="0 0 20 20"><path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8 7a1 1 0 00-1 1v4a1 1 0 001 1h4a1 1 0 001-1V8a1 1 0 00-1-1H8z" clip-rule="evenodd"></path></svg>`
	default:
		bgClass = "bg-gray-100"
		textClass = "text-gray-700"
		icon = ""
	}
	return fmt.Sprintf(`<span class="inline-flex items-center px-2 py-1 rounded-full text-xs font-medium %s %s">%s%s</span>`,
		bgClass, textClass, icon, html.EscapeString(string(status)))
}
