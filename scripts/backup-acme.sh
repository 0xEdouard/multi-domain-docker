#!/usr/bin/env bash
set -euo pipefail

DEST="${1:-/root/acme-backups}"
STAMP="$(date +%Y%m%d-%H%M%S)"
mkdir -p "$DEST"
cp proxy/letsencrypt/acme.json "$DEST/acme-${STAMP}.json"
echo "Backed up to $DEST/acme-${STAMP}.json"