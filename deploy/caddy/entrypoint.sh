#!/bin/sh
set -eu
CLOUDFLARE_API_TOKEN=$(cat /etc/caddy/cloudflare-api-token)
export CLOUDFLARE_API_TOKEN
exec "$@"
