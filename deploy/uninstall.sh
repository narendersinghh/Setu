#!/usr/bin/env bash
# Remove one Setu instance from this VPS (service, Caddy site, cron, database + roles, /opt/<instance>,
# /etc/<instance>). Shared parts (PostgreSQL, Caddy, Gotenberg, Postfix) and other instances stay.
#
#   sudo setu-uninstall --instance crm2            # final backup + env file kept in /var/backups/crm2
#   sudo setu-uninstall --instance crm2 --purge    # also delete /var/backups/crm2 (irreversible)
#
# Options: --instance NAME (required)  --purge  --yes (do not ask for confirmation)
main() {
set -euo pipefail
INSTANCE="" PURGE=0 YES=0
while [[ $# -gt 0 ]]; do
	case "$1" in
	--instance) INSTANCE="${2:-}"; shift 2 ;;
	--purge) PURGE=1; shift ;;
	--yes | -y) YES=1; shift ;;
	-h | --help) sed -n '2,9p' "${BASH_SOURCE[0]:-/dev/null}" 2>/dev/null || true; return 0 ;;
	*) die "unknown option: $1" ;;
	esac
done
[[ -n $INSTANCE ]] || die "--instance is required"
[[ $INSTANCE =~ ^[a-z][a-z0-9_]{1,30}$ ]] || die "invalid --instance"
[[ $EUID -eq 0 ]] || die "run as root (sudo)"
ENV_FILE=/etc/$INSTANCE/setu.env BACKUPS=/var/backups/$INSTANCE
[[ -f $ENV_FILE || -f /etc/systemd/system/$INSTANCE.service ]] || die "instance '$INSTANCE' not found"
grep -q 'Managed by Setu install.sh' "/etc/systemd/system/$INSTANCE.service" 2>/dev/null ||
	[[ -f $ENV_FILE ]] || die "$INSTANCE.service was not created by the Setu installer – refusing"

echo "This removes Setu instance '$INSTANCE': service, database '$INSTANCE', /opt/$INSTANCE, /etc/$INSTANCE."
if [[ $PURGE == 1 ]]; then echo "--purge: backups in $BACKUPS are deleted too. THIS CANNOT BE UNDONE."
else echo "A final backup and the env file (with SETU_MASTER_KEY) are kept in $BACKUPS."; fi
if [[ $YES == 0 ]]; then
	[[ -r /dev/tty ]] || die "no terminal to confirm – pass --yes"
	read -r -p "Type the instance name to confirm: " answer </dev/tty
	[[ $answer == "$INSTANCE" ]] || die "aborted"
fi

if [[ $PURGE == 0 ]]; then
	install -d -m 700 "$BACKUPS"
	if command -v setu-backup >/dev/null && runuser -u postgres -- psql -XAtqc "select 1 from pg_database where datname='$INSTANCE'" 2>/dev/null | grep -q 1; then
		setu-backup "$INSTANCE" || die "final backup failed – nothing removed"
	fi
	[[ -f $ENV_FILE ]] && install -m 600 "$ENV_FILE" "$BACKUPS/setu.env.$(date +%Y%m%d-%H%M)"
fi

systemctl disable --now "$INSTANCE.service" 2>/dev/null || true
rm -f "/etc/systemd/system/$INSTANCE.service" "/etc/cron.d/$INSTANCE-backup" "/usr/local/sbin/$INSTANCE-cli"
systemctl daemon-reload
if [[ -f /etc/caddy/Caddyfile.d/$INSTANCE.caddy ]]; then
	rm -f "/etc/caddy/Caddyfile.d/$INSTANCE.caddy"
	systemctl reload caddy 2>/dev/null || true
fi
(cd / && runuser -u postgres -- psql -X -q -v ON_ERROR_STOP=1 -d postgres) <<SQL || echo "!! could not drop the database/roles – check with: sudo -u postgres psql" >&2
SET client_min_messages TO warning;
DROP DATABASE IF EXISTS "$INSTANCE" WITH (FORCE);
DROP ROLE IF EXISTS "${INSTANCE}_app";
DROP ROLE IF EXISTS "${INSTANCE}_owner";
SQL
rm -rf "/opt/$INSTANCE" "/etc/$INSTANCE"
id "$INSTANCE" >/dev/null 2>&1 && userdel "$INSTANCE" 2>/dev/null || true
if [[ $PURGE == 1 ]]; then rm -rf "$BACKUPS" "/var/log/setu-backup-$INSTANCE.log"; fi
echo "instance '$INSTANCE' removed.$([[ $PURGE == 0 ]] && echo " Backups: $BACKUPS")"
}

die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

main "$@"
