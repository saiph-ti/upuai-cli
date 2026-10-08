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

### Install script (Linux / macOS — servers and CI)

```bash
curl -fsSL https://raw.githubusercontent.com/saiph-ti/upuai-cli/main/install.sh | sh
```

Downloads the release for your OS/arch, checks it against the release `checksums.txt` and installs to `/usr/local/bin` when writable, otherwise `~/.local/bin` (on GitHub Actions that directory is added to `GITHUB_PATH`). Variables go on the `sh` side of the pipe: `curl -fsSL …/install.sh | UPUAI_VERSION=0.21.1 UPUAI_INSTALL_DIR=$HOME/bin sh` pins a version and picks the destination.

### Direct download

Grab the archive for your OS/arch from [Releases](https://github.com/saiph-ti/upuai-cli/releases/latest) — assets are named `upuai_<version>_<os>_<arch>.tar.gz` (`.zip` on Windows) — verify it against `checksums.txt`, untar, and put `upuai` on your `$PATH`.

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

**Claude Code — the CLI installs the skill for you.** The first time you run any
`upuai` command in a linked project (including `upuai init` / `upuai link`), the
CLI writes `.claude/skills/upuai/SKILL.md`, so a fresh Claude Code session already
knows what Upuai is and how to deploy this project — no manual step, even in
projects created before this feature existed. Commit that file to share it with
your team. Manage it explicitly:

```bash
upuai skill install             # install/refresh in this project
upuai skill install --global    # install for every project on this machine (~/.claude)
upuai skill install --claude-md # also add a managed Upuai pointer block to CLAUDE.md
upuai skill status              # show installed vs. bundled version and state
```

The install is idempotent and self-healing: a file you hand-edited (or installed
via `npx skills add`) is never clobbered unless you pass `--force`. Opt out of the
auto-install with `UPUAI_SKIP_SKILL_INSTALL=1` or `installSkill: false` in
`~/.upuai/config.json`.

**Other agents** (Cursor, Codex CLI, Windsurf, and 55+ more) — install via
[vercel-labs/skills](https://github.com/vercel-labs/skills):

```bash
npx skills add saiph-ti/upuai-cli --skill upuai
```

Then ask the agent in natural language: *"deploy this to upuai"*. Full guide and a deep-link for `claude.ai` / `chatgpt.com` (no install): https://upuai.com.br/docs/ai-deploy.

## Commands

### Auth

| Command | Description |
|---------|-------------|
| `login` | Authenticate with Upuai Cloud (browser one-click or Email OTP) |
| `logout` | Log out and clear stored credentials |
| `whoami` | Show current authenticated user, workspace and project context |
| `token` | Manage scoped, revocable API tokens for CI/automation — `create`/`list`/`revoke`. The secret is shown once; use it non-interactively via `UPUAI_TOKEN` |

### Workspace

| Command | Alias | Description |
|---------|-------|-------------|
| `workspace list` | `ws ls` | List every workspace you belong to (● marks the active one) |
| `workspace current` | `show` | Show the active workspace, its ID and your role |
| `workspace switch [ref]` | | Switch the active workspace by slug, name or ID (interactive picker without an argument) |

See [Workspaces](#workspaces) for how linked directories pin their workspace.

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
| `deploy` | | Deploy the linked project or a service (`--wait` blocks until a terminal status; `--image <ref>` sets the image of an image service first) |
| `up` | | Deploy current directory from local source — no git needed (v0.11.0+) |
| `redeploy` | | Redeploy the latest deployment: same commit, current config. Reuses the image of an earlier successful deploy of that commit when nothing that goes into the build changed; `--rebuild` builds it again (v0.28.0+) |
| `rollback` | | Rollback to a previous deployment |
| `promote` | | Promote deployment between environments |
| `down` | | Remove the latest deployment (stop service) |

### Service

| Command | Description |
|---------|-------------|
| `add` | Add a new service to the project (interactive wizard). `--type database` provisions a **managed** Postgres/Redis/MySQL/Mongo via template (connection vars injected automatically); use `--engine` to skip the picker. Add `--worker` (with `--repo`/`--image`) to create a **background worker** (no HTTP/domain — Sidekiq/Celery/BullMQ) |
| `ps` | List the service's processes (web, worker, clock, release) of a multi-process / Procfile service. Aliases: `processes`, `process` |
| `restart` | Restart the linked service. `--process <name>` restarts a single process of a multi-process service |
| `logs` | View service logs. `--process <name>` scopes runtime logs to one process of a multi-process service |
| `scale` | Scale the service to N replicas (`upuai scale 3`), or individual processes (`upuai scale web=2 worker=1`) |
| `run` | Run a command **locally** with service environment variables injected |
| `shell` | Open a **local** subshell with service environment variables injected |
| `ssh` | Open an interactive shell (or run a command) **inside the running container** — `upuai ssh -s api -- bin/rails console`. Auto-allocates a PTY when stdin/stdout are terminals; in a pipe/redirect it runs non-interactively with byte-exact stdout/stderr (`echo x \| upuai ssh -- cat`). Force with `-t/--tty`, disable with `-T/--no-tty`. `--process <name>` targets one process of a multi-process service. `-n/--no-stdin` never attaches stdin (parity with `ssh -n`; needs a command, never allocates a PTY) — use it from CI and agents whose runner leaves stdin open (`upuai ssh -n -- cmd`, or redirect `</dev/null`); a session left waiting on a silent stdin says so on stderr. A session that ends without an exit status is an error, never a silent exit 0. Generic/stack-agnostic; backed by a K8s exec |
| `config show` | Show the current source (image, or repository and branch) and build/deploy config of the linked service (builder, build/start commands, health check, root directory). `-o json` exposes the image at `.config.source.image`. Alias: `config get` |
| `config set` | Update build/deploy config. `--root-dir apps/api` sets the build **Root Directory** for a monorepo on an existing github/gitlab service (no recreate needed; `--root-dir .` builds from the repo root); also `--builder`, `--dockerfile-path`, `--docker-context` (Dockerfile build context relative to the root directory, e.g. `--root-dir apps/api --builder dockerfile --docker-context ../..` gives apps/api's Dockerfile the repository root; v0.28.0+), `--build-command`, `--start-command`, `--health-check` |
| `service delete <name>` | Delete **a single service** (and its deployments, volumes, bucket attachments, cluster workloads, domains) without touching the rest of the project. Teardown runs in the background; the service is restorable for 30 days (volumes are not). `-y` skips confirmation. Contrast with `upuai delete` (whole project) and `upuai down` (stop the deployment, keep the service) |

### Database

| Command | Description |
|---------|-------------|
| `db connect` | Open an interactive `psql` (PostgreSQL) or `mysql` (MySQL) session against the linked database |
| `db connect --print` | Print the public connection string (script-friendly); MySQL also prints host, port, user and database |
| `db backup --out <file>` | PostgreSQL: `pg_dump` (custom format) via the public endpoint. MySQL: `mysqldump`; `--out` defaults to `<service>-<UTC timestamp>.sql` |
| `db restore <file>` / `-f <file>` | PostgreSQL: `pg_restore`. MySQL: streams the `.sql` file into `mysql`. A dump of the other engine is refused |
| `db public` / `db public status` | Show the public endpoint and which origins may connect (MySQL: also user, database and TLS state) |
| `db public enable --allow <ip\|cidr>` | Publish restricted to those origins (repeatable; replaces the list) |
| `db public enable --any` | Publish open to any IP |
| `db public disable` | Remove the public endpoint and its allowlist (a MySQL keeps its port for the next enable) |
| `db extensions` | List the managed Postgres extensions (PostGIS, pgvector, pg_trgm, ...) and their state: `enabled`, `available` or `unavailable` (needs `db update`). `-o json` for scripts |
| `db extensions enable <name>` | `CREATE EXTENSION ... CASCADE` in the `app` database — instant, no restart (e.g. `postgis`) |
| `db extensions disable <name>` | `DROP EXTENSION ... RESTRICT` — refused while anything depends on it (lists the dependents); never cascades. `-y` skips confirmation |
| `db extensions update <name>` | Update an installed extension to the version shipped by the image |
| `db version` | Show the database version and whether a maintenance update is pending (Postgres reads the running image; other engines report the stored version and never have a pending update) |
| `db update` | Apply the pending maintenance update (same major; security patches, base OS, PostGIS). Restarts the database (~1–2 min); `--wait` blocks until it finishes (`--wait-timeout`, default 900 s), `-y` skips confirmation |
| `db credentials repair` | Managed MySQL: check that the application account can log in and re-apply it if the database refuses it. Does not change the password. Lists the services to redeploy |
| `db credentials rotate` | Managed MySQL: generate a new password (the current one stops opening connections immediately). Owner/admin; `-y` skips confirmation. Lists the services to redeploy |

### Volumes

| Command | Description |
|---------|-------------|
| `volume list` | List the project's persistent disks and where they are mounted |
| `volume add --path <abs> --size <GB>` | Create a disk and mount it on the service (single replica from then on) |
| `volume remove <name\|id>` | Detach and delete the disk — the files are lost |

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
| `variables list` | `vars list` | List all environment variables. `--shared` lists the **environment-level** layer; `--project` the **project-level** (global) layer |
| `variables set KEY=VALUE...` | `vars set` | Set variables. `--scope both\|runtime\|build` controls injection phase. `--secret` marks them as secret (masked in every listing, never returned by the API); a variable that is already secret stays secret when you set a new value, and `--secret=false` unmarks it. `--shared` targets the **environment** layer (inherited by every service); `--project` the **project** layer (global to all environments). Default = this service. Precedence on deploy: service > environment > project |
| `variables delete KEY` | `vars delete` | Delete a variable (`--shared`/`--project` for the shared layers) |
| `variables shared list` | `vars shared list` | **Per service**: list which shared (project/environment) variables are injected (`Enabled`/`Origin`) |
| `variables shared enable KEY...` | `vars shared enable` | Inject shared variable(s) into this service. `--origin project\|environment` disambiguates a key defined in both layers |
| `variables shared disable KEY...` | `vars shared disable` | Stop injecting shared variable(s) into this service |
| `domain list` | `domains list` | List custom domains (with their redirect, if any) |
| `domain add <domain>` | `domains add` | Add a custom domain (its apex/www counterpart is added too). `--redirect-to <domain> --status 301\|302` to make it redirect |
| `domain update <domain\|id>` | `domains update` | Set (`--redirect-to <domain> [--status 301\|302]`) or remove (`--no-redirect`) the canonical-host redirect of a domain |
| `domain delete <domain\|id>` | `domains delete` | Delete a custom domain (and its apex/www counterpart) |

### Scheduler (cron)

| Command | Alias | Description |
|---------|-------|-------------|
| `scheduler list` | `cron list` | List scheduled jobs for the service |
| `scheduler create --name <n> --command <cmd> --schedule "<cron>"` | `cron create` | Create a cron job (runs with the service's deployed image). `--timeout <secs>` optional |
| `scheduler create --name <n> --command <cmd> --once` | `cron create` | Run a command **once, now**, in a fresh container of the deployed image. A run is killed after `--timeout` seconds (default 300, max 1800). The job has no schedule and never runs on its own; it stays listed (`on demand`) to run again or delete |
| `scheduler run <name\|id>` | `cron run` | Trigger a one-off run now |
| `scheduler pause <name\|id>` / `resume` | `cron pause`/`resume` | Pause / resume the schedule (jobs with a schedule only) |
| `scheduler delete <name\|id>` | `cron delete` | Delete a scheduled job |

Every service-scoped command accepts `-s/--service <name|slug|id>` to target a service other than the linked one (paridade com `railway variable list -s Postgres`): `variables`, `scheduler`, `ps`, `logs`, `run`, `shell`, `ssh`, `config`, `db`, plus the ones that change state — `deploy`, `up`, `redeploy`, `rollback`, `restart`, `scale`, `down`, `domain`.

Pair it with `-p` to act on another project entirely: `upuai redeploy -p api-prod -s web`. Without `-s`, a `-p` naming a project other than the linked directory's is **refused** rather than silently applied to the directory's service — see [Workspaces](#workspaces).

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

## Workspaces

A workspace is the top of the hierarchy — `workspace → project → environment → service`. You may belong to several (your personal one, plus each organization that invited you), but a session is scoped to **one at a time**.

That scoping is invisible until it bites: anything outside the active workspace answers **404, indistinguishable from "does not exist"**. An empty `upuai ls` usually means "wrong workspace", not "no projects".

```bash
upuai workspace list            # ● marks the active one
upuai workspace switch tai      # by slug, name or ID
upuai workspace switch          # interactive picker
upuai workspace current -o json # {"workspaceId","workspaceName","role"}; with UPUAI_TOKEN: {"workspaceId","workspaceName","workspaceSlug","machineToken"}
```

**Linked directories remember their workspace.** `upuai init` and `upuai link` record it in `.upuai/config.json`, and any command that operates on the linked project or service realigns the session before talking to the API — so `cd`-ing into a project of another workspace just works:

```
$ cd ~/code/projeto-da-tai && upuai up
→ workspace: TAI Tecnologia (was Gabriel Braga)
✓ Deployed
```

The `→` line goes to **stderr**, so `upuai status -o json | jq` stays clean.

A directory linked before this feature existed has no workspace recorded; the first command fills it in automatically, and the notice then reads just `→ workspace: TAI Tecnologia`.

**`-p` names the target, and the directory then steps aside.** The realignment above follows the *target* of the command, not the directory: with `-p` naming another project, the directory's pin has no authority over the session, and neither does its `environmentId`/`serviceId`. That is what makes the advice below possible to follow — while the pin was applied regardless, `switch` and the next command cancelled each other out forever.

```
$ upuai logs -p <id-de-outro-workspace> -s api
✗ project "backend" belongs to workspace "TAI Tecnologia", not the active one
  — run 'upuai workspace switch taitecnologia' first
$ upuai workspace switch taitecnologia
✓ Switched to workspace TAI Tecnologia
$ upuai logs -p <id> -s api        # funciona; a troca sobrevive ao comando
```

`upuai link <project-id>` is the one exception that *does* switch on its own: if the ID belongs to another workspace you are a member of, the CLI moves and links — that switch *is* what linking means. `-p` never moves your session, because a per-command flag should not outlive the command.

A command that needs a service but got `-p` for another project is refused, not guessed:

```
$ upuai down -p api-prod
✗ -p "api-prod" targets a different project than this directory ("upuai")
  — pass -s <service> to pick a service inside it, or run from that project's directory
```

Switching rotates your session tokens and the server pins the workspace to the refresh-token line, so it survives token rotation — you stay there until you switch again.

Machine tokens (`UPUAI_TOKEN`) are bound to the workspace they were created in: `workspace list` and `workspace switch` are refused with a token in the environment (memberships belong to the person, not the token). `upuai whoami` and `upuai workspace current` describe the token itself (name, scopes, workspace and the project it is restricted to), read from the API. To deploy to another workspace from CI, mint a token inside it.

## Authentication

Two authentication methods are supported:

```bash
# Browser one-click (default) — sign in with GitHub or any method the dashboard offers
upuai login

# Email OTP — sends a 6-digit code
upuai login --email
```

Credentials are stored in `~/.upuai/credentials.json` (file permissions `0600`). The login JWT is automatically refreshed on 401 responses. Interactive `upuai login` is the canonical flow **for humans** — same pattern as `railway login`, `vercel login`, `fly auth login`.

For **CI/automation**, mint a scoped, revocable token with `upuai token create` and set it in the `UPUAI_TOKEN` environment variable. It is an opaque, server-validated, long-lived credential (no 2h JWT TTL, no refresh) — the CLI uses it verbatim as the bearer, and a 401 means the token was revoked or expired (it never falls back to the interactive user).

```bash
# The secret is printed ONCE — store it immediately.
upuai token create --name playground-ci --scope deploy            # read + write
upuai token create --name readonly --scope read --expires 90      # read-only (no ssh), expires in 90 days
upuai token create --name proj-ci --scope deploy --project <id>   # narrowed to a single project
upuai token list
upuai token revoke <token-id>

export UPUAI_TOKEN=upua_...   # then any command runs non-interactively
upuai whoami                  # which token, workspace and project this is
```

Tokens can also be created, listed and revoked in the dashboard: **Settings → API tokens**.

### Update an image service from CI

Every deploy of an image service pulls the image again, so a mutable tag (`latest`) is picked up by a plain redeploy. To move a pinned tag, `deploy --image` sets it and deploys in one step. This GitHub Actions workflow checks Docker Hub daily and deploys only when a newer release exists:

```yaml
name: Update mailpit
on:
  schedule:
    - cron: '0 6 * * *'
  workflow_dispatch:

jobs:
  update:
    runs-on: ubuntu-latest
    env:
      UPUAI_TOKEN: ${{ secrets.UPUAI_TOKEN }}   # upuai token create --scope deploy --project <id>
      UPUAI_DISABLE_UPDATE_CHECK: '1'
      IMAGE: axllent/mailpit
    steps:
      - name: Install the Upuai CLI
        run: curl -fsSL https://raw.githubusercontent.com/saiph-ti/upuai-cli/main/install.sh | sh

      - name: Latest release tag on Docker Hub
        id: hub
        shell: bash   # -eo pipefail: a failed curl or an empty match stops the job
        run: |
          tag=$(curl -fsSL "https://hub.docker.com/v2/namespaces/axllent/repositories/mailpit/tags?page_size=100&ordering=last_updated" \
            | jq -r '.results[].name' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -1)
          echo "image=$IMAGE:$tag" >> "$GITHUB_OUTPUT"

      - name: Image running on Upuai
        id: current
        shell: bash
        run: |
          current=$(upuai config show -p my-project -s mailpit -e production -o json | jq -er '.config.source.image')
          echo "image=$current" >> "$GITHUB_OUTPUT"

      - name: Deploy the new tag
        if: steps.hub.outputs.image != steps.current.outputs.image
        run: upuai deploy -p my-project -s mailpit -e production --image "${{ steps.hub.outputs.image }}" --wait --yes
```

Scopes: `read` (safe/GET requests only) or `deploy` (read + write). A token never carries owner-only authority (billing, member management). Only a tenant Owner/Admin can create or revoke tokens.

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
  "workspaceId": "ws-456",
  "workspaceName": "TAI Tecnologia",
  "environment": "staging",
  "framework": "Next.js"
}
```

`workspaceId` pins the directory to the workspace that owns the project — see [Workspaces](#workspaces). Configs written before this field existed keep working and are filled in on the next command.

### Environment variables

All settings can be overridden with `UPUAI_` prefix:

| Variable | Description |
|----------|-------------|
| `UPUAI_API_URL` | API base URL (overrides config) |
| `UPUAI_WEB_URL` | Web dashboard URL (overrides config) |
| `UPUAI_TOKEN` | Scoped machine token from `upuai token create`, for CI/automation. Takes precedence over the stored login. Bound to the workspace it was minted in — `workspace list` and `switch` are refused while it is set; `whoami` and `workspace current` describe the token |
| `UPUAI_DISABLE_UPDATE_CHECK` | Set to `1` to suppress the periodic "new version available" nudge (useful in CI/agent contexts) |
| `UPUAI_SKIP_SKILL_INSTALL` | Set to `1` to disable auto-installing the Upuai agent skill into linked projects (see [Use with AI agents](#use-with-ai-agents)) |

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
- `--root-dir` — subdirectory within the repo for monorepos, or `.` to build from the repo root. A push redeploys the service when it changes a file inside that directory or outside every sibling service's directory (shared packages, lockfile).
- `--image` — Docker image reference. Creates a `docker_image`-type service.

Without `--repo` / `--image`, the service is type `empty` and cannot be deployed until you attach a source via `upuai add --type github ...` or the dashboard.

### deploy

```bash
upuai deploy                          # Deploy to default environment (fire-and-forget)
upuai deploy -e production            # Deploy to production
upuai deploy --wait                   # Block until terminal status (success/failed/...)
upuai deploy --wait --wait-timeout 600 # Stop waiting after 10 minutes (default: no limit)
upuai deploy --wait -o json           # JSON-printed final Deployment object
upuai deploy --watch                  # Watch for file changes and auto-redeploy
upuai deploy -s mailpit --image axllent/mailpit:v1.25.0 --wait --yes  # Set an image service's image, then deploy
```

`--wait` polls every 3 s until the deployment reaches a terminal status — there is no client-side timeout unless you pass `--wait-timeout <seconds>` (the platform ends every deployment on its own). Exit code is non-zero on `failed` / `cancelled` / `build_failed`, and when `--wait-timeout` is hit (the deployment itself keeps running).

`--image` only applies to image services (a git service would be converted, so it is refused) and needs a service (`-s` or the linked one). The image is set in the same environment the deploy targets (`-e`, else the linked one, else the default), and an empty tag or digest is rejected before anything is written.

### up

```bash
upuai up                              # Deploy the current directory from local source
upuai up -e production                # Deploy local source to production
upuai up --wait                       # Block until terminal status (success/failed/...)
upuai up --wait --wait-timeout 600    # Stop waiting after 10 minutes (default: no limit)
```

`upuai up` packages the local working directory into a tarball, uploads it to platform
storage, and triggers a deploy from that source — **no connected git repo required**. It
honors `.gitignore` / `.upuaiignore` and excludes `.git`, `node_modules`, and `.env*`. A
local `upuai.toml` is read just like the git path, so release-phase / migrations apply.
Available since **v0.11.0**.

**The recommended path is git**: connect a repository (GitHub/GitLab) and use `upuai deploy`
(or `git push` for auto-deploy). Git is the source of truth — reproducible, commit-pinned
deploys with history. Use `upuai up` as an **escape hatch**: a quick deploy of local code,
uncommitted changes, scratch dirs, or when there genuinely is no git repo.

`deploy` = deploy from a **connected git** repo (canonical) · `up` = deploy from **local
source** (escape hatch). These are separate commands; `up` is no longer an alias of `deploy`.

### redeploy

```bash
upuai redeploy            # Same commit, current config — reuses the image when build inputs are unchanged
upuai redeploy --rebuild  # Same commit, built again (v0.28.0+)
```

A redeploy reuses the image of an earlier successful deploy of the same commit when nothing that goes into the build changed: build settings (builder, build and start commands, Dockerfile path and context) and build-time variables (scope `both` or `build`). The release command runs again; there is no build. Multi-process (Procfile) services and `upuai up` deploys always build. Set variables the build never reads with `upuai vars set KEY=VAL --scope runtime` so changing them redeploys in about a minute.

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

### ps

```bash
upuai ps                    # List the linked service's processes
upuai ps -s api             # Processes of the "api" service
upuai ps -o json            # JSON output
```

Multi-process services run several process types (web + worker + clock + release)
from a single repo and build — Procfile / Heroku / Railway parity. The table
shows `NAME`, `TYPE`, `REPLICAS`, and `COMMAND`. Scale individual processes with
`upuai scale <name>=<N>` (see below).

### logs

```bash
upuai logs                  # View service logs (default lines)
upuai logs -n 100           # View last 100 lines
upuai logs --lines 50       # Same as -n
upuai logs -f               # Stream runtime logs (live tail)
upuai logs --process worker # Runtime logs of the "worker" process only
```

`--process` scopes **runtime** logs to one process of a multi-process service; it
is ignored for `--build`/`--deploy`/`--timeline`.

### restart

```bash
upuai restart                  # Restart the linked service (web)
upuai restart --process worker # Restart only the "worker" process
```

### scale

```bash
upuai scale 3                  # Scale the service to 3 replicas
upuai scale web=2 worker=1     # Scale individual processes of a multi-process service
```

A bare integer scales the whole (single-process) service. One or more
`<process>=<count>` pairs scale individual processes (see `upuai ps`).

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
upuai db connect                      # Interactive psql (PostgreSQL) / mysql (MySQL) session
upuai db connect --print              # Print connection string and exit
upuai db connect --output json        # Emit access info as JSON
upuai db connect --enable             # Auto-enable public access if disabled
upuai db backup --out file.dump       # PostgreSQL: pg_dump → file.dump
upuai db backup                       # MySQL: mysqldump → <service>-<UTC timestamp>.sql
upuai db restore -f file.dump         # PostgreSQL: pg_restore from file.dump
upuai db restore backup.sql -y        # MySQL: mysql < backup.sql, skip confirmation
upuai db public status                # Public endpoint, allowed origins (MySQL: user, database, TLS)
upuai db extensions                   # Managed Postgres extensions and their state
upuai db extensions enable postgis    # CREATE EXTENSION postgis (instant, no restart)
upuai db extensions disable postgis   # DROP EXTENSION ... RESTRICT (asks confirmation)
upuai db extensions update postgis    # Update to the version shipped by the image
upuai db version                      # Running version + pending update
upuai db update --yes --wait          # Apply the maintenance update and wait
upuai db credentials repair           # Managed MySQL: check the login, re-apply the account if refused
upuai db credentials rotate --yes     # Managed MySQL: new password, then redeploy the listed services
```

`db connect` requires `psql` on `$PATH`; `db backup` / `db restore` require `pg_dump` / `pg_restore` (postgresql-client / libpq). Public access is auto-prompted when disabled — confirm or pass `--enable`.

**MySQL public access.** Each managed MySQL is published on its own port of `<slug>.db.upuai.cloud` (23306–23505, stable across disable/enable); TLS is terminated by the database with the Let's Encrypt wildcard and the client verifies the server identity (`--ssl-mode=VERIFY_IDENTITY`; the connection string carries `?ssl-mode=VERIFY_IDENTITY`). The commands need the MySQL client on `$PATH` — macOS `brew install mysql-client` (keg-only: add `$(brew --prefix mysql-client)/bin` to `PATH`), Debian/Ubuntu `sudo apt install default-mysql-client` (or `mysql-client`), Windows the MySQL Installer. MariaDB's client (the `mysql` of Debian/Ubuntu) is detected and gets `--ssl --ssl-verify-server-cert` instead.
- The password never goes on the command line or in the environment: it is written to a `0600` option file in a private temporary directory, passed as `--defaults-file` (first argument, so `~/.my.cnf` cannot override it) and removed when the client exits — also on errors and signals.
- CA bundle for `--ssl-ca`: `SSL_CERT_FILE` if set and readable, else the system bundle (`/etc/ssl/cert.pem`, `/etc/ssl/certs/ca-certificates.crt`, `/etc/pki/tls/certs/ca-bundle.crt`, `/etc/ssl/ca-bundle.pem`), else the Let's Encrypt roots embedded in the CLI (ISRG Root X1, X2, YE, YR — this is what Windows uses).
- A MySQL created before public access existed restarts **once** (about 1 minute) the first time it is enabled, to load the certificate. `db public enable` / `db connect` ask for confirmation (`--yes` in scripts) and wait up to 3 minutes for it; on timeout they exit non-zero — check `upuai db public status` and retry. Right after enabling, the edge takes a couple of seconds to route the new port; `db connect|backup|restore` wait for the server greeting before starting the client.
- `db backup` runs `mysqldump --single-transaction --routines --events --triggers --set-gtid-purged=OFF --no-tablespaces` (MariaDB's mysqldump has no `--set-gtid-purged`; it is skipped), so the dump restores into any MySQL server; a failed dump leaves no partial file. `db restore` streams the file into `mysql` (a MariaDB dump's sandbox first line is dropped for Oracle's client).
- Reading the public endpoint returns the credentials, so an API token needs the **deploy** scope (`upuai token create --scope deploy`); a read-scoped token gets a clear 403. Redis and MongoDB have no public access.

Extensions are managed in the `app` database (the one in `DATABASE_URL`) from a curated list; the platform's current Postgres image ships PostGIS, pgvector and 25+ others, so enabling one never restarts the database. A database still on an older image shows extensions such as PostGIS as `unavailable` — run `upuai db update` (one restart) first. Your app's migrations can also run `CREATE EXTENSION IF NOT EXISTS postgis;` (the `DATABASE_URL` user owns the `app` database) — the dashboard and `db extensions` show it either way. Extensions that need `shared_preload_libraries` (pg_cron, pg_stat_statements, pgaudit) are not offered; other databases remain available over SQL with the database's own credentials.

Managed MySQL: the platform creates the application account and keeps the credential in use; the database service variables (`MYSQL_USER`, `MYSQL_PASSWORD`, `DATABASE_URL`, ...) mirror it and are read-only. Wire your services with `DATABASE_URL=${{<db-name>.DATABASE_URL}}` so a password change reaches them on the next deploy. The account can create its own databases, users and routines; there is no root account. `db credentials repair` fixes an `Access denied`, `db credentials rotate` issues a new password.

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

# Per-service binding of shared variables (which shared vars this service gets)
upuai vars shared list -s api               # List shared vars + Enabled/Origin for this service
upuai vars shared enable DATABASE_URL REDIS_URL -s worker  # inject into this service
upuai vars shared disable DATABASE_URL -s site             # stop injecting
upuai vars shared enable PUBLIC_ID --origin project -s api # disambiguate layer
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
| `railway connect [svc]` (interactive psql / mysql) | `upuai db connect` |
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
| `railway ssh` (exec into container) | `upuai ssh` |
| `railway service` (list processes) | `upuai ps` |
| `railway scale` (per-process) | `upuai scale web=2 worker=1` |

Out of scope today: managed snapshot create/list/download/restore is dashboard-only on Railway and on Upuai (CNPG runs daily scheduled backups in the cluster; CLI exposure tracked separately).

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
