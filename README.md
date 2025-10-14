# Multi-Domain Infrastructure (V2)

Unified control plane, build worker, and host agent for multi-domain deployments on a single Docker host. The original V1 (Traefik labels + rsync workflow) has been retired on this branch in favour of an automated platform experience.

## Components
- **control-plane/** – Go API that stores projects/services/domains, renders Traefik config, tracks build jobs, and exposes service state for the agent.
- **build-worker/** – Long-running worker that claims build jobs, clones repos, builds/pushes images, and calls the deployment API.
- **agent/** – Host-side daemon that writes Traefik config and reconciles Docker containers against desired state.
- **cmd/infrctl/** – CLI for managing projects/services/domains/deployments and binding GitHub repos.
- **docs/** – Architecture overview and GitHub integration notes.

## Quick Start
1. **Control Plane**
   ```bash
   cd control-plane
   go run . --state ./data/state.json
   ```
   - Flags: `--addr`, `--state`, `--api-token`, `--le-resolver`.
   - Rendered Traefik config: `GET /v1/traefik/config`.

2. **Agent** (runs on the Docker host)
   ```bash
   cd agent
   go run . --control-plane=http://localhost:8080 --traefik-file=/tmp/traefik.yml \
     --deploy-interval=20s --compose-dir ./compose
   ```
   - Requires Docker/Compose access (mount `/var/run/docker.sock` if containerised).
   - Mirrors containers labelled `mdp.service=<service-id>` and maps service ports to `127.0.0.1:<internal_port>`.

3. **Build Worker**
   ```bash
   cd build-worker
   go run . --control-plane=http://localhost:8080 --token=$CP_TOKEN \
     --workspace ./worker-tmp --registry ghcr.io/your-org --push
   ```
   - Needs `git` + `docker`. Provide `GITHUB_TOKEN` for private repos, and Docker login for `--push`.

4. **CLI**
   ```bash
   go run ./cmd/infrctl help
   ```
   - Example flow:
     ```bash
     infrctl project create --name "Demo"
     infrctl service create --project <project-id> --name web --image ghcr.io/org/web:latest --port 8080
     infrctl domain add --service <service-id> --hostname demo.example.com
     infrctl github register --repo owner/repo --service <service-id> --env production
     ```

## GitHub Integration Notes
- Install the GitHub App and register repositories with `infrctl github register`. Push events queue build jobs automatically.
- Build jobs include repo/ref/image metadata; the worker updates deployments once the image is built.
- Additional details: [docs/github-pipeline.md](docs/github-pipeline.md).

## Repository Layout
- `control-plane/`: Go module with HTTP API and JSON-backed state.
- `agent/`: Docker/Traefik reconciler.
- `build-worker/`: Build + deploy pipeline runner.
- `cmd/infrctl/`: End-user CLI.
- `docs/`: Architectural docs and integration plans.
- `go.work`: Go workspace binding the modules.

## Development Notes
- All Go modules target Go 1.22+. Run `gofmt`/`go test` within each module once you install the toolchain.
- Docker/Compose must be available wherever the agent/build worker run.
- Traefik dynamic configuration lives under `/v1/traefik/config`; host certificates/ACME handling remains managed by Traefik itself.

