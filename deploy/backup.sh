#!/usr/bin/env bash
# Dumps the database to deploy/backups/najah-<UTC timestamp>.dump (custom format, compressed)
# and keeps the newest 30. Run nightly from cron; copy the files off the server (docs/deployment.md).
set -euo pipefail
cd "$(dirname "$0")"

DB_USER=$(grep '^DB_USER=' .env | cut -d= -f2-)
DB_NAME=$(grep '^DB_NAME=' .env | cut -d= -f2-)
mkdir -p backups
FILE="backups/najah-$(date -u +%Y%m%dT%H%M%SZ).dump"

docker compose --env-file .env -f docker-compose.yml exec -T postgres \
  pg_dump -U "$DB_USER" -d "$DB_NAME" --format=custom --no-owner > "$FILE.partial"
mv "$FILE.partial" "$FILE"

ls -1t backups/najah-*.dump | tail -n +31 | xargs -r rm --
echo "$FILE ($(du -h "$FILE" | cut -f1))"
