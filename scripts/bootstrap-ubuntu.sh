#!/usr/bin/env bash
set -euo pipefail

# Install Docker Engine (official convenience script)
if ! command -v docker >/dev/null 2>&1; then
  curl -fsSL https://get.docker.com | sh
fi

# Add current user to docker group (won't take effect until re-login)
if id -nG "$SUDO_USER" 2>/dev/null | grep -qw docker; then
  :
else
  usermod -aG docker "${SUDO_USER:-$USER}" || true
fi

# Ensure Compose plugin exists
if ! docker compose version >/dev/null 2>&1; then
  echo "Docker Compose plugin missing. Install via apt (Docker repo) or package manager."
  echo "If using the convenience script, compose plugin should be installed automatically."
fi

# Create shared external network for proxy <-> apps (idempotent)
docker network create proxy >/dev/null 2>&1 || true

# Prepare proxy storage
mkdir -p proxy/letsencrypt
chmod 700 proxy/letsencrypt
touch proxy/letsencrypt/acme.json
chmod 600 proxy/letsencrypt/acme.json

echo "Bootstrap complete. (Re-login may be needed for docker group membership.)"