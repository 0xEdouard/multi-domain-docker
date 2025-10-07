# Multi-Domain Infrastructure on Hetzner

This repository codifies everything needed to run a small Hetzner (or any Docker-capable) server that reverse-proxies many domains and deploys application stacks through GitHub Actions. It is intended for solo developers and small teams who want HTTPS, Traefik, and automated deployments without hand-configuring each host.

---

## What You Get

- **Traefik v3 reverse proxy** with automatic Let's Encrypt certificates, sane security headers, and an external Docker network shared by all app stacks.
- **Bootstrap scripts** that harden a fresh Ubuntu host, install Docker, and prepare the shared `proxy` network.
- **GitHub Actions workflows** for keeping the proxy updated and for remotely deploying app-specific stacks over SSH.
- **Utilities** like automatic ACME backup and optional UFW firewall hardening.

---

## Repository Layout

| Path | Purpose |
|------|---------|
| `proxy/` | Docker Compose stack for Traefik and its dynamic configuration (middleware and TLS settings). |
| `scripts/bootstrap-ubuntu.sh` | One-time server bootstrap: installs Docker, creates the shared `proxy` network, configures cron backups, etc. |
| `scripts/harden-ufw.sh` | Optional script to enable and configure UFW firewall rules. |
| `scripts/backup-acme.sh` | Cron-friendly script that archives Traefik's ACME data. |
| `.github/workflows/proxy-deploy.yml` | CI workflow that deploys the `proxy` stack to the server. |
| `.github/workflows/reusable-remote-deploy.yml` | Reusable workflow that application repos can call to deploy their own stacks onto the same server. |

---

## Prerequisites

1. **Domain DNS**: Point the A/AAAA records for every domain/subdomain you plan to serve to the public IP of your server.
2. **Server Access**: Provision an Ubuntu server (tested on 22.04+) with sudo access via SSH.
3. **GitHub Secrets**: In _this_ repository, add the following secrets so GitHub Actions can SSH to the server:
   - `SERVER_HOST` – The server's IP or hostname (e.g. `203.0.113.10`).
   - `SERVER_USER` – The SSH user with Docker permissions (e.g. `deploy`).
   - `SERVER_SSH_KEY` – Private key for the SSH user in PEM format.
   - `KNOWN_HOSTS` (optional) – Output of `ssh-keyscan -t ed25519 <SERVER_HOST>` to pin the host key. If omitted, Actions accept the host on first run.

---

## First-Time Server Bootstrap

SSH into the server (as root or a sudo-capable user) and execute:

```bash
sudo apt update && sudo apt install -y git
sudo git clone https://github.com/YOUR_ORG/multi-domain-infra.git /opt/multi-domain-infra
cd /opt/multi-domain-infra
cp .env.example .env   # Set LETSENCRYPT_EMAIL and optionally enable staging mode
sudo bash scripts/bootstrap-ubuntu.sh
# (Optional) Lock down the firewall
sudo bash scripts/harden-ufw.sh
```

What the bootstrap script does:
- Installs Docker Engine, Docker Compose plugin, and dependencies.
- Creates an external Docker network named `proxy` for Traefik and all app stacks.
- Sets up directories for Traefik config and certificate storage under `/opt/multi-domain-infra/proxy`.
- Configures log rotation and a cron job for ACME backups using `scripts/backup-acme.sh`.

Once complete, the server is ready to receive deployments from GitHub Actions.

---

## Deploying the Traefik Proxy

The `proxy` stack hosts Traefik and must be running before any applications. Deploy it by running the `Proxy » Deploy` workflow in GitHub Actions. The workflow connects to the server via SSH, pulls the repo, and runs `docker compose up -d` in the `proxy/` directory.

You can re-run this workflow anytime you update Traefik configuration or want to refresh certificates.

Manual deployment is also possible:

```bash
cd /opt/multi-domain-infra/proxy
sudo docker compose pull
sudo docker compose up -d
```

---

## Creating and Deploying Application Stacks

Each application stack lives in its _own_ GitHub repository. Those repos call this repo's reusable workflow to build and deploy themselves onto the server, sharing the same Traefik proxy and Docker network.

1. In the application repo, create a workflow file that uses `multi-domain-infra/.github/workflows/reusable-remote-deploy.yml`.
2. Provide the deployment SSH secrets in the app repo (`SERVER_HOST`, `SERVER_USER`, `SERVER_SSH_KEY`, optional `KNOWN_HOSTS`).
3. Ensure the app's Docker Compose file joins the external `proxy` network and adds the necessary Traefik labels.

### Example: Minimal "Who Am I" Stack

`docker-compose.yml`

```yaml
name: whoami

networks:
  proxy:
    external: true

services:
  app:
    image: traefik/whoami:v1.10
    restart: unless-stopped
    networks:
      - proxy
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.whoami.rule=Host(`whoami.example.com`)"
      - "traefik.http.routers.whoami.entrypoints=websecure"
      - "traefik.http.routers.whoami.tls.certresolver=letsencrypt"
      - "traefik.http.middlewares.secure-headers@file"
```

`.github/workflows/deploy.yml`

```yaml
name: Deploy

on:
  push:
    branches: [main]

jobs:
  deploy:
    uses: YOUR_ORG/multi-domain-infra/.github/workflows/reusable-remote-deploy.yml@main
    with:
      stack-name: whoami
      compose-path: docker-compose.yml
    secrets:
      SERVER_HOST: ${{ secrets.SERVER_HOST }}
      SERVER_USER: ${{ secrets.SERVER_USER }}
      SERVER_SSH_KEY: ${{ secrets.SERVER_SSH_KEY }}
      KNOWN_HOSTS: ${{ secrets.KNOWN_HOSTS }}
```

When this workflow runs, it SSHes into the server, uploads your Compose file, and runs `docker compose up -d` under `/opt/stacks/whoami` (the default location created by the reusable workflow). Traefik immediately discovers the new container via Docker labels and begins routing traffic with HTTPS certificates issued automatically.

---

## Common Operations

| Task | How |
|------|-----|
| Update Traefik version or config | Modify files under `proxy/` and re-run the `Proxy » Deploy` workflow. |
| Back up certificates | `scripts/backup-acme.sh` runs automatically if you keep the cron job installed; run it manually with `sudo bash scripts/backup-acme.sh`. |
| Rotate Let's Encrypt email or toggle staging | Edit `.env` on the server (and in repo if tracked) then redeploy the proxy. |
| Inspect running containers | `sudo docker ps` or `sudo docker compose ls`. |
| Tail Traefik logs | `sudo journalctl -u docker -f` or `sudo docker logs traefik`. |

---

## Troubleshooting Tips

- Ensure your DNS changes have propagated before expecting valid certificates.
- If a deployment fails, rerun the GitHub Action with debug logging enabled (`ACTIONS_STEP_DEBUG=true`) to view SSH output.
- For staging certificates (rate-limit safe), set `LETSENCRYPT_STAGING=true` in `.env` before the first proxy deploy.
- Remember to keep the server updated (`sudo apt upgrade`) and monitor disk usage (`df -h`) to avoid Docker filling the root partition.

---

## Next Steps

- Add more middleware under `proxy/dynamic/` as needed (rate limiting, basic auth, etc.).
- Extend `scripts/backup-acme.sh` to push backups to remote storage.
- Fork this repo and tailor the workflows or scripts to match your team's naming conventions.

Happy shipping! 🚢
