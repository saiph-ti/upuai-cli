# Upuai Cloud CLI

Command-line interface for deploying, managing, and monitoring applications on [Upuai Cloud](https://upuai.cloud).

## Installation

### Homebrew (macOS / Linux)

```bash
brew tap saiph-ti/upuai-cli
brew install upuai
upuai version
```

### Scoop (Windows)

```powershell
scoop bucket add upuai https://github.com/saiph-ti/scoop-upuai-cli
scoop install upuai
upuai version
```

### Direct download

Grab the right archive for your OS/arch from [Releases](https://github.com/saiph-ti/upuai-cli/releases/latest), untar, and put `upuai` on your `$PATH`.

```bash
# Linux x86_64 (replace <version> with the desired tag, e.g. v0.0.1)
curl -sSfL https://github.com/saiph-ti/upuai-cli/releases/download/<version>/upuai_<version_no_v>_linux_x86_64.tar.gz | tar -xz
sudo mv upuai /usr/local/bin/
```

### From source

```bash
git clone https://github.com/saiph-ti/upuai-cli.git
cd upuai-cli
make build
make install INSTALL_DIR=~/.local/bin
upuai version
```

## Quick Start

```bash
# 1. Authenticate (browser OAuth or email OTP — required once per machine)
upuai login

# 2. Create the project + a github-backed service in one shot, then deploy.
upuai init --name my-app --repo myorg/my-app --framework "Next.js" --yes
upuai deploy --wait --yes

# That's it — `--wait` blocks until the deployment hits a terminal status
# and exits non-zero on failure. Skip --wait for the legacy fire-and-forget
# behaviour.

# Day-2 ops:
upuai status                  # project + service state
upuai logs -n 100             # service logs
upuai vars set DATABASE_URL=postgres://... SECRET_KEY=abc123
upuai domains add myapp.example.com
upuai scale 3
upuai rollback --list         # rollback to a previous deploy
```

### Use with AI agents

Install the upuai skill into Claude Code, Cursor, Codex CLI, Windsurf, or any other agent supported by [vercel-labs/skills](https://github.com/vercel-labs/skills) (55+ agents):

```bash
npx skills add saiph-ti/upuai-cli --skill upuai
```

Then ask the agent in natural language: *"deploy this to upuai"*. Full guide and a deep-link for `claude.ai` / `chatgpt.com` (no install): https://upuai.com.br/docs/ai-deploy.

## Commands

### Auth

| Command | Description |
|---------|-------------|
| `login` | Authenticate with Upuai Cloud (GitHub OAuth or Email OTP) |
| `logout` | Log out and clear stored credentials |
| `whoami` | Show current authenticated user and project context |

### Project

| Command | Alias | Description |
|---------|-------|-------------|
| `init` | | Initialize a new project in the current directory |
| `link` | | Link current directory to an existing project |
| `unlink` | | Unlink current directory from the project |
| `list` | `ls` | List all projects |
| `open` | | Open the project in the browser |
| `delete` | | Delete the linked project |
| `status` | | Show project status and services |

### Deploy

| Command | Alias | Description |
|---------|-------|-------------|
| `deploy` | | Deploy from a connected git repo (github/gitlab) |
| `up` | | Deploy current directory from local source — no git needed (v0.11.0+) |
| `redeploy` | | Redeploy the latest deployment |
| `rollback` | | Rollback to a previous deployment |
| `promote` | | Promote deployment between environments |
| `down` | | Remove the latest deployment (stop service) |

### Service

| Command | Description |
|---------|-------------|
| `add` | Add a new service to the project (interactive wizard). `--type database` provisions a **managed** Postgres/Redis/MySQL/Mongo via template (connection vars injected automatically); use `--engine` to skip the picker |
| `restart` | Restart the linked service |
| `logs` | View service logs |
| `scale` | Scale service to N replicas |
| `run` | Run a command **locally** with service environment variables injected |
| `shell` | Open a **local** subshell with service environment variables injected |
| `ssh` | Open an interactive shell (or run a command) **inside the running container** — `upuai ssh -s api -- bin/rails console`. Generic/stack-agnostic; backed by a K8s PTY exec |

### Database

| Command | Description |
|---------|-------------|
| `db connect` | Open an interactive `psql` session against the linked database |
| `db connect --print` | Print the public connection string (script-friendly) |
| `db backup --out <file>` | `pg_dump` the database via the public endpoint |
| `db restore -f <file>` | `pg_restore` a dump file via the public endpoint |

### Environment

| Command | Alias | Description |
|---------|-------|-------------|
| `environment list` | `env list` | List all environments |
| `environment switch <name>` | `env switch` | Switch to a different environment |
| `environment new <name>` | `env new` | Create a new environment |
| `environment delete <name>` | `env delete` | Delete an environment |

### Configuration

| Command | Alias | Description |
|---------|-------|-------------|
| `variables list` | `vars list` | List all environment variables |
| `variables set KEY=VALUE...` | `vars set` | Set one or more environment variables. `--scope both\|runtime\|build` controls injection phase (default `both`; `runtime` = not exposed during build; `build` = not in the running container) |
| `variables delete KEY` | `vars delete` | Delete an environment variable |
| `domain list` | `domains list` | List custom domains |
| `domain add <domain>` | `domains add` | Add a custom domain |
| `domain delete <domain-id>` | `domains delete` | Delete a custom domain |

`variables`, `run`, `shell`, and `ssh` accept `-s/--service <name|slug|id>` to target a service other than the linked one (paridade com `railway variable list -s Postgres`).

> **`run`/`shell` vs `ssh`**: `run`/`shell` execute **locally** with the service's env vars injected (like `railway run`). `ssh` opens a session **inside the running production container** (like `railway ssh` / `fly ssh console`) — use it for `rails console`, `manage.py shell`, one-off maintenance, or debugging in the live pod.

### Utility

| Command | Description |
|---------|-------------|
| `version` | Show CLI version, commit, and build date |
| `completion` | Generate shell completion scripts (bash\|zsh\|fish\|powershell) |
| `upgrade` | Upgrade the CLI to the latest version |

## Global Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--project` | `-p` | Project ID or name |
| `--environment` | `-e` | Environment (`production`, `staging`, `development`) |
| `--output` | `-o` | Output format (`table`, `json`, `text`) |
| `--yes` | `-y` | Skip confirmation prompts |
| `--verbose` | `-v` | Enable verbose output |

## Authentication

Two authentication methods are supported:

```bash
# GitHub OAuth (default) — opens browser
upuai login

# Email OTP — sends a 6-digit code
upuai login --email
```

Credentials are stored in `~/.upuai/credentials.json` (file permissions `0600`). Tokens are automatically refreshed on 401 responses. Interactive `upuai login` is the only supported auth flow — same pattern as `railway login`, `vercel login`, `fly auth login`.

## Configuration

### Global config (`~/.upuai/config.json`)

```json
{
  "apiUrl": "https://api.upuai.com.br",
  "defaultEnvironment": "staging",
  "output": "table"
}
```

### Project config (`.upuai/config.json`)

Created by `upuai init` or `upuai link` in the project root. Automatically added to `.gitignore`.

```json
{
  "projectId": "abc-123",
  "projectName": "my-app",
  "environment": "staging",
  "framework": "Next.js"
}
```

### Environment variables

All settings can be overridden with `UPUAI_` prefix:

| Variable | Description |
|----------|-------------|
| `UPUAI_API_URL` | API base URL (overrides config) |
| `UPUAI_WEB_URL` | Web dashboard URL (overrides config) |
| `UPUAI_DISABLE_UPDATE_CHECK` | Set to `1` to suppress the periodic "new version available" nudge (useful in CI/agent contexts) |

## Command Details

### init

```bash
upuai init                                                          # interactive wizard
upuai init --name my-app --framework "Next.js" --yes                # CLI-only, empty service (attach source later)
upuai init --name my-app --repo myorg/my-app --yes                  # github service ready to deploy
upuai init --name api --repo myorg/monorepo --root-dir apps/api --branch main --yes
upuai init --name web --image nginx:1.27 --yes                      # docker_image service
```

Flag reference:
- `--name` — kebab-case project slug. Required when `--yes` is set.
- `--framework` — one of `Next.js`, `Vite`, `React`, `Node.js`, `Go`, `Django`, `Flask`, `Python`, `Rails`, `Docker`, `Static`. Required when `--yes` is set and the CLI cannot auto-detect.
- `--repo` — `owner/repo` short form or a full GitHub **or** GitLab URL (auto-detected and normalized). Creates a `github`- or `gitlab`-type service with source. If you also pass `--type`, it must be `github` or `gitlab` to match the URL. Mutually exclusive with `--image`.
- `--branch` — git branch (default `main`).
- `--root-dir` — subdirectory within the repo for monorepos.
- `--image` — Docker image reference. Creates a `docker_image`-type service.

Without `--repo` / `--image`, the service is type `empty` and cannot be deployed until you attach a source via `upuai add --type github ...` or the dashboard.

### deploy

```bash
upuai deploy                          # Deploy to default environment (fire-and-forget)
upuai deploy -e production            # Deploy to production
upuai deploy --wait                   # Block until terminal status (success/failed/...)
upuai deploy --wait --wait-timeout 600 # Wait up to 10 minutes (default 300 s)
upuai deploy --wait -o json           # JSON-printed final Deployment object
upuai deploy --watch                  # Watch for file changes and auto-redeploy
```

`--wait` polls every 3 s. Exit code is non-zero on `failed` / `cancelled` / `build_failed`.

### up

```bash
upuai up                              # Deploy the current directory from local source
upuai up -e production                # Deploy local source to production
upuai up --wait                       # Block until terminal status (success/failed/...)
upuai up --wait --wait-timeout 600    # Wait up to 10 minutes (default 300 s)
```

`upuai up` packages the local working directory into a tarball, uploads it to platform
storage, and triggers a deploy from that source — **no connected git repo required**
(same UX as `vercel`, `railway up`, `fly deploy`). It honors `.gitignore` / `.upuaiignore`
and excludes `.git`, `node_modules`, and `.env*`. A local `upuai.toml` is read just like
the git path, so release-phase / migrations apply. Available since **v0.11.0**.

`deploy` = deploy from a **connected git** repo · `up` = deploy from **local source**.
These are separate commands; `up` is no longer an alias of `deploy`.

### redeploy

```bash
upuai redeploy            # Redeploy the latest deployment
```

### promote

```bash
upuai promote                           # staging → production (default)
upuai promote --from development --to staging
```

### rollback

```bash
upuai rollback              # Rollback to previous deployment
upuai rollback --to <id>    # Rollback to specific deployment
upuai rollback --list       # List recent deployments
```

### down

```bash
upuai down                  # Remove the latest deployment
upuai down -y               # Skip confirmation
```

### link / unlink

```bash
upuai link                  # Interactive project selection
upuai link <project-id>    # Direct link by ID
upuai unlink                # Unlink current directory
```

### list

```bash
upuai list                  # List all projects
upuai ls                    # Alias
upuai ls -o json            # JSON output
```

### open

```bash
upuai open                  # Open project in browser
```

### delete

```bash
upuai delete                # Delete the linked project (with confirmation)
upuai delete -y             # Skip confirmation
```

### add

```bash
upuai add                   # Interactive wizard to add a service
upuai add --repo myorg/repo # GitHub or GitLab URL / owner/repo short form (auto-detected)
upuai add --image registry.example.com/app:1.0 \
  --registry-host registry.example.com \
  --registry-user ci --registry-password "$TOKEN"   # Private Docker image
```

Flag reference:
- `--repo` — `owner/repo` short form or a full GitHub **or** GitLab URL (auto-detected). With `--type`, must be `github` or `gitlab` to match.
- `--registry-host` / `--registry-user` / `--registry-password` — credentials for a private Docker registry (used with `--image`). `--registry-user` and `--registry-password` must be supplied together.

### logs

```bash
upuai logs                  # View service logs (default lines)
upuai logs -n 100           # View last 100 lines
upuai logs --lines 50       # Same as -n
```

### restart

```bash
upuai restart               # Restart the linked service
```

### scale

```bash
upuai scale 3               # Scale service to 3 replicas
```

### run

```bash
upuai run npm start             # Run command with service env vars injected
upuai run -- npm start          # Same; "--" is optional
upuai run -s api -- env         # Target a different service ad-hoc
upuai run -- python manage.py migrate
```

The `--` separator is optional. Use it when your command has flags that conflict with upuai's own (`-s`, `-p`, `-e`, `-o`, `-y`, `-v`).

### shell

```bash
upuai shell                  # Subshell with env vars from the linked service
upuai shell -s api           # Subshell scoped to the "api" service
upuai shell --shell /bin/zsh # Override shell (default: $SHELL or cmd.exe)
upuai shell --silent         # Suppress the spawn banner
```

Inside the subshell, run anything that reads env vars: `printenv DATABASE_URL`, `psql "$DATABASE_URL"`, `npm start`, etc. Type `exit` to return.

### db

```bash
upuai db connect                      # Interactive psql session
upuai db connect --print              # Print connection string and exit
upuai db connect --output json        # Emit access info as JSON
upuai db connect --enable             # Auto-enable public access if disabled
upuai db backup --out file.dump       # pg_dump → file.dump
upuai db restore -f file.dump         # pg_restore from file.dump
upuai db restore -f file.dump -y      # Skip confirmation
```

`db connect` requires `psql` on `$PATH`; `db backup` / `db restore` require `pg_dump` / `pg_restore` (postgresql-client / libpq). Public access is auto-prompted when disabled — confirm or pass `--enable`.

### environment

```bash
upuai env list              # List all environments
upuai env switch staging    # Switch to staging
upuai env new preview       # Create new environment
upuai env delete preview    # Delete environment (with confirmation)
```

### variables

```bash
upuai vars list                             # List all env vars (linked service)
upuai vars list -s Postgres                 # List vars from another service
upuai vars list --output json               # JSON output (script-friendly)
upuai vars set KEY=value                    # Set a single variable
upuai vars set DB_URL=postgres://... PORT=8080  # Set multiple at once
upuai vars set -s api API_KEY=xxx           # Set on a specific service
upuai vars delete SECRET_KEY                # Delete a variable
```

### domain

```bash
upuai domains list                # List custom domains
upuai domains add myapp.com       # Add a custom domain
upuai domains delete <domain-id>  # Delete a domain
```

### completion

```bash
upuai completion bash       # Generate bash completion
upuai completion zsh        # Generate zsh completion
upuai completion fish       # Generate fish completion

# Add to your shell profile:
source <(upuai completion bash)
```

### upgrade

```bash
upuai upgrade               # Upgrade CLI to latest version
```

## Coming from Railway?

Common workflows mapped to `upuai`:

| Railway | Upuai |
|---|---|
| `railway login` | `upuai login` |
| `railway link` | `upuai link` |
| `railway up` (local source) | `upuai up` |
| `railway deploy` (connected repo) | `upuai deploy` |
| `railway logs` | `upuai logs` |
| `railway add --database postgres` | `upuai add` (interactive wizard, type=database) |
| `railway connect [svc]` (interactive psql) | `upuai db connect` |
| `railway shell -s <svc>` | `upuai shell -s <svc>` |
| `railway run <cmd>` | `upuai run <cmd>` |
| `railway variable list -s <svc>` | `upuai variables list -s <svc>` |
| `railway variable set KEY=val` | `upuai variables set KEY=val` |
| `railway variable list -s <svc> --json` | `upuai vars list -s <svc> -o json` |
| `railway run pg_dump > x.sql` | `upuai db backup --out x.dump` (managed wrapper) |
| `railway run pg_restore < x.sql` | `upuai db restore -f x.dump` |
| `railway domain` | `upuai domain` |
| `railway environment` | `upuai environment` (alias `env`) |
| `railway redeploy` / `railway down` | `upuai redeploy` / `upuai down` |
| `railway rollback` | `upuai rollback` |

Out of scope today: `railway ssh` (exec into container) — needs orchestrator endpoint. Managed snapshot create/list/download/restore is dashboard-only on Railway and on Upuai (CNPG runs daily scheduled backups in the cluster; CLI exposure tracked separately).

## Detected Frameworks

The CLI auto-detects frameworks during `upuai init`:

| Framework | Detection files |
|-----------|----------------|
| Next.js | `next.config.js`, `next.config.mjs`, `next.config.ts` |
| Vite | `vite.config.ts`, `vite.config.js` |
| React (CRA) | `public/index.html` |
| Node.js (Express) | `package.json` |
| Go | `go.mod` |
| Python (Django) | `manage.py` |
| Python (Flask) | `app.py`, `wsgi.py` |
| Python | `requirements.txt`, `pyproject.toml` |
| Ruby on Rails | `Gemfile`, `config/routes.rb` |
| Docker | `Dockerfile` |
| Static | `index.html` |

## Development

### Makefile targets

| Target | Description |
|--------|-------------|
| `make build` | Build binary to `bin/upuai` |
| `make install` | Build and install (default: `/usr/local/bin`) |
| `make test` | Run tests with race detection |
| `make lint` | Run golangci-lint |
| `make fmt` | Format code (gofmt + goimports) |
| `make dev` | Build and run |
| `make clean` | Remove `bin/` directory |

### Stack

- Go 1.23
- [Cobra](https://github.com/spf13/cobra) — CLI framework
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) — TUI (spinner)
- [Huh](https://github.com/charmbracelet/huh) — Interactive forms (prompts, selects, confirms)
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) — Terminal styling
- [Viper](https://github.com/spf13/viper) — Configuration management
- [fsnotify](https://github.com/fsnotify/fsnotify) — File watching (deploy --watch)
