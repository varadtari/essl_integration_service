#!/usr/bin/env bash
# Seeds a local database with a superadmin and one demo gym so the portal can be tried.
# Local development only: these are throwaway credentials. Run with the service up.
#   scripts/dev_seed.sh            (uses API_KEY from .env, service on localhost:8001)
set -euo pipefail

cd "$(dirname "$0")/.."
API_KEY="${API_KEY:-$(grep '^API_KEY=' .env | cut -d= -f2)}"
BASE="${BASE_URL:-http://localhost:8001}"
JAR="$(mktemp)"
trap 'rm -f "$JAR"' EXIT

ADMIN_EMAIL="admin@najah.local"
ADMIN_PASSWORD="admin-pass-123"
OWNER_EMAIL="owner@panaji.local"
OWNER_PASSWORD="owner-pass-123"

echo "superadmin: $(curl -s -X POST "$BASE/internal/v1/superadmins" -H "x-api-key: $API_KEY" -H 'Content-Type: application/json' \
  -d "{\"name\":\"Client Admin\",\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}" | head -c 160)"

curl -s -c "$JAR" -X POST "$BASE/v1/auth/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}" > /dev/null

echo "demo gym:   $(curl -s -b "$JAR" -X POST "$BASE/admin/v1/gyms" -H 'Content-Type: application/json' \
  -d "{\"name\":\"Panaji Iron Works\",\"owner\":{\"name\":\"Savio\",\"email\":\"$OWNER_EMAIL\",\"password\":\"$OWNER_PASSWORD\"}}" | head -c 160)"

echo
echo "Sign in at http://localhost:5173 as $ADMIN_EMAIL (superadmin) or $OWNER_EMAIL (gym owner)."
echo "Passwords are in this script. A 409 means the account already exists."
