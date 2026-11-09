#!/usr/bin/env bash
# Let Caddy serve HTTPS for everything on this VPS, replacing a Traefik container (e.g. Hostinger's
# n8n template). Every site Traefik routes today (read from its Docker labels) gets the same address in
# Caddy, then Traefik is stopped and kept from starting again. Data of n8n & co. is not touched.
#
#   sudo bash caddy-takeover.sh          # show the plan, ask, then switch
#   sudo bash caddy-takeover.sh --undo   # give ports 80/443 back to Traefik
main() {
set -euo pipefail
c_b=$'\e[1m' c_g=$'\e[32m' c_y=$'\e[33m' c_r=$'\e[31m' c_o=$'\e[0m'
say() { printf '%s\n' "$*"; }
ok() { printf '  %s✓%s %s\n' "$c_g" "$c_o" "$*"; }
warn() { printf '%s!! %s%s\n' "$c_y" "$*" "$c_o" >&2; }
die() { printf '%sERROR: %s%s\n' "$c_r" "$*" "$c_o" >&2; exit 1; }
[[ $EUID -eq 0 ]] || die "run as root (sudo bash $0)"
command -v docker >/dev/null || die "Docker is not installed – nothing to take over"
command -v caddy >/dev/null || die "Caddy is not installed – run the Setu installer first"
command -v python3 >/dev/null || die "python3 is missing (apt-get install -y python3)"
STATE=/var/lib/setu-installer/traefik-takeover
mkdir -p "$(dirname "$STATE")" /etc/caddy/Caddyfile.d

if [[ ${1:-} == --undo ]]; then
	[[ -f $STATE ]] || die "no takeover recorded – nothing to undo"
	say "${c_b}Giving ports 80/443 back to Traefik${c_o}"
	while read -r kind val; do
		case $kind in
		file) rm -f "$val" && ok "removed $val" ;;
		container) docker update --restart=unless-stopped "$val" >/dev/null && ok "Traefik $val: restart policy back on" ;;
		esac
	done <"$STATE"
	systemctl stop caddy || true
	while read -r kind val; do [[ $kind == container ]] && docker start "$val" >/dev/null && ok "started $val"; done <"$STATE"
	rm -f "$STATE"
	warn "Caddy is stopped, so the CRM is reachable only after another web server routes it."
	return 0
fi

# ---------------------------------------------------------------- find Traefik and the sites it serves
TRAEFIK=$(docker ps --format '{{.ID}} {{.Image}} {{.Names}}' | awk 'tolower($2) ~ /traefik/ {print $3}')
[[ -n $TRAEFIK ]] || die "no running Traefik container found"
say "${c_b}Traefik container(s):${c_o} $TRAEFIK"

PLAN=$(docker inspect $(docker ps -q) | python3 -c '
import json, re, sys
out = []
for c in json.load(sys.stdin):
    name = c["Name"].lstrip("/")
    labels = c["Config"].get("Labels") or {}
    if "traefik" in c["Config"].get("Image", "").lower():
        continue
    hosts = []
    for k, v in labels.items():
        if re.match(r"traefik\.http\.routers\.[^.]+\.rule$", k):
            hosts += re.findall(r"Host\(\s*`([^`]+)`\s*\)", v)
    if not hosts:
        continue
    port = None
    for k, v in labels.items():
        if re.match(r"traefik\.http\.services\.[^.]+\.loadbalancer\.server\.port$", k):
            port = v.strip()
    if not port:
        exp = list((c["Config"].get("ExposedPorts") or {}).keys())
        port = exp[0].split("/")[0] if exp else None
    if not port:
        print("SKIP", name, ",".join(hosts), "no-port"); continue
    target = None
    for b in (c["NetworkSettings"].get("Ports") or {}).get(port + "/tcp") or []:
        if b.get("HostPort"):
            target = "127.0.0.1:" + b["HostPort"]; break
    if not target:
        for n in (c["NetworkSettings"].get("Networks") or {}).values():
            if n.get("IPAddress"):
                target = n["IPAddress"] + ":" + port + " container-ip"; break
    if not target:
        print("SKIP", name, ",".join(hosts), "unreachable"); continue
    for h in sorted(set(hosts)):
        print("SITE", name, h, target)
')
[[ -n $PLAN ]] || die "Traefik routes no sites with Host() rules – nothing to move"

say ""
say "${c_b}Plan${c_o}"
FILES=()
while read -r kind name host target extra; do
	case $kind in
	SKIP) warn "$name ($host): cannot be reached from the host ($target) – it would stop working; aborting"; exit 1 ;;
	SITE)
		say "  https://$host  →  $target   (container $name)"
		[[ ${extra:-} == container-ip ]] && warn "   $name publishes no port – Caddy uses its container IP, which can change when the container is re-created (run this script again then)"
		;;
	esac
done <<<"$PLAN"
say "  Traefik ($TRAEFIK) is stopped and will not start again on reboot (undo: sudo bash $0 --undo)"
say ""
local ans=""
read -r -p "  Switch now? (y/n) [y]: " ans </dev/tty || true
[[ ${ans:-y} =~ ^[Yy] ]] || die "cancelled – nothing changed"

# ---------------------------------------------------------------- write Caddy sites, validate
: >"$STATE"
while read -r kind name host target extra; do
	[[ $kind == SITE ]] || continue
	f="/etc/caddy/Caddyfile.d/proxy-${host//[^a-zA-Z0-9.-]/_}.caddy"
	cat >"$f" <<EOF
# Managed by Setu caddy-takeover.sh – was served by Traefik (container $name)
$host {
	encode zstd gzip
	reverse_proxy ${target%% *}
}
EOF
	echo "file $f" >>"$STATE"
	ok "Caddy site $host → ${target%% *}"
done <<<"$PLAN"
if ! log=$(caddy validate --adapter caddyfile --config /etc/caddy/Caddyfile 2>&1); then
	printf '%s\n' "$log" | tail -n 15 >&2
	while read -r kind val; do [[ $kind == file ]] && rm -f "$val"; done <"$STATE"
	rm -f "$STATE"
	die "Caddy rejected the configuration – nothing else was changed"
fi

# ---------------------------------------------------------------- switch
for t in $TRAEFIK; do
	docker update --restart=no "$t" >/dev/null
	docker stop "$t" >/dev/null
	echo "container $t" >>"$STATE"
	ok "stopped Traefik ($t) and turned off its automatic restart"
done
if ! systemctl restart caddy; then
	journalctl -u caddy --no-pager -n 12 | sed 's/^/      /' >&2
	warn "Caddy did not start – putting Traefik back"
	for t in $TRAEFIK; do docker update --restart=unless-stopped "$t" >/dev/null; docker start "$t" >/dev/null; done
	die "switch failed (see above); Traefik is running again"
fi
ok "Caddy is serving ports 80/443"

say ""
say "${c_b}Checking the sites (certificates can take up to a minute)${c_o}"
sleep 8
for f in /etc/caddy/Caddyfile.d/*.caddy; do
	h=$(awk '/^[a-zA-Z0-9.-]+ *\{/{print $1; exit}' "$f")
	[[ -n $h ]] || continue
	code=000
	for _ in 1 2 3 4 5 6; do
		code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 "https://$h/" || true)
		[[ $code != 000 ]] && break
		sleep 10
	done
	if [[ $code != 000 ]]; then ok "https://$h answers (HTTP $code)"; else warn "https://$h not reachable yet – check its DNS points to this VPS and the hPanel firewall allows 80/443; logs: journalctl -u caddy -n 50"; fi
done
say ""
say "${c_g}Done.${c_o} Caddy now serves every site. If you ever run 'docker compose up -d' for n8n again,"
say "remove the traefik service from that compose file first (or it would grab the ports back)."
}
main "$@"
