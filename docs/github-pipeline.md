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
   - Output image tag(s) and call `/v1/services/{id}/deployments` to update desired state.
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
5. Host agent observes new desired images and reconciles containers.
6. Temporary: `infrctl builds worker` can claim jobs and mark them complete for manual testing.
7. `build-worker/main.go` provides a long-running worker daemon capable of polling, claiming jobs, and (currently) simulating builds/pushing status. Replace `performBuild` with real checkout/build logic.

## Preview Environments
- **Naming:** `<project>-pr-<number>` environment stored alongside production/staging.
- **Routing:** Auto-provision subdomain (e.g., `<pr>.project.example.com`); manage certificates through Traefik config.
- **Lifecycle:** Created on PR open, updated on synchronize, deleted on close/merge.

## Outstanding Tasks
- Registry credentials management (per installation or global).
- Secret and environment variable injection for builds and runtime.
- Metrics/log forwarding per deployment and preview env.
- Implement build worker that checks out code, runs build, pushes images, and updates deployments.
