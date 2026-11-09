#!/usr/bin/env bash
# Setu v2 one-command installer for a fresh Ubuntu 24.04 VPS (Hostinger "Ubuntu 24.04" template).
#
#   Easiest (asks every question, incl. the GitHub token): see docs/HOSTINGER.md step 5.
#   curl -fsSL https://raw.githubusercontent.com/narendersinghh/setu/main/deploy/install.sh \
#     | sudo bash -s -- --domain app.example.com --email you@example.com
#
# Options:
#   --domain HOST          public address of this CRM (required on first install of an instance)
#   --email EMAIL          contact for Let's Encrypt certificates (required on first install of the VPS)
#   --instance NAME        instance name (default: setu). One instance = one brand: own user, database,
#                          port, domain, env file and systemd service. Run again with another name and
#                          domain to host a second white-labelled CRM on the same VPS.
#   --channel stable|edge  release channel (stable = latest v* release, edge = latest main build)
#   --release TAG          install this exact release tag instead (e.g. v2.0.1)
#   --admin-email EMAIL    platform admin to create at start-up (SETU_ADMIN_EMAIL)
#   --admin-password PASS  password for it (generated when --admin-email is given without one)
#   --repo OWNER/NAME      GitHub repository with the releases (default: narendersinghh/setu)
#   --token TOKEN          GitHub token (only for private repositories; also used by in-app updates)
#   --port N               local port for this instance (default: keep existing, else first free from 8080)
#   --force                continue on an untested OS version
#   --dry-run              change nothing: print system commands, write generated files under a temp
#                          directory (or --root DIR) so they can be inspected
#
# Safe to run again: it only adds what is missing, never replaces SETU_MASTER_KEY or DB passwords,
# and re-running is also a way to upgrade the binary / change the channel.

main() {
set -euo pipefail
umask 022

# ------------------------------------------------------------------ defaults / args
DOMAIN="" ACME_EMAIL="" INSTANCE="setu" CHANNEL="" RELEASE="" ADMIN_EMAIL="" ADMIN_PASSWORD=""
REPO="" TOKEN="" PORT="" FORCE=0 DRY_RUN=0 ROOT=""
DEFAULT_REPO="narendersinghh/setu"
SCRIPTS_REF="main"
GOTENBERG_IMAGE="gotenberg/gotenberg:8"
STATE_DIR="/var/lib/setu-installer"

while [[ $# -gt 0 ]]; do
	case "$1" in
	--domain) DOMAIN="${2:-}"; shift 2 ;;
	--email) ACME_EMAIL="${2:-}"; shift 2 ;;
	--instance) INSTANCE="${2:-}"; shift 2 ;;
	--channel) CHANNEL="${2:-}"; shift 2 ;;
	--release) RELEASE="${2:-}"; shift 2 ;;
	--admin-email) ADMIN_EMAIL="${2:-}"; shift 2 ;;
	--admin-password) ADMIN_PASSWORD="${2:-}"; shift 2 ;;
	--repo) REPO="${2:-}"; shift 2 ;;
	--token) TOKEN="${2:-}"; shift 2 ;;
	--port) PORT="${2:-}"; shift 2 ;;
	--force) FORCE=1; shift ;;
	--dry-run) DRY_RUN=1; shift ;;
	--root) ROOT="${2:-}"; shift 2 ;;
	-h | --help) sed -n '2,30p' "${BASH_SOURCE[0]:-/dev/null}" 2>/dev/null || true; return 0 ;;
	*) die "unknown option: $1 (see --help)" ;;
	esac
done

if [[ $DRY_RUN == 1 ]]; then
	[[ -n $ROOT ]] || ROOT=$(mktemp -d "${TMPDIR:-/tmp}/setu-dry-run.XXXXXX")
	ROOT=${ROOT%/}
	mkdir -p "$ROOT"
	warn "DRY RUN – nothing on this machine is changed. Generated files go to $ROOT"
elif [[ -n $ROOT ]]; then
	die "--root is only allowed together with --dry-run"
fi

# ------------------------------------------------------------------ validation
[[ $INSTANCE =~ ^[a-z][a-z0-9_]{1,30}$ ]] || die "--instance must be 2-31 chars: lowercase letters, digits, _ (start with a letter)"
case "$INSTANCE" in
root | postgres | caddy | docker | www | www_data | daemon | nobody | postfix | systemd | ubuntu | admin | backup | mail | cron | ssh | sshd | snap | lxd)
	die "--instance '$INSTANCE' is reserved, choose another name (e.g. setu, crm2, acme)" ;;
esac
ENV_FILE="/etc/$INSTANCE/setu.env"
APP_DIR="/opt/$INSTANCE"
SERVICE="$INSTANCE.service"
DB_NAME="$INSTANCE" DB_OWNER="${INSTANCE}_owner" DB_APP="${INSTANCE}_app"
EXISTING=0
[[ -f "$ROOT$ENV_FILE" ]] && EXISTING=1

# values from a previous run (args win, then the existing env file, then defaults)
[[ -n $DOMAIN ]] || DOMAIN=$(env_get SETU_APP_HOST)
[[ -n $CHANNEL ]] || CHANNEL=$(env_get SETU_UPDATE_CHANNEL)
[[ -n $CHANNEL ]] || CHANNEL=stable
[[ -n $REPO ]] || REPO=$(env_get SETU_UPDATE_REPO)
[[ -n $REPO ]] || REPO=$DEFAULT_REPO
[[ -n $TOKEN ]] || TOKEN=$(env_get SETU_UPDATE_TOKEN)
[[ -n $TOKEN ]] || TOKEN=${SETU_GITHUB_TOKEN:-}

# Guided set-up: ask for anything that was not given on the command line (needs a terminal)
if [[ $DRY_RUN == 0 && -z ${SETU_NONINTERACTIVE:-} ]] && { : </dev/tty; } 2>/dev/null; then
	wizard
fi

DOMAIN=$(tr '[:upper:]' '[:lower:]' <<<"$DOMAIN")
[[ -n $DOMAIN ]] || die "--domain is required (the address of this CRM, e.g. app.example.com)"
[[ $DOMAIN =~ ^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}$ ]] || die "--domain '$DOMAIN' is not a valid host name"
[[ $CHANNEL == stable || $CHANNEL == edge ]] || die "--channel must be stable or edge"
[[ $REPO =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || die "--repo must look like owner/name"
[[ -z $ACME_EMAIL || $ACME_EMAIL =~ ^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$ ]] || die "--email is not an email address"
[[ -z $ADMIN_EMAIL || $ADMIN_EMAIL =~ ^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$ ]] || die "--admin-email is not an email address"
[[ $ADMIN_PASSWORD != *[\'\"\\]* && $ADMIN_PASSWORD != *$'\n'* ]] || die "--admin-password must not contain quotes, backslashes or newlines"
[[ -z $ADMIN_PASSWORD || ${#ADMIN_PASSWORD} -ge 8 ]] || die "--admin-password must be at least 8 characters"
[[ -z $ADMIN_PASSWORD || -n $ADMIN_EMAIL ]] || die "--admin-password needs --admin-email"
[[ -z $PORT || ($PORT =~ ^[0-9]+$ && $PORT -ge 1024 && $PORT -le 65000) ]] || die "--port must be a number between 1024 and 65000"
[[ -z $TOKEN || $TOKEN =~ ^[A-Za-z0-9_]+$ ]] || die "--token looks wrong (expected a GitHub token like ghp_… or github_pat_…)"

preflight

case "$(uname -m)" in
x86_64 | amd64) ARCH=amd64 ;;
aarch64 | arm64) ARCH=arm64 ;;
*) die "unsupported CPU architecture $(uname -m) (need x86_64 or aarch64)" ;;
esac
ASSET="setu-linux-$ARCH"

step "Setu installer – instance '$INSTANCE' → https://$DOMAIN ($CHANNEL channel, $ARCH)"
[[ $EXISTING == 1 ]] && info "existing instance found ($ENV_FILE) – updating it in place"

base_system
setup_swap
setup_firewall
setup_postgres
setup_caddy
setup_gotenberg
setup_postfix

step "Instance '$INSTANCE'"
create_user_and_dirs
pick_port
write_env
setup_database
install_binary
install_helpers
write_unit
write_caddy_site
write_backup_cron
start_and_check
summary
}

# ====================================================================== guided set-up
ask() { # ask VAR "Question" [default]
	local __v="" __q="$2" __d="${3:-}"
	[[ -n $__d ]] && __q="$__q [$__d]"
	read -r -p "    $__q: " __v </dev/tty || true
	printf -v "$1" '%s' "${__v:-$__d}"
}
ask_secret() { # ask_secret VAR "Question"
	local __v=""
	read -r -s -p "    $2: " __v </dev/tty || true
	printf '\n' >/dev/tty
	printf -v "$1" '%s' "$__v"
}
wizard() {
	local need=0
	[[ -z $DOMAIN || -z $ACME_EMAIL || ( $EXISTING == 0 && -z $ADMIN_EMAIL ) ]] && need=1
	[[ $need == 0 && -n $TOKEN ]] && return 0
	printf '\n%s Setu set-up – answer a few questions (press Enter to accept the value in [brackets])%s\n\n' "$c_bold" "$c_off"
	while [[ -z $DOMAIN || ! $DOMAIN =~ ^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}$ ]]; do
		ask DOMAIN "1. Web address of your CRM (e.g. crm.yourcompany.com – its DNS must point to this server)"
		DOMAIN=$(tr '[:upper:]' '[:lower:]' <<<"$DOMAIN"); DOMAIN=${DOMAIN#http://}; DOMAIN=${DOMAIN#https://}; DOMAIN=${DOMAIN%%/*}
	done
	while [[ ! $ACME_EMAIL =~ ^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$ ]]; do
		ask ACME_EMAIL "2. Your email (for the SSL certificate and alerts)"
	done
	if [[ $EXISTING == 0 && -z $ADMIN_EMAIL ]]; then
		ask ADMIN_EMAIL "3. Email for YOUR platform-admin login (use a different one from customer accounts)" "$ACME_EMAIL"
		while :; do
			local p2=""
			ask_secret ADMIN_PASSWORD "4. Choose the admin password (10+ characters, typing is hidden)"
			ask_secret p2 "   Type the password again"
			if [[ ${#ADMIN_PASSWORD} -lt 10 ]]; then warn "Too short – use at least 10 characters."
			elif [[ $ADMIN_PASSWORD != "$p2" ]]; then warn "The two passwords are different – try again."
			elif [[ $ADMIN_PASSWORD == *[\'\"\\]* ]]; then warn "Please do not use quotes or backslashes."
			else break; fi
		done
	fi
	if [[ -z $TOKEN ]]; then
		info "5. GitHub token – needed because the Setu repository is private."
		info "   Create it at github.com → Settings → Developer settings → Fine-grained tokens"
		info "   (Repository access: only 'setu' · Permissions: Contents = Read-only)."
		while [[ -z $TOKEN || ! $TOKEN =~ ^[A-Za-z0-9_]+$ ]]; do
			ask_secret TOKEN "   Paste the token (typing is hidden; Enter to skip if the repository is public)"
			[[ -z $TOKEN ]] && break
			[[ $TOKEN =~ ^[A-Za-z0-9_]+$ ]] || warn "That does not look like a GitHub token – paste it again."
		done
	fi
	if [[ -z $RELEASE && $EXISTING == 0 ]]; then
		local v=""
		ask v "6. Which version? 1 = latest build (recommended for now), 2 = stable releases only" "1"
		[[ $v == 2 ]] && CHANNEL=stable || CHANNEL=edge
	fi
	printf '\n    %sSummary%s\n    Address:  https://%s\n    Admin:    %s\n    Version:  %s\n    Token:    %s\n\n' "$c_bold" "$c_off" "$DOMAIN" "${ADMIN_EMAIL:-(unchanged)}" "$CHANNEL" "$([[ -n $TOKEN ]] && echo saved || echo none)"
	local ok=""
	ask ok "Start the installation? (y/n)" "y"
	[[ $ok =~ ^[Yy] ]] || die "Cancelled – nothing was changed."
}

# ====================================================================== helpers
c_bold=$'\e[1m' c_red=$'\e[31m' c_yel=$'\e[33m' c_grn=$'\e[32m' c_off=$'\e[0m'
[[ -t 1 ]] || { c_bold="" c_red="" c_yel="" c_grn="" c_off=""; }
step() { printf '\n%s==> %s%s\n' "$c_bold" "$*" "$c_off"; }
info() { printf '    %s\n' "$*"; }
ok() { printf '    %s✓%s %s\n' "$c_grn" "$c_off" "$*"; }
warn() { printf '%s!!  %s%s\n' "$c_yel" "$*" "$c_off" >&2; }
die() { printf '%sERROR: %s%s\n' "$c_red" "$*" "$c_off" >&2; exit 1; }

# run CMD… – executes, or prints it in dry-run mode
run() {
	if [[ $DRY_RUN == 1 ]]; then
		printf '    [dry-run]'; printf ' %q' "$@"; printf '\n'
	else
		"$@"
	fi
}

apt_install() {
	run env DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a NEEDRESTART_SUSPEND=1 \
		apt-get install -y -q -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold "$@" </dev/null
}

# write_file PATH MODE OWNER  (content on stdin). Returns 0 when the file changed, 1 when identical.
write_file() {
	local path="$ROOT$1" mode=$2 owner=$3 tmp
	mkdir -p "$(dirname "$path")"
	tmp=$(mktemp "$path.XXXXXX")
	cat >"$tmp"
	if [[ -f $path ]] && cmp -s "$tmp" "$path"; then
		rm -f "$tmp"
		return 1
	fi
	chmod "$mode" "$tmp"
	[[ $DRY_RUN == 1 ]] || chown "$owner" "$tmp"
	mv -f "$tmp" "$path"
	return 0
}

# env_get KEY – value from the instance env file (quotes stripped), empty when missing
env_get() {
	local f="${ENV_WORK:-$ROOT/etc/$INSTANCE/setu.env}" v
	[[ -f $f ]] || return 0
	v=$(grep -E "^$1=" "$f" | tail -n1 | cut -d= -f2-) || true
	v=${v#\'} v=${v%\'} v=${v#\"} v=${v%\"}
	printf '%s' "$v"
}

# quote a value for a file read by both systemd (EnvironmentFile) and bash (source)
q() {
	if [[ $1 =~ ^[A-Za-z0-9_./:@%+=,-]*$ ]]; then printf '%s' "$1"; else printf "'%s'" "$1"; fi
}

# env_set KEY VALUE – replace or append in the working copy of the env file
env_set() {
	K=$1 V=$(q "$2") awk 'BEGIN { k = ENVIRON["K"]; v = ENVIRON["V"]; d = 0 }
		index($0, k "=") == 1 { if (!d) { print k "=" v; d = 1 }; next }
		{ print }
		END { if (!d) print k "=" v }' "$ENV_WORK" >"$ENV_WORK.new"
	mv -f "$ENV_WORK.new" "$ENV_WORK"
}

randhex() { openssl rand -hex "$1"; }

ram_mb() { awk '/^MemTotal:/ { printf "%d", $2 / 1024 }' /proc/meminfo; }

port_in_use() {
	local p=$1
	if command -v ss >/dev/null 2>&1; then
		ss -Hltn "sport = :$p" 2>/dev/null | grep -q .
	else
		(exec 3<>"/dev/tcp/127.0.0.1/$p") 2>/dev/null
	fi
}

# port already assigned to another instance (by its env file), even if that service is stopped
port_claimed() {
	local p=$1 f
	for f in "$ROOT"/etc/*/setu.env; do
		[[ -f $f && $f != "$ROOT$ENV_FILE" ]] || continue
		grep -qE "^SETU_ADDR='?[0-9.]*:$p'?\$" "$f" && return 0
	done
	return 1
}

# GitHub API helpers (token optional; needed for private repositories)
gh_curl() {
	local auth=()
	[[ -n $TOKEN ]] && auth=(-H "Authorization: Bearer $TOKEN")
	curl -fsSL --retry 3 --connect-timeout 15 -H "X-GitHub-Api-Version: 2022-11-28" "${auth[@]}" "$@"
}

# ====================================================================== steps
preflight() {
	if [[ $DRY_RUN == 1 ]]; then
		info "[dry-run] skipping root / OS checks"
		return
	fi
	[[ $EUID -eq 0 ]] || die "run as root: curl … | sudo bash -s -- --domain … --email …"
	[[ -r /etc/os-release ]] || die "cannot read /etc/os-release – this installer supports Ubuntu 24.04"
	# shellcheck disable=SC1091
	. /etc/os-release
	if [[ ${ID:-} != ubuntu ]]; then
		die "this installer supports Ubuntu 24.04 (found ${PRETTY_NAME:-unknown}). In hPanel choose the 'Ubuntu 24.04' OS template."
	fi
	if [[ ${VERSION_ID:-} != 24.04 ]]; then
		[[ $FORCE == 1 ]] || die "tested on Ubuntu 24.04 only (found $VERSION_ID). Re-install the VPS with Ubuntu 24.04, or pass --force to try anyway."
		warn "untested Ubuntu $VERSION_ID – continuing because of --force"
	fi
	UBUNTU_CODENAME=${VERSION_CODENAME:-noble}
	command -v systemctl >/dev/null || die "systemd is required"
	if [[ $EXISTING == 0 && -e /etc/$INSTANCE && ! -f /etc/$INSTANCE/setu.env ]]; then
		die "/etc/$INSTANCE already exists and is not a Setu instance – choose another --instance name"
	fi
	if id "$INSTANCE" >/dev/null 2>&1; then
		local home
		home=$(getent passwd "$INSTANCE" | cut -d: -f6)
		[[ $home == "$APP_DIR" ]] || die "system user '$INSTANCE' already exists (home $home) – choose another --instance name"
	fi
	local avail
	avail=$(df -Pm / | awk 'NR == 2 { print $4 }')
	[[ ${avail:-0} -ge 3000 ]] || warn "only ${avail} MB free on / – at least 3 GB is recommended"
}

base_system() {
	step "Base system (packages, updates)"
	UBUNTU_CODENAME=${UBUNTU_CODENAME:-noble}
	run mkdir -p "$STATE_DIR"
	run env DEBIAN_FRONTEND=noninteractive apt-get update -q </dev/null
	if [[ ! -f $STATE_DIR/base-upgraded ]]; then
		info "first run: upgrading installed packages (a few minutes)"
		run env DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a NEEDRESTART_SUSPEND=1 \
			apt-get -y -q -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold upgrade </dev/null
		run touch "$STATE_DIR/base-upgraded"
	fi
	apt_install ca-certificates curl gnupg jq openssl ufw unattended-upgrades cron iproute2 \
		debian-keyring debian-archive-keyring apt-transport-https lsb-release tzdata
	if [[ $DRY_RUN == 0 ]]; then
		printf 'APT::Periodic::Update-Package-Lists "1";\nAPT::Periodic::Unattended-Upgrade "1";\n' \
			>/etc/apt/apt.conf.d/20auto-upgrades
	fi
	run systemctl enable --now unattended-upgrades cron
	ok "packages and automatic security updates"
}

setup_swap() {
	local ram
	ram=$(ram_mb)
	if [[ $ram -ge 4000 ]]; then return; fi
	step "Swap (RAM is ${ram} MB)"
	if [[ $DRY_RUN == 0 ]] && { swapon --show --noheadings | grep -q . || [[ -f /swapfile ]]; }; then
		ok "swap already present"
		return
	fi
	run fallocate -l 2G /swapfile
	run chmod 600 /swapfile
	run mkswap /swapfile
	run swapon /swapfile
	if [[ $DRY_RUN == 0 ]]; then
		grep -q '^/swapfile ' /etc/fstab || echo '/swapfile none swap sw 0 0' >>/etc/fstab
		echo 'vm.swappiness=10' >/etc/sysctl.d/90-setu-swap.conf
		sysctl -q -p /etc/sysctl.d/90-setu-swap.conf || true
	fi
	ok "2 GB swap file"
}

setup_firewall() {
	step "Firewall (ufw: SSH, 80, 443)"
	local p ssh_ports="22"
	if [[ $DRY_RUN == 0 ]] && command -v ss >/dev/null; then
		# keep custom SSH ports open too, so enabling ufw never locks you out
		for p in $(ss -Hltnp 2>/dev/null | awk '/"sshd"/ { n = split($4, a, ":"); print a[n] }' | sort -u); do
			[[ $p =~ ^[0-9]+$ ]] && ssh_ports="$ssh_ports $p"
		done
	fi
	for p in $(tr ' ' '\n' <<<"$ssh_ports" | sort -u); do run ufw allow "$p/tcp"; done
	run ufw allow 80/tcp
	run ufw allow 443/tcp
	run ufw allow 443/udp
	run ufw --force enable
	ok "ufw enabled (also allow 80/443 in Hostinger's VPS firewall in hPanel if you use it)"
}

setup_postgres() {
	step "PostgreSQL 17 (PGDG)"
	if [[ $DRY_RUN == 1 ]] || ! dpkg -s postgresql-17 >/dev/null 2>&1; then
		run install -d -m 755 /usr/share/postgresql-common/pgdg
		run curl -fsSL -o /usr/share/postgresql-common/pgdg/apt.postgresql.org.asc https://www.postgresql.org/media/keys/ACCC4CF8.asc
		echo "deb [signed-by=/usr/share/postgresql-common/pgdg/apt.postgresql.org.asc] https://apt.postgresql.org/pub/repos/apt ${UBUNTU_CODENAME}-pgdg main" |
			write_file /etc/apt/sources.list.d/pgdg.list 644 root:root || true
		run env DEBIAN_FRONTEND=noninteractive apt-get update -q </dev/null
		apt_install postgresql-17 postgresql-client-17
	fi
	# tune for the VPS (shared with the app); recomputed on every run
	local ram sb ecs mwm wm
	ram=$(ram_mb)
	sb=$((ram / 4)); ((sb > 8192)) && sb=8192
	ecs=$((ram * 3 / 5))
	mwm=$((ram / 16)); ((mwm > 1024)) && mwm=1024; ((mwm < 64)) && mwm=64
	if ((ram >= 7000)); then wm=16; elif ((ram >= 3500)); then wm=8; else wm=4; fi
	if write_file /etc/postgresql/17/main/conf.d/90-setu.conf 644 postgres:postgres <<EOF; then
# Managed by Setu install.sh – recomputed from RAM (${ram} MB) on every run.
listen_addresses = 'localhost'
max_connections = 200
shared_buffers = ${sb}MB
effective_cache_size = ${ecs}MB
work_mem = ${wm}MB
maintenance_work_mem = ${mwm}MB
wal_compression = on
max_wal_size = 2GB
checkpoint_completion_target = 0.9
random_page_cost = 1.1
effective_io_concurrency = 200
jit = off
log_min_duration_statement = 500
log_lock_waits = on
idle_in_transaction_session_timeout = 60s
statement_timeout = 120s
EOF
		run systemctl restart postgresql
		ok "tuning written (shared_buffers ${sb}MB) and PostgreSQL restarted"
	else
		ok "PostgreSQL tuned (shared_buffers ${sb}MB)"
	fi
	run systemctl enable --now postgresql
}

setup_caddy() {
	step "Caddy (automatic HTTPS)"
	if [[ $DRY_RUN == 1 ]] || ! command -v caddy >/dev/null 2>&1; then
		if [[ $DRY_RUN == 1 ]]; then
			run sh -c "curl -1sLf https://dl.cloudsmith.io/public/caddy/stable/gpg.key | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg"
		else
			curl -1sLf https://dl.cloudsmith.io/public/caddy/stable/gpg.key | gpg --dearmor --yes -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
		fi
		run sh -c "curl -1sLf https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt > /etc/apt/sources.list.d/caddy-stable.list"
		run env DEBIAN_FRONTEND=noninteractive apt-get update -q </dev/null
		apt_install caddy
	fi
	run install -d -m 755 /etc/caddy/Caddyfile.d
	local main="$ROOT/etc/caddy/Caddyfile"
	if [[ -f $main ]] && grep -q 'Managed by Setu install.sh' "$main"; then
		ok "main Caddyfile already managed by Setu"
	elif [[ -f $main ]] && ! grep -q 'easy way to configure' "$main"; then
		# a hand-written Caddyfile: keep it, only make sure our site files are imported
		if ! grep -q 'Caddyfile.d/\*.caddy' "$main"; then
			printf '\n# Added by Setu install.sh\nimport /etc/caddy/Caddyfile.d/*.caddy\n' >>"$main"
			warn "kept your existing /etc/caddy/Caddyfile and appended an import of Caddyfile.d/*.caddy"
		fi
	else
		[[ -n $ACME_EMAIL ]] || die "--email is required on the first install (Let's Encrypt contact)"
		write_file /etc/caddy/Caddyfile 644 root:root <<EOF || true
# Managed by Setu install.sh – one file per instance lives in /etc/caddy/Caddyfile.d/<instance>.caddy
{
	email $ACME_EMAIL
}

import /etc/caddy/Caddyfile.d/*.caddy
EOF
		ok "main Caddyfile written"
	fi
	if [[ $DRY_RUN == 0 ]] && ss -Hltnp 'sport = :80' 2>/dev/null | grep -v '"caddy"' | grep -q .; then
		warn "port 80 is used by another program ($(ss -Hltnp 'sport = :80' | grep -o 'users:(("[^"]*' | head -1 | cut -d'"' -f2)) – stop it (e.g. systemctl disable --now apache2 nginx) so Caddy can serve HTTPS"
	fi
	run systemctl enable --now caddy
}

setup_gotenberg() {
	step "Gotenberg (PDF rendering, shared by all instances)"
	if [[ $DRY_RUN == 1 ]] || ! command -v docker >/dev/null 2>&1; then
		apt_install docker.io
	fi
	run systemctl enable --now docker
	if [[ $DRY_RUN == 0 ]] && docker container inspect gotenberg >/dev/null 2>&1; then
		docker update --restart unless-stopped gotenberg >/dev/null
		docker start gotenberg >/dev/null
		ok "gotenberg container already present (127.0.0.1:3000)"
		return
	fi
	run docker run -d --name gotenberg --restart unless-stopped --memory 1g \
		-p 127.0.0.1:3000:3000 "$GOTENBERG_IMAGE" \
		gotenberg --chromium-auto-start --api-timeout=120s '--chromium-deny-list=^(?!file:///tmp/).*'
	ok "gotenberg started on 127.0.0.1:3000 (memory limit 1 GB)"
}

setup_postfix() {
	step "Postfix (send-only, for the default 'sendmail' mailer)"
	if [[ $DRY_RUN == 1 ]] || ! dpkg -s postfix >/dev/null 2>&1; then
		if [[ $DRY_RUN == 0 ]]; then
			debconf-set-selections <<<"postfix postfix/mailname string $DOMAIN"
			debconf-set-selections <<<"postfix postfix/main_mailer_type string 'Internet Site'"
		fi
		apt_install postfix
	fi
	# myhostname = the first instance's domain (one per VPS); later instances keep it
	if [[ $DRY_RUN == 1 || ! -f $STATE_DIR/postfix-hostname ]]; then
		run postconf -e "myhostname = $DOMAIN" "inet_interfaces = loopback-only" \
			"mydestination = localhost" "inet_protocols = all" "smtp_tls_security_level = may"
		if [[ $DRY_RUN == 0 ]]; then
			echo "$DOMAIN" >/etc/mailname
			echo "$DOMAIN" >"$STATE_DIR/postfix-hostname"
		fi
		run systemctl restart postfix
	else
		run postconf -e "inet_interfaces = loopback-only"
	fi
	run systemctl enable postfix
	ok "postfix listens on 127.0.0.1 only and sends as $(cat "$STATE_DIR/postfix-hostname" 2>/dev/null || echo "$DOMAIN")"
}

create_user_and_dirs() {
	if [[ $DRY_RUN == 1 ]] || ! id "$INSTANCE" >/dev/null 2>&1; then
		run useradd --system --user-group --home-dir "$APP_DIR" --no-create-home --shell /usr/sbin/nologin "$INSTANCE"
	fi
	run install -d -m 755 -o root -g root "$APP_DIR"
	run install -d -m 755 -o "$INSTANCE" -g "$INSTANCE" "$APP_DIR/bin"
	run install -d -m 750 -o "$INSTANCE" -g "$INSTANCE" "$APP_DIR/data"
	run install -d -m 700 -o root -g root "/etc/$INSTANCE" "/var/backups/$INSTANCE"
	if [[ $DRY_RUN == 1 ]]; then mkdir -p "$ROOT$APP_DIR/bin" "$ROOT$APP_DIR/data" "$ROOT/etc/$INSTANCE"; fi
	ok "user $INSTANCE, $APP_DIR/{bin,data}"
}

pick_port() {
	if [[ -z $PORT ]]; then
		local cur
		cur=$(env_get SETU_ADDR)
		[[ $cur =~ :([0-9]+)$ ]] && PORT=${BASH_REMATCH[1]}
	fi
	if [[ -z $PORT ]]; then
		local p=8080
		while port_in_use "$p" || port_claimed "$p"; do
			p=$((p + 1))
			((p < 8200)) || die "no free port between 8080 and 8199"
		done
		PORT=$p
	elif port_claimed "$PORT"; then
		die "port $PORT is already assigned to another instance"
	fi
	ok "port 127.0.0.1:$PORT"
}

write_env() {
	local work
	work=$(mktemp -d)
	chmod 700 "$work"
	ENV_WORK="$work/setu.env"
	if [[ -f $ROOT$ENV_FILE ]]; then cp "$ROOT$ENV_FILE" "$ENV_WORK"; else
		printf '# Setu instance %s – generated by deploy/install.sh. Keep this file secret (mode 600).\n# SETU_MASTER_KEY encrypts every workspace secret: back it up offline, never change it.\n' "$INSTANCE" >"$ENV_WORK"
	fi
	MASTER_NEW=0
	local key owner_pw app_pw
	key=$(env_get SETU_MASTER_KEY)
	if [[ -z $key ]]; then key=$(randhex 32); MASTER_NEW=1; fi
	[[ $key =~ ^[0-9a-fA-F]{64}$ ]] || die "SETU_MASTER_KEY in $ENV_FILE is not 64 hex characters – fix it by hand (never replace a key that is in use)"
	owner_pw=$(env_get SETU_DB_OWNER_PASSWORD); [[ -n $owner_pw ]] || owner_pw=$(randhex 24)
	app_pw=$(env_get SETU_DB_APP_PASSWORD); [[ -n $app_pw ]] || app_pw=$(randhex 24)
	DB_OWNER_PW=$owner_pw DB_APP_PW=$app_pw

	if [[ -n $ADMIN_EMAIL && -z $ADMIN_PASSWORD ]]; then
		if [[ $(env_get SETU_ADMIN_EMAIL) == "$ADMIN_EMAIL" && -n $(env_get SETU_ADMIN_PASSWORD) ]]; then
			ADMIN_PASSWORD=$(env_get SETU_ADMIN_PASSWORD)
		else
			ADMIN_PASSWORD=$(openssl rand -base64 18 | tr -d '/+=' | cut -c1-16)
			ADMIN_PW_GENERATED=1
		fi
	fi

	env_set SETU_APP_HOST "$DOMAIN"
	env_set SETU_SCHEME https
	env_set SETU_ADDR "127.0.0.1:$PORT"
	env_set SETU_MASTER_KEY "$key"
	env_set SETU_DB_OWNER_PASSWORD "$owner_pw"
	env_set SETU_DB_APP_PASSWORD "$app_pw"
	env_set DATABASE_URL "postgres://$DB_APP:$app_pw@127.0.0.1:5432/$DB_NAME?sslmode=disable"
	env_set MIGRATE_DATABASE_URL "postgres://$DB_OWNER:$owner_pw@127.0.0.1:5432/$DB_NAME?sslmode=disable"
	env_set SETU_STORAGE "$APP_DIR/data"
	env_set GOTENBERG_URL "http://127.0.0.1:3000"
	env_set SETU_UPDATE_REPO "$REPO"
	env_set SETU_UPDATE_CHANNEL "$CHANNEL"
	[[ -n $TOKEN ]] && env_set SETU_UPDATE_TOKEN "$TOKEN"
	if [[ -n $ADMIN_EMAIL ]]; then
		env_set SETU_ADMIN_EMAIL "$ADMIN_EMAIL"
		env_set SETU_ADMIN_PASSWORD "$ADMIN_PASSWORD"
	fi
	ENV_CHANGED=0
	if write_file "$ENV_FILE" 600 root:root <"$ENV_WORK"; then ENV_CHANGED=1; fi
	rm -rf "$work"
	unset ENV_WORK
	ok "env file $ENV_FILE (mode 600)$([[ $MASTER_NEW == 1 ]] && echo ' – new SETU_MASTER_KEY generated')"
}

setup_database() {
	local sql
	sql=$(
		cat <<EOF
\\set ON_ERROR_STOP on
\\set owner '$DB_OWNER'
\\set app '$DB_APP'
\\set db '$DB_NAME'
\\set owner_pw '$DB_OWNER_PW'
\\set app_pw '$DB_APP_PW'
SET client_min_messages TO warning;
SELECT format('CREATE ROLE %I LOGIN', :'owner') WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'owner') \\gexec
SELECT format('CREATE ROLE %I LOGIN', :'app') WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'app') \\gexec
ALTER ROLE :"owner" WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD :'owner_pw';
ALTER ROLE :"app" WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD :'app_pw';
SELECT format('CREATE DATABASE %I OWNER %I ENCODING ''UTF8'' TEMPLATE template0', :'db', :'owner') WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = :'db') \\gexec
ALTER DATABASE :"db" OWNER TO :"owner";
REVOKE ALL ON DATABASE :"db" FROM PUBLIC;
GRANT CONNECT, TEMPORARY ON DATABASE :"db" TO :"app";
\\connect :"db"
SET client_min_messages TO warning;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pgcrypto;
ALTER SCHEMA public OWNER TO :"owner";
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO :"app";
ALTER DEFAULT PRIVILEGES FOR ROLE :"owner" IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO :"app";
ALTER DEFAULT PRIVILEGES FOR ROLE :"owner" IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO :"app";
EOF
	)
	if [[ $DRY_RUN == 1 ]]; then
		printf '%s\n' "$sql" >"$ROOT/database-$INSTANCE.sql"
		info "[dry-run] SQL for runuser -u postgres -- psql written to $ROOT/database-$INSTANCE.sql"
	else
		(cd / && runuser -u postgres -- psql -q -X -d postgres) <<<"$sql" >/dev/null
	fi
	ok "database $DB_NAME, roles $DB_OWNER (migrations) and $DB_APP (app: NOSUPERUSER NOBYPASSRLS)"
}

install_binary() {
	local bin="$APP_DIR/bin/setu" rel tag url sumurl tmp expected actual
	BINARY_CHANGED=0
	if [[ -n $RELEASE ]]; then rel="releases/tags/$RELEASE"
	elif [[ $CHANNEL == edge ]]; then rel="releases/tags/edge"
	else rel="releases/latest"; fi
	if [[ $DRY_RUN == 1 ]]; then
		info "[dry-run] would download $ASSET (+ .sha256) from https://api.github.com/repos/$REPO/$rel, verify and install to $bin"
		return
	fi
	local json
	if ! json=$(gh_curl -H "Accept: application/vnd.github+json" "https://api.github.com/repos/$REPO/$rel" 2>/dev/null); then
		if [[ $rel == releases/latest ]]; then
			die "no stable release found in $REPO. Create one (git tag v2.0.0 && git push --tags) or install the edge build: --channel edge$([[ -z $TOKEN ]] && echo '. Private repository? add --token <GitHub token>')"
		fi
		die "release '${rel#releases/tags/}' not found in $REPO$([[ -z $TOKEN ]] && echo ' (private repository? add --token <GitHub token>)')"
	fi
	tag=$(jq -r '.tag_name' <<<"$json")
	url=$(jq -r --arg n "$ASSET" '.assets[] | select(.name == $n) | .url' <<<"$json")
	sumurl=$(jq -r --arg n "$ASSET.sha256" '.assets[] | select(.name == $n) | .url' <<<"$json")
	[[ -n $url && -n $sumurl ]] || die "release $tag has no $ASSET / $ASSET.sha256 asset (did the release workflow finish?)"
	tmp=$(mktemp -d)
	gh_curl -H "Accept: application/octet-stream" -o "$tmp/$ASSET.sha256" "$sumurl" || die "download of $ASSET.sha256 failed"
	expected=$(awk '{ print $1; exit }' "$tmp/$ASSET.sha256")
	[[ $expected =~ ^[0-9a-f]{64}$ ]] || die "malformed checksum file for $ASSET"
	if [[ -f $bin ]] && [[ $(sha256sum "$bin" | awk '{ print $1 }') == "$expected" ]]; then
		rm -rf "$tmp"
		RELEASE_TAG=$tag
		ok "binary already up to date ($tag)"
		return
	fi
	info "downloading $ASSET from release $tag"
	gh_curl -H "Accept: application/octet-stream" -o "$tmp/$ASSET" "$url" || die "download of $ASSET failed"
	actual=$(sha256sum "$tmp/$ASSET" | awk '{ print $1 }')
	[[ $actual == "$expected" ]] || die "checksum mismatch for $ASSET (expected $expected, got $actual)"
	install -m 755 -o "$INSTANCE" -g "$INSTANCE" "$tmp/$ASSET" "$bin.new"
	[[ -f $bin ]] && cp -p "$bin" "$bin.old"
	mv -f "$bin.new" "$bin"
	rm -rf "$tmp"
	RELEASE_TAG=$tag BINARY_CHANGED=1
	ok "installed $bin ($tag, sha256 verified)"
}

# setu-update / setu-uninstall / setu-backup and the <instance>-cli wrapper
install_helpers() {
	local f
	for f in update uninstall; do
		if [[ $DRY_RUN == 1 ]]; then
			info "[dry-run] would fetch deploy/$f.sh from $REPO@$SCRIPTS_REF to /usr/local/sbin/setu-$f"
		elif gh_curl -H "Accept: application/vnd.github.raw" -o "/usr/local/sbin/setu-$f.new" \
			"https://api.github.com/repos/$REPO/contents/deploy/$f.sh?ref=$SCRIPTS_REF" 2>/dev/null; then
			chmod 755 "/usr/local/sbin/setu-$f.new" && mv -f "/usr/local/sbin/setu-$f.new" "/usr/local/sbin/setu-$f"
		else
			rm -f "/usr/local/sbin/setu-$f.new"
			warn "could not fetch deploy/$f.sh from $REPO – run it from a clone of the repository instead"
		fi
	done
	write_file /usr/local/sbin/setu-backup 755 root:root <<'EOF' || true
#!/usr/bin/env bash
# Nightly backup of one Setu instance: database (pg_dump -Fc) + uploaded files, kept 14 days.
# Usage: setu-backup <instance>     Restore: see docs/HOSTINGER.md ("Backups").
set -euo pipefail
inst=${1:?usage: setu-backup <instance>}
[[ $inst =~ ^[a-z][a-z0-9_]{1,30}$ ]] || { echo "bad instance name" >&2; exit 1; }
dir=/var/backups/$inst keep=${SETU_BACKUP_DAYS:-14} ts=$(date +%Y%m%d-%H%M)
install -d -m 700 "$dir"
cd /
runuser -u postgres -- pg_dump -Fc -d "$inst" >"$dir/$inst-db-$ts.dump.part"
mv "$dir/$inst-db-$ts.dump.part" "$dir/$inst-db-$ts.dump"
if [[ -d /opt/$inst/data ]]; then tar -C "/opt/$inst" -czf "$dir/$inst-files-$ts.tar.gz" data; fi
find "$dir" -maxdepth 1 -type f \( -name "$inst-db-*.dump" -o -name "$inst-files-*.tar.gz" \) -mtime +"$keep" -delete
echo "$(date -Is) backup ok: $dir/$inst-db-$ts.dump ($(du -h "$dir/$inst-db-$ts.dump" | cut -f1))"
EOF
	write_file "/usr/local/sbin/$INSTANCE-cli" 755 root:root <<EOF || true
#!/usr/bin/env bash
# Run the Setu binary of instance '$INSTANCE' with its environment, e.g.
#   $INSTANCE-cli platform-admin you@example.com      $INSTANCE-cli migrate
#   $INSTANCE-cli import-v1 /tmp/export.jsonl "Company name"
set -euo pipefail
[[ \$EUID -eq 0 ]] || exec sudo "\$0" "\$@"
set -a; . $ENV_FILE; set +a
cd $APP_DIR
exec runuser -u $INSTANCE -- $APP_DIR/bin/setu "\$@"
EOF
	ok "helpers: setu-update, setu-uninstall, setu-backup, $INSTANCE-cli"
}

write_unit() {
	UNIT_CHANGED=0
	if write_file "/etc/systemd/system/$SERVICE" 644 root:root <<EOF; then UNIT_CHANGED=1; fi
# Managed by Setu install.sh (instance $INSTANCE)
[Unit]
Description=Setu CRM ($INSTANCE – $DOMAIN)
Wants=network-online.target postgresql.service docker.service
After=network-online.target postgresql.service docker.service postfix.service

[Service]
Type=simple
User=$INSTANCE
Group=$INSTANCE
# postdrop: lets the postfix 'sendmail' command queue mail despite NoNewPrivileges
SupplementaryGroups=postdrop
EnvironmentFile=$ENV_FILE
WorkingDirectory=$APP_DIR
ExecStart=$APP_DIR/bin/setu all
Restart=always
RestartSec=2
TimeoutStopSec=30
LimitNOFILE=65536

# hardening – the binary may only write its data and its own bin/ (in-app self-update)
NoNewPrivileges=yes
ProtectSystem=strict
ReadWritePaths=$APP_DIR/data $APP_DIR/bin -/var/spool/postfix/maildrop
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectControlGroups=yes
ProtectClock=yes
ProtectHostname=yes
RestrictSUIDSGID=yes
RestrictRealtime=yes
RestrictNamespaces=yes
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX AF_NETLINK
LockPersonality=yes
MemoryDenyWriteExecute=yes
SystemCallArchitectures=native
CapabilityBoundingSet=
AmbientCapabilities=
UMask=0027

[Install]
WantedBy=multi-user.target
EOF
	run systemctl daemon-reload
	ok "systemd unit $SERVICE"
}

write_caddy_site() {
	local f="/etc/caddy/Caddyfile.d/$INSTANCE.caddy"
	if write_file "$f" 644 root:root <<EOF; then
# Managed by Setu install.sh (instance $INSTANCE)
$DOMAIN {
	encode zstd gzip
	reverse_proxy 127.0.0.1:$PORT
}
EOF
		local log
		if [[ $DRY_RUN == 0 ]] && ! log=$(caddy validate --adapter caddyfile --config /etc/caddy/Caddyfile 2>&1); then
			printf '%s\n' "$log" | tail -n 20 >&2
			rm -f "$f"
			die "Caddy rejected the configuration (see above) – $f was removed"
		fi
		run systemctl reload-or-restart caddy
	fi
	ok "Caddy site $DOMAIN → 127.0.0.1:$PORT ($f)"
}

write_backup_cron() {
	local min=$(((PORT * 7) % 50 + 5))
	write_file "/etc/cron.d/$INSTANCE-backup" 644 root:root <<EOF || true
# Nightly backup of Setu instance $INSTANCE (14 days in /var/backups/$INSTANCE) – managed by install.sh
SHELL=/bin/bash
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
$min 2 * * * root /usr/local/sbin/setu-backup $INSTANCE >>/var/log/setu-backup-$INSTANCE.log 2>&1
EOF
	ok "nightly backup at 02:$(printf %02d "$min") → /var/backups/$INSTANCE"
}

start_and_check() {
	step "Starting $SERVICE"
	run systemctl enable -q "$SERVICE"
	if [[ $DRY_RUN == 1 ]]; then
		run systemctl restart "$SERVICE"
		return
	fi
	if [[ ${BINARY_CHANGED:-0} == 1 || ${ENV_CHANGED:-0} == 1 || ${UNIT_CHANGED:-0} == 1 ]] || ! systemctl is-active -q "$SERVICE"; then
		systemctl restart "$SERVICE"
	fi
	local i
	for i in $(seq 1 90); do
		if curl -fsS -m 3 -H "Host: $DOMAIN" "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1; then
			ok "healthy (http://127.0.0.1:$PORT/healthz)"
			HEALTHY=1
			break
		fi
		sleep 1
	done
	if [[ ${HEALTHY:-0} != 1 ]]; then
		journalctl -u "$SERVICE" -n 40 --no-pager >&2 || true
		die "$SERVICE did not become healthy within 90 s – see the log above (journalctl -u $INSTANCE -f)"
	fi
	# public HTTPS (needs the DNS A record; Caddy fetches the certificate on first request)
	local pub dns
	pub=$(curl -fsS4 -m 5 https://api.ipify.org 2>/dev/null || true)
	dns=$(getent ahostsv4 "$DOMAIN" 2>/dev/null | awk 'NR == 1 { print $1 }' || true)
	if curl -fsS -m 30 "https://$DOMAIN/healthz" >/dev/null 2>&1; then
		ok "https://$DOMAIN is live with a valid certificate"
	elif [[ -z $dns ]]; then
		warn "$DOMAIN does not resolve yet – add an A record → ${pub:-<VPS IP>}; HTTPS starts working a few minutes after DNS is live"
	elif [[ -n $pub && $dns != "$pub" ]]; then
		warn "$DOMAIN points to $dns but this VPS is $pub. Fix the A record (or, behind Cloudflare's proxy, set SSL mode to Full (strict))."
	else
		warn "https://$DOMAIN is not reachable yet – check the hPanel VPS firewall allows 80/443, then: journalctl -u caddy -n 50"
	fi
}

summary() {
	local admin_line
	if [[ -n $ADMIN_EMAIL ]]; then
		if [[ ${ADMIN_PW_GENERATED:-0} == 1 ]]; then
			admin_line="$ADMIN_EMAIL / $ADMIN_PASSWORD   (generated – change it after the first login)"
		else
			admin_line="$ADMIN_EMAIL / (the password you gave, stored in $ENV_FILE)"
		fi
	elif [[ -n $(env_get SETU_ADMIN_EMAIL) ]]; then
		admin_line="$(env_get SETU_ADMIN_EMAIL) (from $ENV_FILE)"
	else
		admin_line="none set – sign up at the URL, then: sudo $INSTANCE-cli platform-admin you@example.com"
	fi
	local ip rua dmarc
	ip=$(curl -fsS4 -m 5 https://api.ipify.org 2>/dev/null || echo '<VPS IP>')
	rua=$(env_get SETU_ADMIN_EMAIL)
	rua=${rua:-$ACME_EMAIL}
	dmarc="v=DMARC1; p=none${rua:+; rua=mailto:$rua}"
	cat <<EOF

${c_bold}${c_grn}Setu instance '$INSTANCE' is installed.${c_off}

  URL              https://$DOMAIN
  Platform admin   $admin_line
  Release          ${RELEASE_TAG:-?} (channel $CHANNEL, repo $REPO)
  Env file         $ENV_FILE  (mode 600 – contains SETU_MASTER_KEY and DB passwords)
  Master key       SETU_MASTER_KEY in that file – copy it to your password manager NOW.
                   Losing it makes every stored API key/password unreadable. Never change it.
  Service          systemctl status $INSTANCE   |   logs: journalctl -u $INSTANCE -f
  Data / binary    $APP_DIR/data   |   $APP_DIR/bin/setu
  Backups          nightly → /var/backups/$INSTANCE (14 days); now: sudo setu-backup $INSTANCE
  Update           Platform console → Updates, or: sudo setu-update --instance $INSTANCE
  Admin CLI        sudo $INSTANCE-cli platform-admin <email>

  Email (default 'sendmail' mailer via local Postfix) – add these DNS records for $DOMAIN's domain:
    SPF    TXT  @       "v=spf1 ip4:$ip ~all"   (merge with an existing SPF record)
    DMARC  TXT  _dmarc  "$dmarc"
    PTR    set the VPS reverse DNS to $(cat "$STATE_DIR/postfix-hostname" 2>/dev/null || echo "$DOMAIN") in hPanel
  DKIM needs a signing service (see docs/HOSTINGER.md) – or use an SMTP mailbox in Platform → Email.
EOF
	if [[ -n $ADMIN_EMAIL ]]; then
		info "Tip: after the first login you may remove SETU_ADMIN_PASSWORD from $ENV_FILE."
	fi
	if [[ $DRY_RUN == 1 ]]; then
		printf '\n[dry-run] generated files:\n'
		(cd "$ROOT" && find . -type f | sort | sed 's/^\./    /')
	fi
}

main "$@"
