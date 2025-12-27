# 🚢 Schooner

> Self-hosted continuous deployment for Docker-based homelabs

[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?style=flat&logo=go)](https://go.dev)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?style=flat&logo=docker)](https://docker.com)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

## ✨ What is Schooner?

Schooner is a lightweight, self-hosted continuous deployment tool designed for homelabs. It automatically builds and deploys your Docker containers when you push to GitHub. Think of it as your personal mini-Heroku! 🏠

### 🎯 Key Features

- 🔄 **Auto-deploy on push** - GitHub webhooks trigger automatic builds
- 🐳 **Multiple build strategies** - Dockerfile, Docker Compose, or Buildpacks
- 🔐 **GitHub OAuth** - Secure login with your GitHub account
- 📊 **Real-time logs** - Watch your builds live with SSE streaming
- 🌐 **Cloudflare Tunnel support** - Built-in tunnel management (optional)
- 📱 **Clean web UI** - Modern, responsive dashboard
- 🗄️ **SQLite database** - No external dependencies
- 🔔 **Webhook management** - Auto-creates GitHub webhooks on import

## 📸 Screenshots

```
┌─────────────────────────────────────────┐
│  🚢 Schooner          Dashboard  Settings│
├─────────────────────────────────────────┤
│                                         │
│  📦 my-app          ● Running    Deploy │
│  📦 api-service     ● Running    Deploy │
│  📦 blog            ○ Stopped    Deploy │
│                                         │
└─────────────────────────────────────────┘
```

## 🚀 Quick Start

### Prerequisites

- 🐳 Docker & Docker Compose
- 🔑 GitHub account (for OAuth)
- 🌐 Domain with Cloudflare (optional, for public access)

### 1️⃣ Clone the repository

```bash
git clone https://github.com/bas-slats/schooner.git
cd schooner
```

### 2️⃣ Create your config

```bash
cp config/config.example.yaml config.yaml
```

Edit `config.yaml`:

```yaml
server:
  host: "0.0.0.0"
  port: 8080
  base_url: "https://your-domain.com"  # 👈 Your public URL
  secret_key: "generate-a-random-string-here"

database:
  path: "/data/homelab-cd.db"

git:
  work_dir: "/data/repos"

github_oauth:
  client_id: "your-github-oauth-client-id"      # 👈 From GitHub
  client_secret: "your-github-oauth-secret"     # 👈 From GitHub
```

### 3️⃣ Create a GitHub OAuth App

1. Go to **GitHub → Settings → Developer settings → OAuth Apps**
2. Click **New OAuth App**
3. Fill in:
   - **Application name:** `Schooner`
   - **Homepage URL:** `https://your-domain.com`
   - **Callback URL:** `https://your-domain.com/oauth/github/callback`
4. Copy the **Client ID** and **Client Secret** to your config

### 4️⃣ Run with Docker Compose

```bash
docker compose up -d
```

### 5️⃣ Access the UI

Open `http://localhost:7123` (or your configured domain) and login with GitHub! 🎉

## 🏗️ Architecture

```
┌──────────────────────────────────────────────────────────┐
│                        Schooner                          │
├──────────────┬──────────────┬──────────────┬────────────┤
│   📡 API     │   🎨 UI      │   🔨 Build   │  🗄️ DB    │
│   (Chi)      │   (HTMX)     │   Workers    │  (SQLite)  │
└──────┬───────┴──────────────┴──────┬───────┴────────────┘
       │                              │
       ▼                              ▼
┌──────────────┐              ┌──────────────┐
│   GitHub     │              │   Docker     │
│   Webhooks   │              │   Engine     │
└──────────────┘              └──────────────┘
```

### 📁 Project Structure

```
schooner/
├── 📂 cmd/schooner/        # 🚀 Entry point
├── 📂 internal/
│   ├── 📂 api/             # 🌐 HTTP handlers & routes
│   ├── 📂 build/           # 🔨 Build orchestration
│   │   └── 📂 strategies/  # 📋 Dockerfile, Compose, Buildpacks
│   ├── 📂 cloudflare/      # ☁️ Tunnel management
│   ├── 📂 config/          # ⚙️ Configuration
│   ├── 📂 database/        # 🗄️ SQLite & queries
│   ├── 📂 docker/          # 🐳 Docker client
│   ├── 📂 git/             # 📦 Git operations
│   ├── 📂 github/          # 🐙 GitHub API
│   └── 📂 models/          # 📊 Data models
├── 📂 ui/static/           # 🎨 Frontend assets
├── 📂 migrations/          # 🗃️ DB schema
├── 📄 Dockerfile           # 🐳 Container build
├── 📄 docker-compose.yaml  # 🚢 Orchestration
└── 📄 config.yaml          # ⚙️ Your config (not in git)
```

## 🛠️ Development

### Prerequisites

- 🐹 Go 1.24+
- 🐳 Docker
- 📦 Make (optional)

### Build from source

```bash
# 📥 Install dependencies
go mod download

# 🔨 Build binary
make build

# 🧪 Run tests
make test

# 🚀 Run locally
make run
```

### 🔥 Hot reload development

```bash
# Install air (hot reload tool)
go install github.com/air-verse/air@latest

# Run with hot reload
make dev
```

## 📚 Build Strategies

### 🐳 Dockerfile (default)

Builds using a standard Dockerfile in your repo.

```yaml
build_strategy: dockerfile
dockerfile_path: Dockerfile
```

### 📦 Docker Compose

Runs `docker compose up` for multi-container apps.

```yaml
build_strategy: compose
compose_file: docker-compose.yml
```

### ☁️ Buildpacks

Uses Cloud Native Buildpacks (no Dockerfile needed).

```yaml
build_strategy: buildpacks
```

## 🔧 Configuration Reference

| Setting | Description | Default |
|---------|-------------|---------|
| `server.port` | HTTP port | `8080` |
| `server.base_url` | Public URL for webhooks | `http://localhost:8080` |
| `database.path` | SQLite database path | `/data/homelab-cd.db` |
| `git.work_dir` | Cloned repos directory | `/data/repos` |
| `docker.cleanup_enabled` | Auto-cleanup old images | `true` |
| `docker.keep_image_count` | Images to keep per app | `5` |

## 🌐 Cloudflare Tunnel (Optional)

Schooner can manage a Cloudflare Tunnel to expose your apps publicly:

```yaml
cloudflare:
  tunnel_token: "your-tunnel-token"
  tunnel_id: "your-tunnel-id"
  domain: "yourdomain.com"
```

## 🤝 Contributing

Contributions are welcome! 🎉

1. 🍴 Fork the repo
2. 🌿 Create a feature branch (`git checkout -b feature/amazing`)
3. 💾 Commit your changes (`git commit -m 'Add amazing feature'`)
4. 📤 Push to the branch (`git push origin feature/amazing`)
5. 🔃 Open a Pull Request

## 📄 License

MIT License - feel free to use this for your homelab! 🏠

## 💖 Acknowledgments

- 🐹 Built with [Go](https://go.dev)
- 🌐 [Chi](https://go-chi.io) for routing
- ⚡ [HTMX](https://htmx.org) for interactivity
- 🐳 [Docker SDK](https://docs.docker.com/engine/api/sdk/) for container management
- 📦 [go-git](https://github.com/go-git/go-git) for Git operations

---

<p align="center">
  Made with ❤️ for homelabbers everywhere
  <br>
  🚢 Happy deploying!
</p>
