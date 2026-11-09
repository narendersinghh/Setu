# Installing Setu on a Hostinger VPS (step by step)

This guide takes you from "I bought a VPS" to "my CRM runs at `https://app.mybrand.com`", without needing
to be a Linux expert. One command installs everything. You can run **several CRM brands on one VPS**
(for example two white-labelled products), each with its own domain, database, branding and plans.

What gets installed (once per VPS): PostgreSQL 17, Caddy (automatic HTTPS), Gotenberg (PDF rendering,
in Docker), Postfix (outgoing email), firewall, automatic security updates, a swap file on small plans.
Per CRM brand ("instance"): its own system user, `/opt/<instance>`, database, settings file, port,
systemd service, HTTPS site and nightly backups.

---

## 1. Choose the VPS plan

| Plan | vCPU / RAM | Good for |
|---|---|---|
| KVM 1 | 1 / 4 GB | testing only |
| **KVM 2** (minimum) | 2 / 8 GB | one brand, up to a few hundred organisations |
| **KVM 4** (recommended) | 4 / 16 GB | two brands, many organisations, heavy AI/report jobs |
| KVM 8 | 8 / 32 GB | large installs |

Disk is rarely the limit (uploads, PDFs and backups live on the VPS disk). Choose the data-centre
closest to your customers (e.g. India → Mumbai).

When asked for the **operating system**, choose **Ubuntu 24.04** – the plain OS template, *not* a
"with panel" or "with Docker/Coolify" template. The installer refuses other systems.

## 2. Set the root password / SSH key (hPanel)

1. hPanel → **VPS** → your server → **Settings** (or *Overview*).
2. Set a strong **root password** (save it in your password manager).
3. Optional but recommended: **SSH keys** → add your public key (`~/.ssh/id_ed25519.pub`). Then you can
   log in without a password.
4. Note the server's **IP address** (shown on the VPS overview page).

## 3. Point your domain at the VPS

Create an **A record** for the address customers will use, e.g. `app.mybrand.com` → `<VPS IP>`.

* **Domain at Hostinger:** hPanel → **Domains** → your domain → **DNS / Nameservers** → *Manage DNS
  records* → add `A`, name `app`, points to `<VPS IP>`, TTL 300.
* **Domain at Cloudflare:** DNS → *Add record* → `A`, name `app`, IPv4 `<VPS IP>`. Start with the cloud
  icon **grey (DNS only)** so the certificate can be issued; you can switch it to orange (proxied) later
  – then set SSL/TLS mode to **Full (strict)**.

Check it: `ping app.mybrand.com` should show the VPS IP (can take a few minutes).

## 4. Connect to the VPS

* **Easiest:** hPanel → VPS → **Browser terminal** (a black window opens, you are logged in as root).
* **From your computer:** `ssh root@<VPS IP>` (Windows: PowerShell or PuTTY).

## 5. Run the installer

You need one thing ready: a **GitHub token** (the Setu code is in a private repository).
Create it once at github.com → your photo → **Settings → Developer settings → Personal access tokens →
Fine-grained tokens → Generate new token**: Repository access *Only select repositories* → `setu`,
Permissions → *Contents: Read-only*, expiry 1 year. Copy it (it starts with `github_pat_`).
Never paste the token into chats or emails – only into the installer and the Platform console.

Now copy this **one line**, paste it into the terminal (right-click → Paste) and press **Enter**:

```bash
bash -c 'read -rsp "Paste your GitHub token and press Enter: " T </dev/tty; echo; curl -fsSL -H "Authorization: Bearer $T" https://raw.githubusercontent.com/narendersinghh/setu/main/deploy/install.sh -o /tmp/setu-install.sh && sudo SETU_GITHUB_TOKEN="$T" bash /tmp/setu-install.sh || echo "Download failed – check the token and try again."'
```

**Token prompt does not accept a paste?** (some browser terminals block pasting into hidden prompts) –
put the token inside the command instead: paste the line below into Notepad, replace `PASTE_TOKEN_HERE`
with your token (no spaces), copy the whole line and paste it into the terminal:

```bash
T=PASTE_TOKEN_HERE; curl -fsSL -H "Authorization: Bearer $T" https://raw.githubusercontent.com/narendersinghh/setu/main/deploy/install.sh -o /tmp/setu-install.sh && sudo SETU_GITHUB_TOKEN="$T" bash /tmp/setu-install.sh; history -c
```

`history -c` removes the line (and the token) from the terminal history afterwards.

The installer then asks a few questions – just type the answer and press Enter:

| Question | What to type |
|---|---|
| 1. Web address of your CRM | e.g. `crm.yourcompany.com` (the domain from step 3) |
| 2. Your email | for the SSL certificate and alerts |
| 3. Email for your platform-admin login | the email **you** will log in with (not a customer's) |
| 4. Admin password | 10+ characters, typed twice (nothing shows while typing – that is normal) |
| 5. Which version | press Enter (latest build) |
| Start the installation? | press Enter |

(Experts can still pass everything as options: `--domain`, `--email`, `--admin-email`, `--admin-password`,
`--channel`, `--token`, `--instance` – see `install.sh --help`.)

It takes 3–8 minutes. At the end you see a summary like:

```
  URL              https://app.mybrand.com
  Platform admin   you@mybrand.com / Xy7…   (generated – change it after the first login)
  Env file         /etc/setu/setu.env  (mode 600 – contains SETU_MASTER_KEY and DB passwords)
  …
```

**Copy the `SETU_MASTER_KEY` to your password manager now:**

```bash
sudo grep SETU_MASTER_KEY /etc/setu/setu.env
```

It encrypts every API key and password your customers save. If it is lost, those secrets cannot be
recovered; if it is changed, they stop working. Never change it.

Running the installer again is safe: it only adds what is missing and never replaces the master key or
database passwords.

### n8n (or another Docker app) already uses ports 80/443 through Traefik?

Hostinger's n8n / Docker Manager templates run **Traefik**, which holds ports 80/443, so Caddy cannot
start. Let Caddy serve every site instead (n8n keeps its address and data). With your token in place of
`PASTE_TOKEN_HERE` (Notepad method from step 5):

```bash
T=PASTE_TOKEN_HERE; curl -fsSL -H "Authorization: Bearer $T" https://raw.githubusercontent.com/narendersinghh/setu/main/deploy/caddy-takeover.sh -o /tmp/caddy-takeover.sh && sudo bash /tmp/caddy-takeover.sh; history -c
```

It lists the sites it will move, asks for **y**, switches, and checks each address. Undo any time:
`sudo bash /tmp/caddy-takeover.sh --undo`. If you later **re-deploy the Traefik project in hPanel's Docker
Manager**, it takes the ports back – run the script again.

## 6. First login as platform admin

1. Open your CRM address (e.g. `https://crm.yourcompany.com`) and sign in with the admin email and the password you chose.
2. You land in the **Platform console** (`/admin`). Change your password (account menu).
3. Optional: remove `SETU_ADMIN_PASSWORD` from `/etc/setu/setu.env` afterwards
   (`sudo nano /etc/setu/setu.env`, then `sudo systemctl restart setu`).

Didn't pass `--admin-email`? Sign up normally on the site, then make yourself platform admin:
`sudo setu-cli platform-admin you@mybrand.com`.

## 7. Set up your brand, plans and email

In the Platform console:

* **Branding** – name, tagline, logos (light and dark background), favicon, fonts, colours, corner radius,
  default theme. This is what your customers see; "Setu" never appears if you change it.
* **Plans** – create plans with prices, trial days, limits (users, leads, automations, …) and modules
  (CRM, Prospecting). Mark which are public (shown on the sign-up page) and the default plan.
* **Email** – the default mailer for all organisations:
  * **sendmail** – uses the local Postfix; works immediately but needs DNS records (section 12).
  * **SMTP** – e.g. a Hostinger mailbox (`smtp.hostinger.com`, port 465 SSL, the mailbox address and
    password). Best deliverability with the least effort.
  Organisations can still add their own SMTP / email service in their Settings → Email.
* **Organisations** – see sign-ups, change plan/status, override modules and limits, "Open workspace"
  for support.

## 8. A second CRM brand on the same VPS

Each brand is an **instance** with its own domain, database, users, branding and plans. Point the second
domain at the same VPS IP (section 3), then paste the same line as in step 5 with a new instance name at
the end (`crm2` here) and answer the questions with the second brand's address and admin email:

```bash
bash -c 'read -rsp "Paste your GitHub token and press Enter: " T </dev/tty; echo; curl -fsSL -H "Authorization: Bearer $T" https://raw.githubusercontent.com/narendersinghh/setu/main/deploy/install.sh -o /tmp/setu-install.sh && sudo SETU_GITHUB_TOKEN="$T" bash /tmp/setu-install.sh --instance crm2 || echo "Download failed – check the token and try again."'
```

The second instance gets the next free port (8081), `/opt/crm2`, `/etc/crm2/setu.env` (its **own**
master key – back it up too), database `crm2`, service `crm2`, backups in `/var/backups/crm2`.
Instance names: lowercase letters, digits and `_` (e.g. `setu`, `crm2`, `acme`).

Use `crm2` wherever this guide says `setu` for commands about the second brand
(`systemctl status crm2`, `journalctl -u crm2 -f`, `sudo setu-update --instance crm2`, `sudo crm2-cli …`).

Sizing: each instance uses ~100–300 MB RAM plus its share of PostgreSQL; Gotenberg (1 GB max) is shared.

## 9. Releases on GitHub (where the installer gets the program)

The installer and the updater download ready-made binaries from the repository's **GitHub Releases**,
built by `.github/workflows/release.yml`:

* Every push to `main` refreshes the **`edge`** pre-release (channel `edge`).
* Every tag `v*` creates a normal release (channel `stable` = the latest one).

Make your first stable release (on your computer, in the repository):

```bash
git tag v2.0.0
git push origin v2.0.0
```

Wait until the "release" action is green (GitHub → *Actions*), then check *Releases* shows
`setu-linux-amd64`, `setu-linux-arm64` and the `.sha256` files. Until a `v*` tag exists, install with
`--channel edge`.

Private repository: Settings → Actions → General → *Workflow permissions* must allow **read and write**
(the workflow publishes releases). Servers need `--token` to download.

## 10. Updating

* **From the app (recommended):** Platform console → **Updates** → *Check* → **Update**. The program
  downloads the new binary, checks its checksum, replaces itself and restarts; database migrations run
  automatically. Switch between `stable` and `edge` there.
* **From the terminal:** `sudo setu-update --instance setu`
  (`--check` only checks, `--channel edge|stable` switches channel, `--release v2.0.1` installs/pins an exact
  version, `--rollback` returns to the previous binary). It takes a database backup first and restores the
  previous binary automatically if the new one does not start.
* Re-running `install.sh` also updates to the latest release of the instance's channel.

## 11. Backups

* Every night (02:xx) the database (`pg_dump`) and uploaded files of each instance are saved in
  `/var/backups/<instance>/`; 14 days are kept. Log: `/var/log/setu-backup-<instance>.log`.
* Backup now: `sudo setu-backup setu`.
* **Copy backups off the VPS** – a VPS backup on the same VPS does not protect against losing the VPS.
  Turn on Hostinger's weekly VPS backups/snapshots in hPanel, and/or copy `/var/backups` daily to another
  server or cloud storage with `rclone`. Keep the env file (master key) in your password manager.
* Restore (example for instance `setu`):

```bash
sudo systemctl stop setu
sudo setu-backup setu                                   # safety copy of the current state first
sudo -u postgres dropdb setu
sudo -u postgres createdb -O setu_owner setu
sudo -u postgres pg_restore -d setu /var/backups/setu/setu-db-YYYYMMDD-HHMM.dump
sudo -u postgres psql -c 'REVOKE ALL ON DATABASE setu FROM PUBLIC; GRANT CONNECT, TEMPORARY ON DATABASE setu TO setu_app'
sudo tar -C /opt/setu -xzf /var/backups/setu/setu-files-YYYYMMDD-HHMM.tar.gz
sudo systemctl start setu
```

A restore only works with the **same `SETU_MASTER_KEY`** that was in use when the backup was taken.
To move to a new VPS: run the installer there, copy the old `/etc/setu/setu.env` over the new one (keeps the
master key and DB passwords), re-run the installer (it re-applies the DB passwords), then restore as above.

## 12. Email deliverability

With the default **sendmail** mailer, mail leaves directly from your VPS. Without these DNS records it
lands in spam:

| Record | Where | Value |
|---|---|---|
| SPF | TXT on the sending domain (`@`) | `v=spf1 ip4:<VPS IP> ~all` (if you already have SPF, e.g. for Hostinger mail, add `ip4:<VPS IP>` to it – only **one** SPF record per domain) |
| DMARC | TXT `_dmarc` | `v=DMARC1; p=none; rua=mailto:you@mybrand.com` |
| PTR (reverse DNS) | hPanel → VPS → Settings → *PTR record* | your domain, e.g. `app.mybrand.com` |
| DKIM | needs a signing service (e.g. OpenDKIM) on the VPS | advanced – see below |

Simplest reliable alternative: create a mailbox (Hostinger Email or any provider), and in
Platform console → **Email** choose **SMTP** with that mailbox. The provider then handles SPF/DKIM.
For high volume use a transactional service (Amazon SES, Brevo, Postmark, …) via SMTP.

Some providers block outgoing port 25. If mail from `sendmail` never arrives, check
`sudo tail -50 /var/log/mail.log` and switch to SMTP.

## 13. Logs and everyday commands

```bash
systemctl status setu                 # running?
journalctl -u setu -f                 # live log (Ctrl+C to stop)
journalctl -u setu --since "1 hour ago"
sudo systemctl restart setu
journalctl -u caddy -n 100            # HTTPS / certificate problems
sudo docker logs --tail 50 gotenberg  # PDF problems
sudo setu-cli platform-admin you@mybrand.com
```

Settings live in `/etc/<instance>/setu.env` (restart the service after editing).

## 14. Firewall

The installer enables `ufw` and allows only SSH (22), HTTP (80) and HTTPS (443). PostgreSQL, the app
ports and Gotenberg listen on `127.0.0.1` only.

Hostinger also has its own **VPS firewall** in hPanel (VPS → *Security* → *Firewall*). If you enable
it, add rules that **accept TCP 80 and 443** (and 22 for SSH), or certificates cannot be issued and the site is
unreachable.

## 15. Troubleshooting

| Symptom | Fix |
|---|---|
| `no stable release found` | Create a tag `v2.0.0` (section 9) or add `--channel edge` |
| `release … not found` with a private repo | Add `--token <GitHub token>` |
| Installer: "this installer supports Ubuntu 24.04" | Reinstall the VPS with the plain *Ubuntu 24.04* template (hPanel → VPS → OS & Panel → Operating system) |
| Browser shows a certificate error / site not reachable | DNS A record wrong or not propagated (`ping app.mybrand.com`); hPanel firewall blocks 80/443; Cloudflare orange cloud before the certificate was issued (switch to grey, wait, then back). `journalctl -u caddy -n 100` |
| `did not become healthy` | `journalctl -u setu -n 100` – usually a database or env setting; fix and `sudo systemctl restart setu` |
| PDFs fail | `sudo docker ps` should show `gotenberg`; `sudo docker start gotenberg` |
| Emails in spam / not delivered | Section 12; `sudo tail -50 /var/log/mail.log` |
| Out of memory | Upgrade the plan (KVM 4), or `free -h` / `htop` to see what is using RAM |
| Update broke something | `sudo setu-update --instance setu --rollback` |
| Remove a brand | `sudo setu-uninstall --instance crm2` (keeps a final backup + env file in `/var/backups/crm2`; `--purge` deletes them too) |
