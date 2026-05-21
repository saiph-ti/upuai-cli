---
name: upuai
description: Deploy, manage, and troubleshoot projects on Upuai Cloud using the upuai CLI. Route-first skill — read the routing table below and follow the matching section.
version: 1.0.0
when-to-use: When the user wants to deploy a project to Upuai, check status/logs, configure env vars or domains, manage databases, roll back, promote between environments, or use the upuai CLI for any task.
homepage: https://upuai.com.br
---

# Upuai Cloud — deploy via CLI

Upuai is a PaaS (peer of Railway, Render, Fly.io) hosted in Brazil. This skill teaches you to drive its CLI `upuai` (Go binary, repo `saiph-ti/upuai-cli`, distributed via Homebrew / Scoop).

## Routing

Read only the section(s) that match the user's intent.

| User intent | Read |
|-------------|------|
| First-time setup or "deploy this project" | [Setup](#setup) → [Deploy](#deploy) |
| "what's broken / logs / status / rollback" | [Troubleshoot](#troubleshoot) |
| "set env var / add domain / change build" | [Configure](#configure) |
| "connect to db / backup / restore" | [Database](#database) |
| Promote staging → production | [Environments](#environments) |
| Anything else | Read `https://upuai.com.br/llms-full.txt` |

## Non-interactive contract

Always invoke `upuai` in non-interactive mode. Without these, prompts will hang in agent environments.

1. **Auth**: always run `upuai whoami` first. If it returns the expected user, you're authenticated — the CLI reads `~/.upuai/credentials.json` and auto-refreshes the JWT on 401. If `whoami` fails, ask the user to run `upuai login` once on their own machine (browser OAuth or email OTP — both interactive). **There is no headless auth path** — `upuai login` is the only supported auth flow, same pattern as `railway login`, `vercel login`, `fly auth login`. Don't invent env-var shortcuts.
2. **Skip confirmations**: pass `-y` / `--yes` on any command that mutates state (`init`, `deploy`, `down`, `delete`, `rollback`, `promote`, `db restore`, `vars delete`, `domain delete`).
3. **JSON output for parsing**: pass `-o json` on `status`, `logs`, `list`, `vars list`, `domain list`, `env list`, and (when waiting) `deploy --wait -o json`.
4. **Pre-supply flags on `init`**: when `--yes` is set, `init` requires `--name <slug>`. Pass `--framework <name>` to skip auto-detect prompts. Pass `--repo <owner>/<repo>` (or `--image <ref>`) to create a deployable service in one step instead of an empty placeholder. The CLI errors out with a clear message if a flag is missing rather than hanging on a prompt.
5. **Block until terminal**: prefer `upuai deploy --wait` over polling `upuai status` yourself — the CLI already handles the polling, status transitions, timeout, and non-zero exit on failure.

Before running any command that touches user state, confirm the action with the user. Read flags from the user — do not invent project names, custom domains, or env-var values.

## Setup

Run once per machine. Skip if `upuai version` is **>= 0.10.0** and `upuai whoami` returns the expected user. Older versions lack the `--repo` flag on `init` and the `--wait` flag on `deploy` that this skill depends on — instruct the user to run `upuai upgrade` (or `brew upgrade upuai` / `scoop update upuai`) before continuing.

```bash
# macOS / Linux
brew tap saiph-ti/upuai-cli
brew install upuai

# Windows (PowerShell)
scoop bucket add upuai https://github.com/saiph-ti/scoop-upuai-cli
scoop install upuai

# Verify
upuai version   # must be >= 0.10.0
```

Authenticate (only needed once per machine — the CLI persists credentials in `~/.upuai/credentials.json` and auto-refreshes them):

```bash
upuai whoami       # If this prints the user, you're already authenticated — skip the rest.
upuai login        # Otherwise: opens browser for GitHub OAuth (interactive — run on user's own machine).
upuai login --email # Alternative: email OTP (6-digit code via email).
```

Same pattern as `railway login` / `vercel login` / `fly auth login` — interactive only. After login, the CLI rotates tokens automatically; the user does not need to re-login until they `upuai logout` or move to a new machine.

## Deploy

### Mental model

A project on Upuai owns one or more **services**. Each service has a **type** that determines where its source lives:

| Type | Source | Created via |
|------|--------|-------------|
| `github` / `gitlab` | Repo pulled fresh on every deploy | `upuai init --repo owner/repo` or `upuai add --type github` |
| `docker_image` | Pre-built image from a registry | `upuai init --image nginx:1.27` or `upuai add --image …` |
| `docker` | Dockerfile-based, source uploaded | Dashboard / `upuai add --type docker` |
| `empty` | None — placeholder | `upuai init` without `--repo`/`--image` |
| `database`, `bucket`, `function` | Platform-managed | `upuai add --type database` etc. |

`upuai deploy` triggers a build + rollout for the **linked service**. An `empty` service cannot be deployed — attach a source first (re-init with `--repo`, or `upuai add --type github`, or use the dashboard).

### Happy path — single-service repo

This is two commands. Confirm with the user before running each — `--name`, `--repo`, and `--framework` should reflect the user's intent.

```bash
# 1. Create the project + a github-backed service in one shot.
upuai init \
  --name <project-slug> \
  --repo <owner>/<repo> \
  --branch main \
  --framework <name> \
  --yes

# 2. Trigger the deploy and block until it reaches a terminal status.
upuai deploy --wait --yes -o json
```

`--wait` polls every 3 seconds until status is `success` / `failed` / `cancelled` / `build_failed` / `superseded`. Default timeout: 300s — override with `--wait-timeout 600`. Exit code is non-zero on `failed` / `cancelled` / `build_failed`.

### Variants

- **Docker image** (no build, just pull + run):
  ```bash
  upuai init --name <slug> --image <registry/image:tag> --framework Docker --yes
  upuai deploy --wait --yes -o json
  ```
- **Monorepo** (build a subdirectory):
  ```bash
  upuai init --name <slug> --repo <owner>/<repo> --root-dir apps/api --framework <name> --yes
  ```
- **Existing project, fresh checkout** (just link + deploy):
  ```bash
  upuai link <project-id> --service <service-name> --env production
  upuai deploy --wait --yes -o json
  ```
- **No-CLI fallback** — if `upuai init --repo` fails with a GitHub auth error, the user has not authorized the Upuai GitHub App yet. Direct them to `https://app.upuai.com.br/projects/new` to install it; then re-run from this section.

### Flag reference (init)

- `--name <slug>` — kebab-case project slug. **Required when `--yes` is set.**
- `--repo <owner>/<repo>` — creates a `github`-type service. URLs (`https://github.com/owner/repo[.git]`, `git@github.com:owner/repo`) are normalized to `owner/repo`.
- `--branch <name>` — git branch (default `main`).
- `--root-dir <path>` — subdirectory within the repo for monorepos (e.g. `apps/api`).
- `--image <ref>` — creates a `docker_image`-type service; mutually exclusive with `--repo`.
- `--framework <name>` — one of `Next.js`, `Vite`, `React`, `Node.js`, `Go`, `Django`, `Flask`, `Python`, `Rails`, `Docker`, `Static`. **Required when `--yes` is set and the CLI cannot auto-detect.** When in doubt, ask the user — a repo with both `Dockerfile` and `next.config.js` could go either way.

### Reading the result

In JSON mode (`-o json`), the final `Deployment` object holds:

```json
{
  "id": "deploy_…",
  "serviceId": "svc_…",
  "environmentId": "env_…",
  "status": "success",
  "url": "https://my-app.upuai.com.br",
  "builder": "railpack",
  "startedAt": "…",
  "finishedAt": "…"
}
```

If `status === "success"` and `url` responds 200, report it to the user. Otherwise jump to [Troubleshoot](#troubleshoot).

> The exact JSON shape can shift across CLI versions. Run `upuai status -o json | jq .` once to confirm field names before scripting on top of them; do not hardcode the schema based on this example.

## Troubleshoot

Decision tree for "it's not working":

1. **Did `deploy` even trigger?** → `upuai status -o json | jq '.environments[].services[].lastDeployment'`. If everything is `null`, the project isn't linked or has no deployments yet — run `upuai link <project-id> --service <name> --env <env>` (the `--service` / `--env` flags skip the interactive picker).
2. **Status `failed` or `build_failed`?** → `upuai logs -n 100 --build` shows the build output; `upuai logs -n 100 --deploy` shows the release-phase + rollout log; `upuai logs -n 100` shows runtime logs. Common causes:
   - **Build failure** (`build_failed`): missing `buildCommand` for the framework, missing dependency, wrong Node/Python version. Suggest `upuai.toml` with explicit `[build]` block — see [Configure](#configure).
   - **Release phase fail** (`failed` during release): if `releaseCommand` is set (e.g. `prisma migrate deploy`), it runs before the rollout. Check `--deploy` logs for the release-phase output.
   - **Runtime crash** (deployment is `success` but service unhealthy / restarting): wrong `startCommand`, missing env var, port mismatch. Check `upuai vars list -o json` and confirm the app listens on `process.env.PORT` (Upuai injects `PORT`).
3. **Status `success` but URL doesn't respond 200?**
   - Service may bind to wrong port → ensure app reads `process.env.PORT`.
   - Health check path may be wrong → set `healthCheckPath` in `upuai.toml`.
   - Custom domain not propagated yet — try the `*.upuai.com.br` URL from the deployment object first.
4. **Recent change broke it?** → `upuai rollback --list` to see deployments, then `upuai rollback --to <id> --yes` to revert.
5. **Deeper inspection** → `upuai status -o json` exposes a full timeline (git clone / build / release / deploy stages) when the CLI is >= 0.4.0. Use it to pinpoint which stage failed before reaching for logs.

### Useful commands

```bash
upuai status -o json                # full state
upuai logs -n 200                   # last 200 log lines
upuai logs -n 50 -o json            # JSON-parseable
upuai redeploy --yes                # rerun last deploy (no code change)
upuai restart --yes                 # restart service (clears in-memory state)
upuai rollback --list               # list deployments for rollback
upuai rollback --to <deploy-id> --yes
```

For database investigation see [Database](#database).

## Configure

### upuai.toml — config as code

The canonical way to express build/release/deploy behaviour. Lives at the repo root. Cached server-side per SHA. Doc + schema: `https://upuai.com.br/docs/upuai-toml` and `https://upuai.com.br/schemas/upuai-toml-v1.json`.

Minimal example — only set keys the user needs:

```toml
#:schema https://upuai.com.br/schemas/upuai-toml-v1.json

[build]
builder = "railpack"            # default; or "dockerfile"
buildCommand = "pnpm build"
dockerfilePath = "Dockerfile"   # only when builder = "dockerfile"

[deploy]
startCommand = "node dist/server.js"
releaseCommand = "pnpm exec prisma migrate deploy"
releaseTimeoutSeconds = 300
healthCheckPath = "/health"
```

Precedence: dashboard UI values win over `upuai.toml`, which wins over Procfile `release:` (legacy). Don't set the same key in two places.

### Environment variables

```bash
upuai vars list -o json                              # current vars (linked service)
upuai vars set DATABASE_URL=postgres://... --yes     # set one
upuai vars set KEY1=val1 KEY2=val2 --yes             # set multiple atomically
upuai vars set -s api API_KEY=xxx --yes              # target a specific service
upuai vars delete SECRET_KEY --yes
```

Vars take effect on the **next deploy** — trigger `upuai redeploy --yes` if the user expects them live immediately. Never write secrets to the user's chat / repo / logs.

### Custom domains

```bash
upuai domains list -o json
upuai domains add myapp.com --yes        # adds + returns DNS records to set
upuai domains delete <domain-id> --yes
```

After `domains add`, instruct the user to set the DNS records (CNAME / A) returned by the API at their registrar. Propagation can take minutes to hours.

### Scaling

```bash
upuai scale 3 --yes        # set replica count to 3
```

## Database

The CLI provides managed wrappers around `psql` / `pg_dump` / `pg_restore` that talk to Upuai's public DB endpoint (`<svc>.db.upuai.cloud:5432?sslmode=require`) without exposing raw credentials.

```bash
upuai db connect --print               # print connection string (script-friendly)
upuai db connect --output json         # access info as JSON
upuai db connect --enable              # auto-enable public access if disabled
upuai db connect                       # interactive psql session (needs TTY — skip in agent flows)
upuai db backup --out backup.dump      # pg_dump
upuai db restore -f backup.dump --yes  # pg_restore
```

For automated tasks, use `--print` / `--output json` to fetch the connection string, then run queries via your own `psql` invocation. Do not run `upuai db connect` without `--print` inside an agent — it opens an interactive subshell.

## Environments

Upuai supports `production`, `staging`, `development` by default; custom names allowed.

```bash
upuai env list                              # list environments
upuai env switch staging                    # change linked env for the directory
upuai env new preview                       # create a new env
upuai deploy -e production --yes            # deploy to a specific env (overrides linked)

# Promote a deployment between envs (default: staging → production)
upuai promote --yes
upuai promote --from development --to staging --yes
```

`promote` copies the build artifact, not the source — fast and consistent. Use it instead of redeploying when you want bit-for-bit parity.

## Frameworks the CLI auto-detects

Pass one of these to `--framework`:

| Framework | Detection signal |
|-----------|------------------|
| Next.js | `next.config.{js,mjs,ts}` |
| Vite | `vite.config.{ts,js}` |
| React | `public/index.html` |
| Node.js | `package.json` |
| Go | `go.mod` |
| Django | `manage.py` |
| Flask | `app.py`, `wsgi.py` |
| Python | `requirements.txt`, `pyproject.toml` |
| Rails | `Gemfile` + `config/routes.rb` |
| Docker | `Dockerfile` |
| Static | `index.html` |

## Reference

- CLI README + full command list: `https://github.com/saiph-ti/upuai-cli`
- CLI doc page: `https://upuai.com.br/docs/upuai-cli`
- `upuai.toml` reference: `https://upuai.com.br/docs/upuai-toml`
- Long-form context for LLMs: `https://upuai.com.br/llms-full.txt`
- Dashboard: `https://app.upuai.com.br`
- Status page (if a deploy looks platform-wide broken): `https://upuai.com.br/status`

## Honest limitations

- This skill makes you knowledgeable about the CLI; it does not give you cluster access. The CLI talks to Upuai's API on the user's behalf.
- `upuai login` is interactive (browser OAuth or email OTP) — the only supported auth flow, same pattern as `railway login` / `vercel login` / `fly auth login`. On agent runners without a browser, ask the user to log in once on their own machine. Credentials persist in `~/.upuai/credentials.json` and refresh automatically; no headless / token-based auth path exists today.
- `upuai add` (without `--type`/`--name`/`--repo`/`--image`), `upuai link` (without `--service`/`--env`), `upuai shell` (subshell), and `upuai db connect` (without `--print`) all need a TTY. Use the non-interactive flag variants noted in the relevant sections above.
- Builds run on Upuai's cluster, not locally. Real-time progress isn't streamed by `upuai deploy --wait` itself; use `upuai logs --build` and `upuai logs --deploy` for live tail of those stages while a deploy is in flight.
