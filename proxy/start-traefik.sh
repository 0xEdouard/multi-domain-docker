#!/bin/bash

if [ "$LETSENCRYPT_STAGING" = "true" ]; then
  cat > /etc/traefik/traefik-dynamic.yml << EOF
log:
  level: WARN

accessLog: {}

providers:
  docker:
    exposedByDefault: false
  file:
    directory: /etc/traefik/dynamic
    watch: true

entryPoints:
  web:
    address: ":80"
  websecure:
    address: ":443"

certificatesResolvers:
  le:
    acme:
      email: "${LETSENCRYPT_EMAIL}"
      storage: /letsencrypt/acme.json
      tlsChallenge: {}
      caServer: "https://acme-staging-v02.api.letsencrypt.org/directory"
EOF
else
  cat > /etc/traefik/traefik-dynamic.yml << EOF
log:
  level: WARN

accessLog: {}

providers:
  docker:
    exposedByDefault: false
  file:
    directory: /etc/traefik/dynamic
    watch: true

entryPoints:
  web:
    address: ":80"
  websecure:
    address: ":443"

certificatesResolvers:
  le:
    acme:
      email: "${LETSENCRYPT_EMAIL}"
      storage: /letsencrypt/acme.json
      tlsChallenge: {}
EOF
fi

# Start Traefik with the dynamic config
exec traefik --configfile=/etc/traefik/traefik-dynamic.yml