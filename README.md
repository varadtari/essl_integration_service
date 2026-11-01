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
