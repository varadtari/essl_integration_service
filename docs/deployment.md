# Deployment

One VM runs everything with Docker Compose (`deploy/`): **Caddy** (HTTPS, routing), the Go
**api**, the **portal** (static build) and **Postgres**. Migrations run as a one-off container
before the api starts. Built images live in GitHub's container registry (ghcr.io).

```
app.<domain>        → portal; /api/* → api (the live feed is streamed unbuffered)
adms.<domain>       → api /iclock  (devices, HTTPS)
<server>:8081       → api /iclock  (devices, plain HTTP, for firmware without TLS)
```

`/api/internal/*` is not reachable from outside; ops tasks run on the server.

## Test the stack locally

Everything below was checked this way before any server existed.

```sh
docker build -t essl_integration_service:local .
docker build -f Dockerfile.migrate -t essl_integration_service-migrate:local .
(cd ../najah_portal && docker build --build-arg VITE_ADMS_SERVER=adms.localhost -t najah_portal:local .)

cd deploy
cp .env.example .env     # set APP_DOMAIN=app.localhost, ADMS_DOMAIN=adms.localhost, fill the secrets
docker compose --env-file .env -f docker-compose.yml up -d --wait
ADMIN_PASSWORD=... ./bootstrap-admin.sh "Client Admin" admin@example.in
```

Open https://app.localhost (Caddy's local certificate: accept the browser warning, or
`curl -k`). Point the simulator at the plain-HTTP device port:
`go run ./cmd/devsim -server http://localhost:8081 -sn <registered serial>`.

## First deployment (Oracle Cloud Always Free)

1. **VM.** Ubuntu 24.04, Ampere A1 (ARM) or AMD shape, in Mumbai or Hyderabad. Reserve a
   **static public IP**. Add your SSH key.
2. **Open ports** 80, 443 and 8081 (TCP) in the VCN security list. Oracle's Ubuntu images
   also block them in the VM's own iptables:
   ```sh
   for p in 80 443 8081; do sudo iptables -I INPUT 6 -m state --state NEW -p tcp --dport $p -j ACCEPT; done
   sudo netfilter-persistent save
   ```
3. **DNS.** `app.<domain>` and `adms.<domain>` → the static IP (A records; on Cloudflare set
   them to "DNS only", not proxied, so devices and Let's Encrypt reach the VM directly).
4. **Docker.** `curl -fsSL https://get.docker.com | sh`, then `sudo usermod -aG docker $USER`
   and log in again.
5. **Registry access.** Images on ghcr.io are private by default. Create a GitHub token with
   only `read:packages` and on the VM run `docker login ghcr.io -u varadtari`.
6. **Files.** `sudo mkdir -p /opt/najah && sudo chown $USER /opt/najah`; copy `deploy/*` there
   and create `/opt/najah/.env` from `.env.example` with real domains and new secrets
   (`openssl rand -hex 32`). Set the three `*_IMAGE` lines to the `ghcr.io/varadtari/...:latest`
   images.
7. **Start.** `docker compose --env-file .env -f docker-compose.yml up -d --wait`, then
   `./bootstrap-admin.sh "Client Admin" <email>`. Caddy obtains the certificates on first
   request; check `docker compose logs caddy` if https://app.<domain> does not load.
8. **CI deploys.** In each GitHub repo: secrets `DEPLOY_HOST`, `DEPLOY_USER`, `DEPLOY_SSH_KEY`
   (a key used only for deploys, in the VM user's `authorized_keys`); variables
   `DEPLOY_ENABLED=true`, and in the portal repo `VITE_ADMS_SERVER=adms.<domain>` and
   `VITE_ADMS_PORT` (8081 for plain HTTP). From then on, merging to `main` deploys.

### First-deploy checklist

- [ ] https://app.<domain> loads with a valid certificate; http redirects to https.
- [ ] Sign in; the session cookie is `Secure; HttpOnly`.
- [ ] `https://app.<domain>/api/internal/v1/superadmins` answers 404.
- [ ] A device (or devsim from another network) connects; in Settings → Devices its
      **Network** shows the gym's public IP, not `172.30.0.1`. If it shows the Docker gateway,
      Docker is not preserving client IPs on this host: run Caddy with `network_mode: host`.
- [ ] The live feed updates on a punch.
- [ ] Backup and **restore** once (below) before real gyms go live.

## Everyday operations

All commands run in `/opt/najah`; `C="docker compose --env-file .env -f docker-compose.yml"`.

| Task | Command |
|---|---|
| Status | `$C ps` |
| Logs | `$C logs -f --tail 100 api` (or `caddy`, `postgres`) |
| Deploy a version | merge to `main` (CI), or set the `*_IMAGE` tags in `.env` and `$C up -d --wait` |
| Roll back | set `API_IMAGE` / `PORTAL_IMAGE` to the previous commit's tag, `$C up -d --wait` |
| Device protocol debugging | `ADMS_LOG_HEARTBEATS=true` in `.env`, `$C up -d api`, read `device_raw_messages` |
| Database shell | `$C exec postgres psql -U najah -d najah` |

Rolling back the code does not roll back migrations. Migrations are written to be additive;
a migration that needs undoing gets its own down step, run deliberately.

## Backups

`deploy/backup.sh` writes a compressed `pg_dump` to `deploy/backups/` and keeps the newest 30.
Run it nightly and copy the files off the VM:

```cron
30 20 * * * cd /opt/najah && ./backup.sh && rclone copy backups r2:najah-backups --max-age 48h
```

(20:30 UTC is 02:00 IST. `rclone config` with a Cloudflare R2 bucket; R2's free tier is 10 GB.)

`deploy/restore.sh <dump>` stops the api, replaces the database with the dump and starts the api
again. Locally, a backup → `down -v` (stack and volumes deleted) → fresh `up` → restore brought
back every row and the owner could sign in to the same data.

## Moving to a paid server

Same compose file. Copy `/opt/najah` (including `.env` and the latest dump), start the stack,
restore the dump, then point the two DNS records at the new IP. Devices keep working because
they use the hostname, not the IP.
