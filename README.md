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
    image: myapp:latest
    networks: [proxy]
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.myapp.rule=Host(`myapp.com`)"
      - "traefik.http.routers.myapp.entrypoints=websecure"
      - "traefik.http.routers.myapp.tls.certresolver=le"
      - "traefik.http.middlewares.security-headers@file"
```

**.github/workflows/deploy.yml**:

```yaml
name: Deploy
on: [push]
jobs:
  deploy:
    uses: YOUR_ORG/multi-domain-infra/.github/workflows/reusable-remote-deploy.yml@main
    with:
      stack-name: myapp
      compose-path: docker-compose.yml
    secrets: inherit
```

## Operations

- **Update proxy**: Modify `proxy/` files, re-run `Proxy » Deploy`
- **Backup certs**: `sudo bash scripts/backup-acme.sh`
- **View logs**: `sudo docker logs traefik`
