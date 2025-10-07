# Multi-Domain Infrastructure

Traefik v3 reverse proxy with automatic Let's Encrypt certificates for hosting multiple domains on a single Hetzner server.

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
