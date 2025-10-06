# Hetzner Infra (multi-domain Traefik + Docker)

Infra-only repo for a single Hetzner server that fronts many domains/subdomains.

- Docker + Compose bootstrap
- Shared external network: `proxy`
- Traefik v3 reverse proxy, auto HTTPS (Let's Encrypt), security headers
- CI/CD:
  - `proxy-deploy.yml` keeps the proxy up-to-date
  - `reusable-remote-deploy.yml` is a reusable workflow your **app repos** call to deploy/update their stacks via SSH (no monorepo)

## 0) Prereqs

- Point DNS A/AAAA records for your domains/subdomains to the server IP.
- Add the following **GitHub Actions Repository Secrets** (in _this_ repo):
  - `SERVER_HOST` → e.g. `203.0.113.10`
  - `SERVER_USER` → e.g. `deploy` (create or use an SSH-able user with docker perms)
  - `SERVER_SSH_KEY` → private key (PEM) for that user
- Optionally add `KNOWN_HOSTS` to pin the server host key (output of `ssh-keyscan -t ed25519 <ip>`). If omitted, we auto-accept on first run.

## 1) First-time bootstrap (once)

SSH into the server (as root or a sudoer) and run:

```bash
apt update && apt install -y git
git clone https://github.com/YOU/hetzner-infra.git
cd hetzner-infra
cp .env.example .env  # edit email + staging toggle
sudo bash scripts/bootstrap-ubuntu.sh
# (optional) lock down firewall:
sudo bash scripts/harden-ufw.sh
```
