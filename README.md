# Upuai Cloud CLI

Command-line interface for deploying, managing, and monitoring applications on [Upuai Cloud](https://upuai.cloud).

## Installation

### Homebrew (macOS / Linux)

```bash
brew tap saiph-ti/upuai-cli
brew install upuai
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
# 1. Authenticate
upuai login

# 2. Initialize project (auto-detects framework)
upuai init

# 3. Deploy
upuai deploy

# 4. Check status
upuai status

# 5. View logs
upuai logs

# 6. Set environment variables
upuai vars set DATABASE_URL=postgres://... SECRET_KEY=abc123

# 7. Add a custom domain
upuai domains add myapp.example.com

# 8. Scale your service
upuai scale 3
```

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
| `deploy` | `up` | Deploy the current project |
| `redeploy` | | Redeploy the latest deployment |
| `rollback` | | Rollback to a previous deployment |
| `promote` | | Promote deployment between environments |
| `down` | | Remove the latest deployment (stop service) |

### Service

| Command | Description |
|---------|-------------|
| `add` | Add a new service to the project (interactive wizard) |
| `restart` | Restart the linked service |
| `logs` | View service logs |
| `scale` | Scale service to N replicas |
| `run` | Run a command with service environment variables injected |

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
| `variables set KEY=VALUE...` | `vars set` | Set one or more environment variables |
| `variables delete KEY` | `vars delete` | Delete an environment variable |
| `domain list` | `domains list` | List custom domains |
| `domain add <domain>` | `domains add` | Add a custom domain |
| `domain delete <domain-id>` | `domains delete` | Delete a custom domain |

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

Credentials are stored in `~/.upuai/credentials.json` (file permissions `0600`). Tokens are automatically refreshed on 401 responses.

You can also authenticate via environment variable:

```bash
export UPUAI_TOKEN=<your-token>
```

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
| `UPUAI_API_URL` | API base URL |
| `UPUAI_TOKEN` | Authentication token (skips credential store) |

## Command Details

### deploy

```bash
upuai deploy              # Deploy to default environment
upuai deploy -e production # Deploy to production
upuai deploy --watch      # Watch for file changes and auto-redeploy
```

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
```

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
upuai run -- npm start      # Run command with service env vars injected
upuai run -- python app.py  # Works with any command
```

### environment

```bash
upuai env list              # List all environments
upuai env switch staging    # Switch to staging
upuai env new preview       # Create new environment
upuai env delete preview    # Delete environment (with confirmation)
```

### variables

```bash
upuai vars list                             # List all env vars
upuai vars set KEY=value                    # Set a single variable
upuai vars set DB_URL=postgres://... PORT=8080  # Set multiple at once
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
