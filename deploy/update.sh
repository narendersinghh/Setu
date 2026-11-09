#!/usr/bin/env bash
# Manual update of one Setu instance – the same steps as the in-app updater (Platform console → Updates):
# download the release asset for this CPU, verify its .sha256, swap the binary, restart, wait for /healthz.
# If the new version does not become healthy, the previous binary is put back automatically.
#
#   sudo setu-update --instance setu                 # installed by install.sh
#   sudo bash deploy/update.sh --instance setu       # from a clone of the repository
#
# Options:
#   --instance NAME        instance to update (default: setu)
#   --channel stable|edge  override SETU_UPDATE_CHANNEL for this run (also saved to the env file)
#   --release TAG          install this exact release (e.g. v2.0.1) – also how you pin/downgrade
#   --token TOKEN          GitHub token for a private repository (default: SETU_UPDATE_TOKEN)
#   --force                reinstall even when the binary is already the latest
#   --rollback             put back the previous binary (bin/setu.old) and restart
#   --check                only show whether an update is available
main() {
set -euo pipefail
INSTANCE=setu CHANNEL="" RELEASE="" TOKEN="" FORCE=0 ROLLBACK=0 CHECK=0
while [[ $# -gt 0 ]]; do
	case "$1" in
	--instance) INSTANCE="${2:-}"; shift 2 ;;
	--channel) CHANNEL="${2:-}"; shift 2 ;;
	--release) RELEASE="${2:-}"; shift 2 ;;
	--token) TOKEN="${2:-}"; shift 2 ;;
	--force) FORCE=1; shift ;;
	--rollback) ROLLBACK=1; shift ;;
	--check) CHECK=1; shift ;;
	-h | --help) sed -n '2,18p' "${BASH_SOURCE[0]:-/dev/null}" 2>/dev/null || true; return 0 ;;
	*) die "unknown option: $1" ;;
	esac
done
[[ $INSTANCE =~ ^[a-z][a-z0-9_]{1,30}$ ]] || die "invalid --instance"
[[ $EUID -eq 0 ]] || die "run as root (sudo)"
ENV_FILE=/etc/$INSTANCE/setu.env BIN=/opt/$INSTANCE/bin/setu SERVICE=$INSTANCE.service
[[ -f $ENV_FILE ]] || die "instance '$INSTANCE' not found ($ENV_FILE missing) – installed instances: $(ls /etc/*/setu.env 2>/dev/null | cut -d/ -f3 | xargs)"
for c in curl jq sha256sum systemctl; do command -v "$c" >/dev/null || die "$c is required"; done

REPO=$(env_get SETU_UPDATE_REPO); REPO=${REPO:-narendersinghh/setu}
[[ -n $TOKEN ]] || TOKEN=$(env_get SETU_UPDATE_TOKEN)
if [[ -n $CHANNEL ]]; then
	[[ $CHANNEL == stable || $CHANNEL == edge ]] || die "--channel must be stable or edge"
	if [[ $(env_get SETU_UPDATE_CHANNEL) != "$CHANNEL" ]]; then
		if grep -q '^SETU_UPDATE_CHANNEL=' "$ENV_FILE"; then
			sed -i "s/^SETU_UPDATE_CHANNEL=.*/SETU_UPDATE_CHANNEL=$CHANNEL/" "$ENV_FILE"
		else
			echo "SETU_UPDATE_CHANNEL=$CHANNEL" >>"$ENV_FILE"
		fi
		echo "channel set to $CHANNEL in $ENV_FILE"
	fi
else
	CHANNEL=$(env_get SETU_UPDATE_CHANNEL); CHANNEL=${CHANNEL:-stable}
fi
PORT=$(env_get SETU_ADDR); PORT=${PORT##*:}
HOST=$(env_get SETU_APP_HOST)

if [[ $ROLLBACK == 1 ]]; then
	[[ -f $BIN.old ]] || die "no previous binary ($BIN.old) to roll back to"
	cp -p "$BIN" "$BIN.failed"
	mv -f "$BIN.old" "$BIN"
	echo "rolled back to the previous binary (the replaced one is kept as $BIN.failed)"
	systemctl restart "$SERVICE"
	wait_healthy || die "still unhealthy after rollback – journalctl -u $INSTANCE -n 100"
	return 0
fi

case "$(uname -m)" in
x86_64 | amd64) ASSET=setu-linux-amd64 ;;
aarch64 | arm64) ASSET=setu-linux-arm64 ;;
*) die "unsupported architecture $(uname -m)" ;;
esac
if [[ -n $RELEASE ]]; then rel="releases/tags/$RELEASE"
elif [[ $CHANNEL == edge ]]; then rel="releases/tags/edge"
else rel="releases/latest"; fi

json=$(gh_curl -H "Accept: application/vnd.github+json" "https://api.github.com/repos/$REPO/$rel") ||
	die "cannot read $rel from $REPO$([[ -z $TOKEN ]] && echo ' (private repository? pass --token or set SETU_UPDATE_TOKEN)')"
tag=$(jq -r '.tag_name' <<<"$json")
title=$(jq -r '.name // empty' <<<"$json")
url=$(jq -r --arg n "$ASSET" '.assets[] | select(.name == $n) | .url' <<<"$json")
sumurl=$(jq -r --arg n "$ASSET.sha256" '.assets[] | select(.name == $n) | .url' <<<"$json")
[[ -n $url && -n $sumurl ]] || die "release $tag has no $ASSET / $ASSET.sha256 asset"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
gh_curl -H "Accept: application/octet-stream" -o "$tmp/$ASSET.sha256" "$sumurl" || die "checksum download failed"
expected=$(awk '{ print $1; exit }' "$tmp/$ASSET.sha256")
[[ $expected =~ ^[0-9a-f]{64}$ ]] || die "malformed checksum file"
current=$([[ -f $BIN ]] && sha256sum "$BIN" | awk '{ print $1 }' || true)
if [[ $current == "$expected" && $FORCE == 0 ]]; then
	echo "$INSTANCE is up to date (${title:-$tag})"
	return 0
fi
if [[ $CHECK == 1 ]]; then
	echo "update available for $INSTANCE: ${title:-$tag} (run without --check to install)"
	return 0
fi

echo "downloading $ASSET from ${title:-$tag} ($REPO)"
gh_curl -H "Accept: application/octet-stream" -o "$tmp/$ASSET" "$url" || die "download failed"
actual=$(sha256sum "$tmp/$ASSET" | awk '{ print $1 }')
[[ $actual == "$expected" ]] || die "checksum mismatch (expected $expected, got $actual) – nothing changed"

# backup the database first: migrations run on start-up
if command -v setu-backup >/dev/null; then
	echo "backing up the database before updating"
	setu-backup "$INSTANCE" || die "backup failed – not updating (use --force after fixing, or run setu-backup by hand)"
fi

install -m 755 -o "$INSTANCE" -g "$INSTANCE" "$tmp/$ASSET" "$BIN.new"
[[ -f $BIN ]] && cp -p "$BIN" "$BIN.old"
mv -f "$BIN.new" "$BIN"
echo "binary replaced, restarting $SERVICE"
systemctl restart "$SERVICE"
if wait_healthy; then
	echo "updated $INSTANCE to ${title:-$tag}"
else
	echo "!! new version is not healthy – restoring the previous binary" >&2
	journalctl -u "$SERVICE" -n 30 --no-pager >&2 || true
	if [[ -f $BIN.old ]]; then
		cp -p "$BIN" "$BIN.failed"
		cp -p "$BIN.old" "$BIN"
		systemctl restart "$SERVICE"
		wait_healthy && echo "previous version restored (database migrations are additive, so this is safe)"
	fi
	die "update failed – see journalctl -u $INSTANCE"
fi
}

die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

env_get() {
	local v
	v=$(grep -E "^$1=" "$ENV_FILE" | tail -n1 | cut -d= -f2-) || true
	v=${v#\'} v=${v%\'} v=${v#\"} v=${v%\"}
	printf '%s' "$v"
}

gh_curl() {
	local auth=()
	[[ -n $TOKEN ]] && auth=(-H "Authorization: Bearer $TOKEN")
	curl -fsSL --retry 3 --connect-timeout 15 -H "X-GitHub-Api-Version: 2022-11-28" "${auth[@]}" "$@"
}

wait_healthy() {
	local i
	for i in $(seq 1 90); do
		if curl -fsS -m 3 -H "Host: $HOST" "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1; then
			echo "healthy"
			return 0
		fi
		sleep 1
	done
	return 1
}

main "$@"
