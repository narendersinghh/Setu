# Setu v2 – operations guide

## 1. First deploy (one VPS, Ubuntu 24.04, 4 vCPU / 8 GB)

```bash
# server hardening
apt update && apt -y upgrade && apt -y install ufw unattended-upgrades docker.io docker-compose-v2
ufw default deny incoming && ufw allow 22/tcp && ufw allow 80/tcp && ufw allow 443/tcp && ufw enable
# SSH: key login only (PasswordAuthentication no in /etc/ssh/sshd_config)

git clone https://github.com/narendersinghh/setu.git /opt/setu && cd /opt/setu/deploy
cp .env.example .env   # fill SETU_APP_HOST, ACME_EMAIL, SETU_MASTER_KEY, DB passwords
docker compose up -d --build
docker compose exec app setu platform-admin you@yourdomain.com   # after you sign up
```

DNS: an `A` record for `app.yourdomain.com` → VPS IP (Cloudflare proxied is fine). Caddy obtains the
certificate automatically. Everyone signs up and signs in at that one address.

**Keep `SETU_MASTER_KEY` safe** (password manager + offline copy). It encrypts every workspace's API keys.

## 2. Updates and rollback

```bash
cd /opt/setu && git pull && cd deploy && docker compose up -d --build   # migrations run automatically
# rollback: git checkout <previous tag> && docker compose up -d --build
```
Migrations are additive; take a backup before upgrades that the release notes mark as "schema change".

## 3. Moving v1 installs into v2

On each v1 server: `php tools/export-v2.php > export.jsonl` (contains decrypted keys – copy it securely).
On v2: `docker compose cp export.jsonl app:/tmp/ && docker compose exec app setu import-v1 /tmp/export.jsonl "Company name"`,
then delete the file. Users keep their email and password. Re-connect Google Calendar and update webhook URLs
(Meta/WhatsApp/forms) to the new address shown in Settings.

## 4. Backups

* Nightly: `deploy/backup.sh` from cron (`15 2 * * * /opt/setu/deploy/backup.sh`) and copy `deploy/backups` plus the
  `setu_data` volume (uploads, PDFs) to a second server with `rclone`/`rsync`.
* Larger installs: pgBackRest with WAL archiving for point-in-time recovery.
* Test a restore every month: `pg_restore -d setu_restore backups/<file>.dump`.

## 5. Monitoring

* Uptime Kuma → `https://app.yourdomain.com/healthz` (checks the database).
* Beszel or Netdata for CPU/RAM/disk. Postgres logs slow queries (> 500 ms) by default.
* Failed background jobs: `SELECT type, count(*), max(error) FROM jobs WHERE status='failed' GROUP BY 1;`
* Periodic tasks: `SELECT * FROM schedules;` (last_ok / last_error).

## 6. Scaling

| Load | Change |
|---|---|
| CPU busy with AI/audit jobs | `docker compose up -d --scale worker=3` (workers share the queue safely) |
| Web traffic | run `app` on 2+ servers behind Caddy/load balancer – the app keeps no local state except files (move `/data` to SeaweedFS/Garage S3 first) |
| Database | move Postgres to its own VPS, add a streaming replica, PgBouncer in transaction mode |
| Lakhs of organisations | Citus: `SELECT create_distributed_table(t, 'org_id')` for tenant tables, or pods via `orgs.shard` |

`SETU_ORG_JOB_CAP` limits how many jobs one organisation can run at once so heavy users never block others.

## 7. Security checklist

* App connects as `setu_app` (no superuser, no BYPASSRLS) – every tenant table is protected by Row-Level Security.
* Only ports 22/80/443 open; Postgres is not exposed.
* Secrets: workspace API keys encrypted (AES-256-GCM, per-workspace key derived from `SETU_MASTER_KEY`).
* Sessions: HttpOnly + Secure + SameSite=Lax cookie, CSRF header on every change, login throttling.
* Outbound requests to user-supplied URLs are blocked from reaching private/internal addresses.
