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

## Live events

`GET /v1/events/stream` is a Server-Sent Events stream for the gym's dashboard:

| Event | Data | When |
|---|---|---|
| `ready` | `{gymId}` | On connect |
| `checkin` | a check-in, as in `/v1/checkins` | A new punch is stored |
| `device` | `{deviceId}` | A device connects for the first time or after being offline |
| `refresh` | `{}` | Events may have been missed (big backlog, slow client, listener reconnect): reload |
| `signedout` | `{}` | The user was deactivated; the stream then closes |

Punches are announced with `pg_notify` inside the insert transaction, so only committed rows are
announced. One `LISTEN` connection per process relays them to the open streams of that gym.
Behind a reverse proxy, disable response buffering for this path.

## Device simulator

`cmd/devsim` speaks ADMS like a real terminal, for development without hardware and for load
tests. Register its serial in the portal first (Settings → Devices), then:

```sh
go run ./cmd/devsim -sn DEVSIM0001
> punch 3 face      # member with device pin 3 checks in
> offline           # punches are kept on the "device"
> online            # ...and uploaded as a backlog
```

Commands sent from the portal (open door, re-pull logs) are printed as they arrive and
acknowledged. Load test, registering the devices with a gym owner's login:

```sh
DEVSIM_EMAIL=... DEVSIM_PASSWORD=... go run ./cmd/devsim -load 50 -register -duration 1m
```

Locally, 50 devices polling every 5 s and punching every ~5 s each ran with no errors at
p95 11 ms per punch upload and 8.5 ms per poll.

## API

All responses use the template's shape: `{"type":"success","message":…}` or an
`ApplicationError` whose `message.displayMessage` is safe to show to users. Money is in paise;
dates are `YYYY-MM-DD` calendar days in the gym's timezone.

| Group | Who | Routes |
|---|---|---|
| `/v1/auth` | anyone | `POST /login`, `POST /logout` |
| `/v1/me` | any session | `GET` |
| gym | owner, staff | `GET /v1/dashboard/summary`, `GET/PATCH /v1/gym`, `GET/POST /v1/plans`, `PATCH /v1/plans/:planId`, `GET/POST /v1/members`, `GET/PATCH/DELETE /v1/members/:memberId`, `POST /v1/members/:memberId/renew`, `GET /v1/overdue`, `GET /v1/payments?month=YYYY-MM`, `GET/POST /v1/staff`, `PATCH /v1/staff/:userId` |
| gym (devices) | owner, staff | `GET/POST /v1/devices`, `GET/PATCH/DELETE /v1/devices/:deviceId`, `GET/POST /v1/devices/:deviceId/commands`, `GET /v1/checkins?from&to&memberId&deviceId&verdict&limit&before`, `GET /v1/events/stream` (Server-Sent Events) |
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
