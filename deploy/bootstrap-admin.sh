#!/usr/bin/env bash
# Creates the client's superadmin login. The internal API is not exposed through Caddy, so
# this calls it from inside the api container.
#   deploy/bootstrap-admin.sh "Client Admin" admin@example.in
# The password is read from the terminal, never from arguments or history.
set -euo pipefail
cd "$(dirname "$0")"

NAME="${1:?usage: bootstrap-admin.sh <name> <email>}"
EMAIL="${2:?usage: bootstrap-admin.sh <name> <email>}"
if [ -n "${ADMIN_PASSWORD:-}" ]; then PASSWORD="$ADMIN_PASSWORD"; else read -r -s -p "Password for $EMAIL: " PASSWORD; echo; fi
API_KEY=$(grep '^API_KEY=' .env | cut -d= -f2-)

BODY=$(printf '{"name":"%s","email":"%s","password":"%s"}' "$NAME" "$EMAIL" "$PASSWORD")
docker compose --env-file .env -f docker-compose.yml exec -T api \
  wget -qO- --header "x-api-key: $API_KEY" --header "Content-Type: application/json" \
  --post-data "$BODY" http://localhost:8001/internal/v1/superadmins
echo
