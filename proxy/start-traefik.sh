#!/bin/bash

echo "LETSENCRYPT_STAGING is set to: '$LETSENCRYPT_STAGING'"

# Create dynamic traefik config with conditional staging
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

# Add staging server if LETSENCRYPT_STAGING is true
if [ "$LETSENCRYPT_STAGING" = "true" ]; then
  echo "Using staging Let's Encrypt server"
  echo "      caServer: \"https://acme-staging-v02.api.letsencrypt.org/directory\"" >> /etc/traefik/traefik-dynamic.yml
else
  echo "Using production Let's Encrypt server"
fi

echo "}" >> /etc/traefik/traefik-dynamic.yml

# Start Traefik with the dynamic config
exec traefik --configfile=/etc/traefik/traefik-dynamic.yml
