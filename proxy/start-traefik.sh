#!/bin/bash

CMD="traefik \
  --providers.docker=true \
  --providers.docker.exposedbydefault=false \
  --providers.file.directory=/etc/traefik/dynamic \
  --entrypoints.web.address=:80 \
  --entrypoints.websecure.address=:443 \
  --api.dashboard=false"

# Add staging server if LETSENCRYPT_STAGING is true
if [ "$LETSENCRYPT_STAGING" = "true" ]; then
  CMD="$CMD --certificatesresolvers.le.acme.caserver=https://acme-staging-v02.api.letsencrypt.org/directory"
fi

exec $CMD
