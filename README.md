# Multi-Domain Infrastructure

Traefik v3 reverse proxy with automatic Let's Encrypt certificates for hosting multiple domains on a single Hetzner server.

> **V2 (WIP)**: The `v2` branch is evolving toward a control-plane-driven experience (Go services, host agent, Docker-based deploys). See [docs/v2-architecture.md](docs/v2-architecture.md) for scope and roadmap.

## Setup

1. **DNS**: Point A/AAAA records for your domains to server IP
2. **GitHub Secrets** (in this repo):

   - `SERVER_HOST` - Server IP/hostname
   - `SERVER_USER` - SSH user with Docker perms
   - `SERVER_SSH_KEY` - SSH private key (PEM)
   - `LETSENCRYPT_EMAIL` - Email for Let's Encrypt
   - `LETSENCRYPT_STAGING` - Set to `true` for staging certs

3. **Deploy**: Run `Proxy » Deploy` workflow in GitHub Actions

## Deploy Apps

Each app repo calls the reusable workflow. Example:

**docker-compose.yml**:

```yaml
networks:
  proxy:
    external: true

services:
  app:
    build: ./myapp
    container_name: myapp
    restart: unless-stopped
    networks: [proxy]
    labels:
      - traefik.enable=true
      - traefik.http.routers.myapp.rule=Host(`myapp.com`)
      - traefik.http.routers.myapp.entrypoints=websecure
      - traefik.http.routers.myapp.tls.certresolver=le
      - traefik.http.services.myapp.loadbalancer.server.port=80
```

**.github/workflows/deploy.yml**:

```yaml
name: Deploy
on:
  workflow_dispatch:
  push:
    branches: [main]

jobs:
  deploy:
    uses: 0xEdouard/multi-domain-infra/.github/workflows/reusable-remote-deploy.yml@main
    with:
      stack_name: studio-51
      remote_compose_path: docker-compose.yml
    secrets:
      SERVER_HOST: ${{ secrets.SERVER_HOST }}
      SERVER_USER: ${{ secrets.SERVER_USER }}
      SERVER_SSH_KEY: ${{ secrets.SERVER_SSH_KEY }}
```

## V2 Control Plane (Local Dev)

### Run with Go
1. Install Go 1.22+
2. From repo root run `cd control-plane && go run .`
3. Flags:
   - `--addr` (default `:8080`)
   - `--state` (default `./data/state.json`)
   - `--api-token` (optional bearer token)
   - `--le-resolver` (Traefik resolver name, default `le`)

### Run with Docker
```bash
cd control-plane
docker compose up --build
```

- Override auth token: `CP_API_TOKEN=supersecret docker compose up --build`
- State persists to `control-plane/data/state.json` (mounted volume).
- Traefik config available at `GET http://localhost:8080/v1/traefik/config`
- Future host agents will poll the API to manage proxy + workloads automatically.

## Agent Stub

### Run with Go
1. Install Go 1.22+
2. `cd agent && go run . --control-plane=http://localhost:8080 --traefik-file=./tmp/traefik.yml`

Flags / env:
- `--control-plane` or `CONTROL_PLANE_URL`
- `--token` or `CONTROL_PLANE_TOKEN`
- `--traefik-file` or `TRAEFIK_DYNAMIC_PATH`
- `--poll-interval` (default `15s`)

### Run with Docker
```bash
cd agent
docker build -t infra-agent .
docker run --rm -e CONTROL_PLANE_URL=http://host.docker.internal:8080 \
  -v $(pwd)/tmp:/data \
  infra-agent --traefik-file=/data/traefik.yml
```

The stub currently writes the fetched Traefik config and logs a TODO for container reconciliation.

## CLI (`infrctl`)

Build/run with Go 1.22+:
```bash
cd cmd/infrctl
go run . help
```

Environment:
- `INFRCTL_API` (default `http://localhost:8080`)
- `INFRCTL_TOKEN` (optional bearer token)

Example flow:
```bash
infrctl project create --name "Demo"
infrctl service create --project <project-id> --name web --image nginx:alpine --port 8080
infrctl domain add --service <service-id> --hostname demo.example.com
infrctl deploy set --service <service-id> --image ghcr.io/org/demo:sha123
infrctl builds list
```

### GitHub integration stubs
- List registered repositories: `infrctl github repos`
- Register a repo manually (GitHub App plumbing TBD):  
  `infrctl github register --repo owner/name --branch main --compose docker-compose.yml`
- GitHub App installation events auto-register repositories; the manual command is only needed for overrides or repos without installation scopes.
- Record an installation (temporary manual step):  
  `infrctl github installations register --account org --external-id 12345 --secret <webhook-secret>`
- Control plane endpoint: `POST /v1/github/repos` records owner/name, branch, compose path, installation ID (if available).  
  `POST /v1/github/installations` captures installation IDs + secrets. `POST /v1/github/webhook` currently accepts events and will verify signatures once secrets are linked.  
  This is the foundation for wiring real GitHub webhooks and automated deploys.
- See [docs/github-pipeline.md](docs/github-pipeline.md) for the proposed build/deploy pipeline and preview environment flow.
- Build jobs queued from `push` webhooks are visible via `infrctl builds list`; update status manually with `infrctl builds update --id <job> --status running|succeeded|failed` while the automated builder is under construction.
- Run the stub worker locally to auto-claim jobs: `infrctl builds worker --name local --interval 5s --auto-complete`.
- Dedicated worker binary (see `build-worker/`) can poll `/v1/build-jobs/claim`, simulate/perform builds, and report status back to the control plane.
