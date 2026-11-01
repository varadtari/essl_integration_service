# essl_integration_service

Backend for the Najah gym portal. Gyms' eSSL AI Face Magnum devices push check-ins to this
service over ADMS, and membership expiry drives door access on the device.

Layout and conventions follow the `sso_service` template: Gin, controllers → services → DAOs,
raw SQL on pgx, golang-migrate migrations, zap logging, `ApplicationError` / `ApiError`.

## Local development

Prerequisites: Go 1.26, Docker, [golang-migrate](https://github.com/golang-migrate/migrate), [just](https://github.com/casey/just).

```sh
cp .env.example .env     # fill in API_KEY
just db-up               # Postgres 16 on localhost:55432 (clear of system Postgres on 5432/5433)
just migrate-up
just run                 # http://localhost:8001/v1/health
just test
```

### First login

Create the client's superadmin through the internal route (needs `API_KEY` from `.env`):

```sh
curl -X POST localhost:8001/internal/v1/superadmins \
  -H "x-api-key: $API_KEY" -H "Content-Type: application/json" \
  -d '{"name":"Admin","email":"admin@example.com","password":"change-me-please"}'
```

For local development, `scripts/dev_seed.sh` creates a superadmin and a demo gym with
throwaway credentials (listed in the script).

The superadmin then onboards each gym (`POST /admin/v1/gyms`), which creates the gym, its
owner login and the default plans.

## Tests

Unit tests run anywhere. The HTTP tests in `test/` need a separate database, which they
**truncate** on every test, so it must never be the one you develop against:

```sh
docker compose -f deploy/docker-compose.dev.yml exec postgres \
  psql -U najah_local -d najah_local -c 'CREATE DATABASE najah_test OWNER najah_local'
migrate -database "postgres://najah_local:najah_local@localhost:55432/najah_test?sslmode=disable" \
  -path database/migrations up
echo TEST_DB_NAME=najah_test >> .env
go test ./...
```

Without `TEST_DB_NAME` the database tests are skipped.

## API

All responses use the template's shape: `{"type":"success","message":…}` or an
`ApplicationError` whose `message.displayMessage` is safe to show to users. Money is in paise;
dates are `YYYY-MM-DD` calendar days in the gym's timezone.

| Group | Who | Routes |
|---|---|---|
| `/v1/auth` | anyone | `POST /login`, `POST /logout` |
| `/v1/me` | any session | `GET` |
| gym | owner, staff | `GET /v1/dashboard/summary`, `GET/PATCH /v1/gym`, `GET/POST /v1/plans`, `PATCH /v1/plans/:planId`, `GET/POST /v1/members`, `GET/PATCH/DELETE /v1/members/:memberId`, `POST /v1/members/:memberId/renew`, `GET /v1/overdue`, `GET /v1/payments?month=YYYY-MM`, `GET/POST /v1/staff`, `PATCH /v1/staff/:userId` |
| gym (devices) | owner, staff | `GET/POST /v1/devices`, `GET/PATCH/DELETE /v1/devices/:deviceId`, `GET/POST /v1/devices/:deviceId/commands`, `GET /v1/checkins?from&to&memberId&deviceId&verdict&limit&before` |
| `/iclock` | devices (by registered `SN`) | ADMS: see [docs/adms-protocol.md](docs/adms-protocol.md) |
| `/admin` | superadmin | `GET/POST /v1/gyms`, `PATCH /v1/gyms/:gymId` |
| `/internal` | `x-api-key` | `POST /v1/superadmins` |

Owner-only within the gym group: gym settings, plans, staff, deleting members, adding and
removing devices, and device commands other than opening the door. The gym a
request acts on always comes from the session, never from the URL or body.

## Migrations

```sh
just migrate-new <name>  # database/migrations/0000NN_<name>.{up,down}.sql
just migrate-up
just migrate-down-1
```

Each migration is wrapped in `BEGIN; … COMMIT;`, uses quoted camelCase columns, and attaches
`trigger_set_timestamp()` to every table with an `updatedAt` column.

## Git workflow

Never commit to `main`. Branch from the latest `origin/main` as `feature/<name>`, push the
branch, and open a pull request.
