# GitHub Integration Plan

## Objectives
- Trigger automated builds and deployments when code lands on tracked branches.
- Maintain a clear mapping between GitHub repositories, control-plane services, and environments.
- Support preview environments for pull requests without manual wiring in app repos.

## Event Flow
1. **installation / installation_repositories**
   - Store installation metadata (ID, account, webhook secret).
   - Sync repo roster; auto-register compose path when present.
2. **push (tracked branch)**
   - Validate webhook signature (now implemented in control plane).
   - Enqueue build job with repo + commit SHA.
   - Build worker clones the repo, builds/pushes the image, uploads the compose file (if present), and calls `/v1/services/{id}/deployments` to update desired state.
3. **pull_request (opened/synchronized/closed)**
   - For `opened/synchronize`: create or refresh preview environment (unique slug, branch-specific deployment).
   - For `closed/merged`: tear down preview resources and mark deployment inactive.

## Build/Deploy Pipeline (Proposed)
1. Fetch repository at commit SHA (using installation token).
2. Detect compose configuration (default `docker-compose.yml` or path stored in control plane).
3. Build images:
   - Option A: `docker compose build` and push resulting images to registry (`ghcr.io/...`).
   - Option B: Use BuildKit/buildpacks per service; store image references back on control plane.
4. Publish artifacts and update deployment intent via control plane API.
5. Host agent observes new desired images/compose specs and reconciles containers (single-container via `docker run`, multi-container via `docker compose`).
6. `build-worker/main.go` provides a long-running worker daemon capable of polling, claiming jobs, cloning repos, building/pushing images, and reporting compose + artifact data back to the control plane. Requires `git`, `docker`, optional `GITHUB_TOKEN`, and registry credentials for pushes.

## Preview Environments
- **Naming:** `<project>-pr-<number>` environment stored alongside production/staging.
- **Routing:** Auto-provision subdomain (e.g., `<pr>.project.example.com`); manage certificates through Traefik config.
- **Lifecycle:** Created on PR open, updated on synchronize, deleted on close/merge.

## Outstanding Tasks
- Registry credentials management (per installation or global).
- Secret and environment variable injection for builds and runtime.
- Metrics/log forwarding per deployment and preview env.
- Hardening: retries, build cache, streaming logs, preview environment automation.
