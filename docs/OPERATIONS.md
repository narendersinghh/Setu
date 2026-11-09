# Setu v2 – operations guide

## 1. First deploy (one VPS, Ubuntu 24.04)

There are two ways to run Setu. **Step-by-step guide for Hostinger (and any Ubuntu 24.04 VPS):
[HOSTINGER.md](HOSTINGER.md).**

| | A. systemd installer (**recommended**) | B. Docker Compose (alternative) |
|---|---|---|
| Command | `curl -fsSL https://raw.githubusercontent.com/narendersinghh/setu/main/deploy/install.sh \| sudo bash -s -- --domain app.example.com --email you@example.com` | `docker compose up -d --build` in `deploy/` |
| Program | prebuilt binary from GitHub Releases (`release.yml`) | image built from the source checkout |
| In-app updates (Platform → Updates) | **yes** – the binary replaces itself, systemd restarts it | **no** – update with `git pull && docker compose up -d --build` |
| Several brands on one VPS | yes: `--instance crm2 --domain app.other.com` | one stack per brand (copy `deploy/`, change ports) |
| PostgreSQL / Caddy | native packages (PGDG 17, Caddy apt repo) | containers |

### A. systemd installer

```bash
curl -fsSL https://raw.githubusercontent.com/narendersinghh/setu/main/deploy/install.sh | sudo bash -s -- \
  --domain app.yourdomain.com --email you@yourdomain.com --admin-email you@yourdomain.com [--channel edge] [--token GH_TOKEN]
```

Per instance: user + `/opt/<instance>/{bin,data}`, env file `/etc/<instance>/setu.env` (mode 600, holds
`SETU_MASTER_KEY`), PostgreSQL database `<instance>` with roles `<instance>_owner` (migrations) and
`<instance>_app` (NOSUPERUSER NOBYPASSRLS), port from 8080 up on 127.0.0.1, unit `<instance>.service`
(`setu all`), Caddy site `/etc/caddy/Caddyfile.d/<instance>.caddy`, nightly backup cron. Shared: PostgreSQL 17,
Caddy, one Gotenberg container (127.0.0.1:3000), send-only Postfix, ufw, unattended-upgrades, swap (< 4 GB RAM).
Helpers: `setu-update --instance X`, `setu-backup X`, `setu-uninstall --instance X`, `X-cli <command>`.
Re-running the installer is safe (never replaces the master key or DB passwords). `--dry-run` shows what it would do.

### B. Docker Compose

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

**systemd install (A):** Platform console → Updates (stable | edge channel), or
`sudo setu-update --instance setu` (`--release vX.Y.Z` pins a version, `--rollback` restores the previous
binary). Releases come from `.github/workflows/release.yml` (push to `main` → `edge`, tag `v*` → stable).

**Docker Compose (B):**

```bash
cd /opt/setu && git pull && cd deploy && docker compose up -d --build   # migrations run automatically
# rollback: git checkout <previous tag> && docker compose up -d --build
```
Migrations are additive; take a backup before upgrades that the release notes mark as "schema change".

## 3. Moving v1 installs into v2

On each v1 server: `php tools/export-v2.php > export.jsonl` (contains decrypted keys – copy it securely).
On v2 (systemd install): `sudo install -o setu -m 600 export.jsonl /opt/setu/data/tmp/ && sudo setu-cli import-v1 /opt/setu/data/tmp/export.jsonl "Company name"`;
Docker Compose: `docker compose cp export.jsonl app:/tmp/ && docker compose exec app setu import-v1 /tmp/export.jsonl "Company name"`,
then delete the file. Users keep their email and password. Re-connect Google Calendar and update webhook URLs
(Meta/WhatsApp/forms) to the new address shown in Settings.

## 4. Backups

* systemd install (A): `/etc/cron.d/<instance>-backup` runs `setu-backup <instance>` nightly (database dump +
  files, 14 days in `/var/backups/<instance>`); restore steps in [HOSTINGER.md](HOSTINGER.md#11-backups).
* Docker Compose (B): nightly `deploy/backup.sh` from cron (`15 2 * * * /opt/setu/deploy/backup.sh`) and copy `deploy/backups` plus the
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
