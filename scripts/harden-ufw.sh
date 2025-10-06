#!/usr/bin/env bash
set -euo pipefail

apt-get update
apt-get install -y ufw

ufw default deny incoming
ufw default allow outgoing
ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp

yes | ufw enable
ufw status verbose