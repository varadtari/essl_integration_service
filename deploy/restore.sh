#!/usr/bin/env bash
# Restores a dump into the stack's database, replacing what is there. The API is stopped while
# it runs so nothing writes mid-restore.
#   deploy/restore.sh backups/najah-20261104T010000Z.dump
set -euo pipefail
cd "$(dirname "$0")"

FILE="${1:?usage: restore.sh <dump file>}"
[ -f "$FILE" ] || { echo "no such file: $FILE"; exit 1; }
DB_USER=$(grep '^DB_USER=' .env | cut -d= -f2-)
DB_NAME=$(grep '^DB_NAME=' .env | cut -d= -f2-)
COMPOSE=(docker compose --env-file .env -f docker-compose.yml)

if [ "${CONFIRM:-}" != "yes" ]; then
  read -r -p "This replaces all data in $DB_NAME with $FILE. Type 'restore' to continue: " answer
  [ "$answer" = "restore" ] || { echo "cancelled"; exit 1; }
fi

"${COMPOSE[@]}" stop api
"${COMPOSE[@]}" exec -T postgres pg_restore -U "$DB_USER" -d "$DB_NAME" --clean --if-exists --no-owner < "$FILE"
"${COMPOSE[@]}" up -d api
echo "restored $FILE"
