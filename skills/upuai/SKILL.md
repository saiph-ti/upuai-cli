---
name: upuai
description: Deploy, manage, and troubleshoot projects on Upuai Cloud using the upuai CLI. Route-first skill — read the routing table below and follow the matching section.
version: 1.1.0
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
| "project not found" / empty project list / user is in more than one org | [Workspaces](#workspaces) |
| Anything else | Read `https://upuai.com.br/llms-full.txt` |

## Non-interactive contract

Always invoke `upuai` in non-interactive mode. Without these, prompts will hang in agent environments.

1. **Auth**: always run `upuai whoami` first. If it returns the expected user, you're authenticated — the CLI reads `~/.upuai/credentials.json` and auto-refreshes the JWT on 401. If `whoami` fails, ask the user to run `upuai login` once on their own machine (browser OAuth or email OTP — both interactive), same pattern as `railway login`, `vercel login`, `fly auth login`. For **CI/automation**, the sanctioned headless path is a scoped token: a human runs `upuai token create --name <name> --scope deploy` (add `--project <id>` to scope it to one project, `--expires <days>` for a TTL), then sets the printed secret in `UPUAI_TOKEN` (opaque, long-lived, revocable). `--scope read` mints a GET-only token. List with `upuai token list`, revoke with `upuai token revoke <id>`. Never stuff a user JWT into an env var — use `upuai token`.
2. **Skip confirmations**: pass `-y` / `--yes` on any command that mutates state (`init`, `deploy`, `down`, `delete`, `rollback`, `promote`, `db restore`, `vars delete`, `domain delete`).
3. **JSON output for parsing**: pass `-o json` on `status`, `logs`, `list`, `vars list`, `domain list`, `env list`, and (when waiting) `deploy --wait -o json`.
4. **Pre-supply flags on `init`**: when `--yes` is set, `init` requires `--name <slug>`. Pass `--framework <name>` to skip auto-detect prompts. Pass `--repo <owner>/<repo>` (or `--image <ref>`) to create a deployable service in one step instead of an empty placeholder. The CLI errors out with a clear message if a flag is missing rather than hanging on a prompt.
5. **Block until terminal**: prefer `upuai deploy --wait` over polling `upuai status` yourself — the CLI already handles the polling, status transitions, timeout, and non-zero exit on failure.
6. **Know your workspace**: every session is scoped to ONE workspace, and the API returns a plain 404 for anything outside it. `upuai whoami -o json` reports `workspace` / `workspaceId` / `role` — **except under `UPUAI_TOKEN`**, where it reports `machineToken: true` and omits them, because a machine token is opaque and its workspace is not readable client-side (it is fixed at creation; mint the token inside the workspace you intend to deploy to). A linked directory pins its workspace and the CLI realigns the session before any command that touches the linked project or service, printing `→ workspace: <name>` to **stderr** (stdout stays clean for `-o json`). See [Workspaces](#workspaces).

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
| `database`, `bucket`, `function` | Platform-managed | `upuai add --type database` (engine picker / `--engine postgres\|redis\|mysql\|mongo`) provisions a **managed** instance and auto-injects connection vars (`DATABASE_URL`/`REDIS_URL`/…); `bucket`/`function` similar |

`upuai deploy` triggers a build + rollout for the **linked service**. An `empty` service cannot be deployed — attach a source first (re-init with `--repo`, or `upuai add --type github`, or use the dashboard).

Every service-scoped command takes `-s/--service <name|slug|id>` to target another service — `deploy`, `up`, `redeploy`, `rollback`, `restart`, `scale`, `down`, `domain`, `config`, `logs`, `ps`, `variables`, `scheduler`, `run`, `shell`, `ssh`, `db`. Combine with `-p` for another project: `upuai redeploy -p api-prod -s web`.

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

### Deploy from local source — `upuai up` (no git repo)

**`upuai deploy` deploys from a CONNECTED git repo** (github/gitlab — pulled fresh on every deploy). **`upuai up` deploys from your LOCAL working directory** — it packages the current directory into a tarball, uploads it to platform storage, and triggers a deploy from that source. No connected git repo required. **Introduced in CLI v0.11.0** (`upuai up` is no longer an alias of `deploy`).

**Git is the canonical, recommended path** (source of truth, reproducible/commit-pinned deploys, auto-deploy on push). Prefer `upuai deploy` (or connecting a repo so `git push` auto-deploys) whenever a github/gitlab service is linked. Use `upuai up` only as an **escape hatch**: no git repo connected, or a quick deploy of local code (uncommitted changes, scratch dirs).

```bash
upuai up --wait --yes -o json
```

- Honors `.gitignore` / `.upuaiignore`; always excludes `.git`, `node_modules`, `.env*`.
- Reads local `upuai.toml` exactly like the git path — release-phase / migrations apply identically.
- `--wait` blocks until the deployment reaches a terminal status (exits non-zero on failure); `--wait-timeout <seconds>` caps the wait (default 300).

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
- **No-CLI fallback** — if `upuai init --repo` fails with a GitHub auth error, the user has not authorized the Upuai GitHub App yet. Direct them to `https://app.upuai.com.br/settings/account`, where GitHub installations are connected and managed; then re-run from this section.

### Flag reference (init)

- `--name <slug>` — kebab-case project slug. **Required when `--yes` is set.**
- `--repo <owner>/<repo>` — creates a repo-backed service. **GitHub and GitLab are both supported** — the provider is auto-detected from the URL/host. URLs (`https://github.com/owner/repo[.git]`, `git@github.com:owner/repo`, GitLab equivalents) are normalized to `owner/repo`. If you also pass `--type`, it must be `github` or `gitlab` to match the detected provider. Same auto-detect applies to `upuai add --repo <url>`.
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

0. **"Project not found" / `upuai list` is empty / a project you know exists 404s?** → you are almost certainly in the wrong workspace. `upuai workspace list` shows all of them and marks the active one; `upuai workspace switch <slug>` moves. The API cannot tell you "wrong workspace" directly — it returns 404 for anything outside the active one, by design. See [Workspaces](#workspaces).
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
upuai config show                   # inspect builder/commands/health/root-dir
upuai config set --root-dir apps/api  # set monorepo build Root Directory on an existing github/gitlab service
upuai service delete <name> --yes   # delete ONE service (deployments+volumes+buckets+domains) — NOT the whole project
```

`service delete` is the per-service counterpart to `upuai delete` (whole project) and `upuai down` (stop the deployment, keep the service). It resolves `<name>` by name/slug/id. The cluster teardown runs in the background — the command returns as soon as the request is accepted. The service can be restored from the project's deleted services for 30 days (variables, domains and build config come back); volumes are NOT restored, their disks are erased on delete.

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

# Response headers and path redirects applied at the edge (Traefik), before the
# app — works for static sites and server apps alike. The platform already sends
# X-Content-Type-Options, X-Frame-Options and Referrer-Policy; add or override here.
[http.headers]
Strict-Transport-Security = "max-age=31536000"   # HSTS is opt-in (sticky in browsers; only add includeSubDomains if EVERY subdomain serves HTTPS)

[[http.redirects]]
from = "/precos"          # `:name` matches one path segment, `*` matches the rest
to = "/planos"
status = 301              # 301 (default) or 302
```

Precedence: dashboard UI values win over `upuai.toml`, which wins over Procfile `release:` (legacy). Don't set the same key in two places.

`[http]` applies to ALL hostnames of the service and `from` is always a path. A redirect between domains (www → apex, canonical host) is an attribute of the domain, not of the commit: `upuai domain update www.x.com --redirect-to x.com` — see Custom domains below.

### Environment variables

```bash
upuai vars list -o json                              # current vars (linked service)
upuai vars set DATABASE_URL=postgres://... --yes     # set one
upuai vars set KEY1=val1 KEY2=val2 --yes             # set multiple atomically
upuai vars set -s api API_KEY=xxx --yes              # target a specific service
upuai vars delete SECRET_KEY --yes
```

Shared variables (project/environment layers) are **opt-in per service** — pick which ones a service receives:

```bash
upuai vars shared list -s api                              # shared vars + Enabled/Origin for this service
upuai vars shared enable DATABASE_URL REDIS_URL -s worker  # inject into this service
upuai vars shared disable DATABASE_URL -s site             # stop injecting
upuai vars shared enable PUBLIC_ID --origin project -s api # key defined in both layers
```

Vars take effect on the **next deploy** — trigger `upuai redeploy --yes` if the user expects them live immediately. Never write secrets to the user's chat / repo / logs.

### Custom domains

```bash
upuai domains list -o json                                   # includes `redirectTo` when set
upuai domains add myapp.com --yes                            # adds + returns DNS records to set (for BOTH sides of the apex/www pair)
upuai domains add www.myapp.com --redirect-to myapp.com --status 301 --yes   # www is the domain you add; apex is created as its pair and www redirects to it
upuai domains update www.myapp.com --redirect-to myapp.com   # set/change the canonical-host redirect (301 default, or --status 302)
upuai domains update www.myapp.com --no-redirect             # www serves the app again
upuai domains delete myapp.com --yes                         # hostname or id; deletes the apex/www pair together
```

Adding an apex (`myapp.com`) automatically adds `www.myapp.com` (and vice versa) — the pair counts as ONE domain of the plan quota. A pair created from now on already has the counterpart redirecting to the domain the user added (301, keeps path and query string). Pairs created before this existed serve the same content on both hosts: enable the redirect with `domains update <www> --redirect-to <apex>` (or the dashboard). Any domain of the service can redirect to any other one (e.g. the generated `*.apps.upuai.cloud` host → the custom domain); no chains, no wildcard as source. The redirect lives at the edge and needs no redeploy.

After `domains add`, instruct the user to set the DNS records (CNAME / A) returned by the API at their registrar — both hosts of the pair need records. Propagation can take minutes to hours. Note: `curl -I` (HEAD) shows `308` for permanent redirects; a GET (what browsers and search engines do) gets `301`.

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

## Workspaces

A **workspace** is the top of the hierarchy: `workspace → project → environment → service`. A user can belong to several (a personal one plus each organization they were invited to), but a session is scoped to **one at a time** — the `tenantId` claim in the token.

This matters because the API is fail-closed: anything outside the active workspace answers **404, indistinguishable from "does not exist"**. An empty `upuai list` usually means "wrong workspace", not "no projects".

```bash
upuai workspace list                 # all your workspaces; ● marks the active one
upuai workspace current -o json      # {"workspaceId","workspaceName","role"}
upuai workspace switch tai           # by slug, name or ID
upuai workspace switch               # interactive picker (needs a TTY)
```

**Linked directories pin their workspace.** `upuai init` / `upuai link` record it in `.upuai/config.json`, and any command that resolves the linked project or service realigns the session before calling the API — so entering a project of another workspace just works. The switch is announced on **stderr** (`→ workspace: TAI Tecnologia (was Gabriel Braga)`), never stdout, so `-o json` stays pipeable.

**The realignment follows the target, not the directory.** With `-p` naming another project, the directory stops being the target: its workspace pin does not move the session, and its `environmentId`/`serviceId` are not reused. So after the error below, `workspace switch` sticks and the retry works — the pin no longer cancels it out.

Cross-workspace by ID:
- `upuai link <project-id>` of another workspace **switches and links** — adopting the workspace is what linking means.
- `-p <project-id>` of another workspace does **not** switch (a per-command flag must not move the whole session); it errors telling you which workspace owns the project. Run the `workspace switch` it suggests, then repeat the command with the same `-p`.
- `-p <project-id>` on a command that needs a service, without `-s`, is **refused** — the linked service belongs to this directory's project, so applying it there would act on a target you did not name. Pass `-s <service>`.

Switching rotates the session tokens and is durable: the server pins the workspace to the refresh-token line, so it survives token rotation and later commands.

**Machine tokens do not switch.** A token from `upuai token create` is bound to the workspace it was minted in; `UPUAI_TOKEN` pointing at the wrong workspace is a pipeline misconfiguration — mint a new token inside the target workspace. The CLI says so instead of failing with a generic 403.

## Environments

Environments live **inside** a project, which lives inside a workspace. Upuai supports `production`, `staging`, `development` by default; custom names allowed.

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
- `upuai login` is interactive (browser OAuth or email OTP), same pattern as `railway login` / `vercel login` / `fly auth login`. On agent runners without a browser, ask the user to log in once on their own machine. Credentials persist in `~/.upuai/credentials.json` and refresh automatically. For headless/CI there **is** a sanctioned path — a scoped machine token in `UPUAI_TOKEN` (see the [Non-interactive contract](#non-interactive-contract)); it is opaque, long-lived, revocable, and bound to the workspace it was minted in.
- `upuai add` (without `--type`/`--name`/`--repo`/`--image`), `upuai link` (without `--service`/`--env`), `upuai shell` (subshell), and `upuai db connect` (without `--print`) all need a TTY. Use the non-interactive flag variants noted in the relevant sections above.
- `upuai run` / `upuai shell` run **locally** with the service env vars injected (like `railway run`). `upuai ssh [-s svc] [-- cmd]` opens a session **inside the running production container** (like `railway ssh` / `fly ssh console`) — generic and stack-agnostic (`upuai ssh -- bin/rails console`, `-- python manage.py shell`, `-- node`, or no command for a shell). It runs in the live pod, so destructive commands affect the serving container. **`ssh` works non-interactively too**: a PTY is allocated only when stdin/stdout are terminals, so pipes and redirects return byte-exact output with separate stdout/stderr (`echo "SELECT 1" | upuai ssh -s db -- psql`, `upuai ssh -- cat log.txt > out.txt`). Force with `-t/--tty` or disable with `-T/--no-tty` (parity with `ssh -t/-T`).
- `upuai variables set KEY=val --scope runtime|build` scopes a var to a single phase: `runtime` keeps it out of the build (e.g. `DATABASE_URL`, secrets — avoids baking into a layer / breaking `assets:precompile`); `build` keeps it out of the running container (e.g. a private registry/npm token). Default `both` = build + runtime (Railway/Vercel parity).
- Builds run on Upuai's cluster, not locally. Real-time progress isn't streamed by `upuai deploy --wait` itself; use `upuai logs --build` and `upuai logs --deploy` for live tail of those stages while a deploy is in flight.
