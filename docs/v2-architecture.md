# V2 Architecture (Draft)

## Goals
- Centralize routing and deployment configuration in a control plane instead of repo-level Traefik labels.
- Deliver a developer-friendly deployment experience (CLI/GitHub App) similar to Vercel.
- Automate routing/rollouts via host agents and shared proxy without manual Docker compose tweaks.
- Remain Hetzner-friendly today, but pave the path to multi-host support.

## Core Components
1. **Control Plane API (`control-plane/`)**
   - Language: Go (standard HTTP + chi/stdlib).
   - Data store: start with file-backed state for iteration; migrate to SQLite/Postgres when ready.
   - Resources: `Project`, `Service`, `Deployment`, `Domain`, `Environment`, `Host`, `Repository`.
   - Responsibilities: validate mutations, track desired state, surface config for agents, emit events.

2. **Developer Interface**
   - CLI (`cmd/infrctl`) hitting the control plane (create project/service, deploy image, add domains, manage secrets).
   - MVP: project/service bootstrap, `deploy` command (set desired image), domain management, register/list GitHub repos.
   - GitHub App integration (future) to auto-wire workflows and preview envs.

3. **Host Agent (`agent/`)**
   - Go daemon (containerized). Polls control plane for host-specific desired state.
   - Applies changes: ensures Docker containers/images running, writes Traefik config, reloads proxy, reports health.
   - Designed to run on every host; decoupled from control plane deployment.
   - MVP: poll `/v1/traefik/config`, write dynamic file locally, log intended container actions.

4. **Routing Layer**
   - Keep Traefik for now. Agent renders dynamic config (routers/services). Evaluate Caddy/other proxies later.

5. **Builder/Registry Pipeline (Later)**
   - BuildKit/buildpacks for auto building repos to OCI images, publish to registry, update control plane deployments.
   - Worker process claims jobs via `/v1/build-jobs/claim`, runs builds, and updates deployment state (CLI worker stub available for manual testing).

6. **GitHub App Integration**
   - GitHub App installed per org/account with webhook delivery to control plane.
   - Stores installation metadata (+ tokens) and repository roster.
   - Installation events auto-sync repository entries; manual CLI overrides remain available.
   - On push: kick off build pipeline, publish images, update deployment state.
   - On PR: optional preview environments, clean-up post merge/close.
   - Detailed event/build flow outlined in [github-pipeline.md](github-pipeline.md).

## Flow Overview
1. Developer triggers deploy (CLI or GitHub App).
2. Control plane records desired deployment (image tag, env, domains).
3. Host agent(s) fetch state for their host, pull images, run containers, update Traefik config.
4. Traefik serves domains with automatic TLS. Control plane/agent report status back to developers.

## Roadmap Milestones
1. **MVP Control Plane**
   - REST endpoints for projects/services/domains/deployments.
   - Traefik config rendering endpoint for agents.
   - Basic admin auth (static token).
2. **Agent Stub**
   - Poll `/v1/traefik/config` (or host-scoped endpoint) and write Traefik dynamic file locally.
   - Stub container lifecycle handlers (log actions, no-op reconciliation).
3. **CLI**
   - Commands to bootstrap projects, push deployments, assign domains.
4. **Builder + GitHub App**
   - Automated builds, preview environments, workflow templates.
   - Control plane stores GitHub installation + repository metadata; CLI assists onboarding.
5. **Multi-Host Scheduling**
   - Track host inventory, schedule deployments, offer failover.

## Deployment Strategy
- Package services/agent as Docker images (multi-stage builds for small binaries).
- Ship via Compose or systemd units calling `docker run`.
- For multi-host: run control plane (and DB) on dedicated host(s). Agents register themselves and pull config.

## Open Questions
- Auth model long term (tokens vs OIDC).
- Secrets storage backend (SOPS/Vault integration).
- Observability stack (logs/metrics) integration.
- Blue/green vs rolling deploy semantics.

## GitHub Integration Flow (Planned)
1. **Install App**
   - User installs GitHub App on selected repos/orgs.
   - Control plane stores installation ID + generated webhook secret (`POST /v1/github/installations`).

2. **Sync Repos**
   - App webhook (`installation_repositories`) notifies control plane about accessible repos.
   - Control plane records repo metadata (owner/name/default branch/compose path) via `/v1/github/repos`.
   - CLI `infrctl github repos` surfaces the list; `github register` allows manual overrides until automation lands.

3. **Onboard Service**
   - Developer links a repo to a project/service via CLI (`infrctl project onboard --repo ...` TBD).
   - Control plane associates service with repo + compose path.

4. **Push / Merge Event**
   - Webhook `push` (main) triggers build pipeline: control plane now enqueues a `BuildJob` persisted in the state file (visible via `/v1/build-jobs`).
   - Worker (future) picks up the job, runs builds, and calls deployment endpoints.
   - Builder (GitHub Action or control-plane worker) checks out repo, runs Compose/Docker build, publishes images.
   - Deployment metadata updated via control plane API (`deployments` endpoint).

5. **Agent Redeploy**
   - Host agent sees new desired image, pulls it, orchestrates container rollout, writes Traefik config if domains changed.

6. **PR Previews (later)**
   - On PR open/synchronize: create preview environment (unique environment slug).
   - On close/merge: tear down preview, preserve artifact history.
