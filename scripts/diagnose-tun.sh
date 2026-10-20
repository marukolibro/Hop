#!/bin/sh
# Diagnoses Hop's TUN mode (增强模式) on this Mac. It only reads: no sudo, no
# launchctl, no writes, nothing sent anywhere but test requests. The
# controller secret it reads to ask Hop's core questions is never printed.
#
# usage: scripts/diagnose-tun.sh          check once, now
#        scripts/diagnose-tun.sh --wait   check once, then wait (up to 15
#                                         minutes) until the helper runs a
#                                         core with TUN on, and check again
set -u

LABEL=io.github.marukolibro.hop.helper
SOCK=/var/run/$LABEL.sock
PLIST=/Library/LaunchDaemons/$LABEL.plist
TOOL=/Library/PrivilegedHelperTools/$LABEL
HELPER_HOME="/Library/Application Support/$LABEL"
APP_DIR="$HOME/Library/Application Support/hop"
CONTROLLER=127.0.0.1:19090
HOP_DEVICE=utun199
# The resolvers the helper sets while TUN is on (cmd/hop-helper managedResolvers).
MANAGED_RESOLVERS="223.5.5.5 119.29.29.29 114.114.114.114"
WAIT_LIMIT=900

passes=0
fails=0
warns=0
pass() { passes=$((passes + 1)); printf '  PASS  %s\n' "$1"; }
fail() { fails=$((fails + 1)); printf '  FAIL  %s\n' "$1"; }
warn() { warns=$((warns + 1)); printf '  WARN  %s\n' "$1"; }
info() { printf '  INFO  %s\n' "$1"; }
section() { printf '\n== %s\n' "$1"; }

secret() {
	if [ -r "$APP_DIR/controller-secret" ]; then
		tr -d '[:space:]' <"$APP_DIR/controller-secret"
	fi
}

controller() {
	curl -s -m 5 -H "Authorization: Bearer $(secret)" "http://$CONTROLLER$1"
}

helper() {
	curl -s -m 5 --unix-socket "$SOCK" "http://helper$1"
}

json() {
	# json <key path like tun.enable>: reads stdin
	python3 -c 'import json,sys
d=json.load(sys.stdin)
for k in sys.argv[1].split("."):
    d=d.get(k) if isinstance(d,dict) else None
print("" if d is None else d)' "$1" 2>/dev/null
}

route_iface() {
	route -n get "$1" 2>/dev/null | awk '/interface:/{print $2}'
}

helper_core_running() {
	[ "$(helper /status | json running)" = "True" ]
}

check_app() {
	section "Hop and its core"
	pid=$(pgrep -f 'Hop( Dev)?\.app/Contents/MacOS/Hop' | head -n 1)
	if [ -n "$pid" ]; then pass "Hop runs (pid $pid)"; else fail "Hop does not run"; fi
	version=$(controller /version | json version)
	if [ -n "$version" ]; then pass "controller answers on $CONTROLLER ($version)"; else fail "controller does not answer on $CONTROLLER"; fi
	configs=$(controller /configs)
	info "mode=$(echo "$configs" | json mode) tun.enable=$(echo "$configs" | json tun.enable) tun.device=$(echo "$configs" | json tun.device) tun.stack=$(echo "$configs" | json tun.stack)"
}

check_helper() {
	section "Privileged helper"
	for f in "$TOOL" "$PLIST"; do
		if [ -e "$f" ]; then
			info "$f: $(stat -f '%Su:%Sg %Lp' "$f")"
		else
			info "$f: not installed"
		fi
	done
	if [ -e "$PLIST" ]; then
		if lint=$(plutil -lint "$PLIST" 2>&1); then pass "plist lints"; else fail "plist does not lint: $lint"; fi
	fi
	if [ -d "$HELPER_HOME" ]; then info "helper home: $(ls "$HELPER_HOME" | tr '\n' ' ')"; fi
	if [ ! -S "$SOCK" ]; then
		info "no helper socket at $SOCK (not installed, or launchd did not start it)"
		return
	fi
	v=$(helper /version | json version)
	if [ -n "$v" ]; then pass "helper answers (protocol $v)"; else fail "helper socket exists but does not answer"; fi
	status=$(helper /status)
	info "helper status: $status"
	info "dns managed: $(echo "$status" | json dnsManaged)"
	if [ "$(echo "$status" | json running)" = "True" ]; then
		core_pid=$(echo "$status" | json pid)
		if [ -n "$core_pid" ]; then info "helper core: $(ps -o user=,pid=,etime= -p "$core_pid" 2>/dev/null)"; fi
	fi
}

check_routes() {
	section "Routes"
	via=$(route_iface 1.1.1.1)
	info "1.1.1.1 routes via $via"
	others=$(netstat -rn -f inet | awk '$1 ~ /^(default|0\/1|128\.0\/1|1|2\/7|4\/6|8\/5|16\/4|32\/3|64\/2)$/ && $4 ~ /^utun/ {print $4}' | sort -u | tr '\n' ' ')
	info "utun interfaces holding default-ish routes: ${others:-none}"
	if ! helper_core_running; then
		for other in $others; do
			[ "$other" = "$HOP_DEVICE" ] || warn "another TUN holds the routes ($other): Hop refuses 增强模式 until it is turned off in that app"
		done
	fi
	if helper_core_running; then
		if [ "$via" = "$HOP_DEVICE" ]; then pass "traffic routes through Hop's TUN ($HOP_DEVICE)"; else fail "TUN is on but 1.1.1.1 routes via $via, not $HOP_DEVICE"; fi
		for other in $others; do
			[ "$other" = "$HOP_DEVICE" ] || warn "another TUN also holds routes: $other"
		done
		gw=$(netstat -rn -f inet | awk '$1=="default" && $4 !~ /^utun/ {print $2; exit}')
		if [ -n "$gw" ]; then
			gw_via=$(route_iface "$gw")
			case "$gw_via" in
			utun*) fail "the LAN gateway $gw routes into $gw_via: local networks are not excluded" ;;
			*) pass "the LAN gateway $gw stays on $gw_via" ;;
			esac
		fi
	fi
}

check_dns() {
	section "DNS"
	resolvers=$(scutil --dns | awk '/nameserver\[[0-9]+\]/{print $3}' | sort -u | tr '\n' ' ')
	info "system resolvers: ${resolvers:-none}"
	backup="$HELPER_HOME/dns-backup.json"
	managed=yes
	for r in $MANAGED_RESOLVERS; do
		case " $resolvers" in
		*" $r "*) ;;
		*) managed=no ;;
		esac
	done
	if helper_core_running; then
		if [ "$managed" = yes ]; then
			pass "the system DNS is Hop's managed resolvers ($MANAGED_RESOLVERS)"
		else
			fail "TUN is on but the system DNS is ${resolvers:-none}, not Hop's managed resolvers"
		fi
		for host in www.google.com github.com www.baidu.com; do
			addr=$(dscacheutil -q host -a name "$host" | awk '/ip_address/{print $2; exit}')
			info "$host -> ${addr:-no answer}"
			if awk -v h="$host" '$1 !~ /^#/ { for (i = 2; i <= NF; i++) if ($i == h) found = 1 } END { exit !found }' /etc/hosts; then
				# The core answers names in /etc/hosts from the file, as the
				# system would: a real address there is the user's choice.
				info "$host is pinned in /etc/hosts; the core answers it from there"
				continue
			fi
			case "$addr" in
			198.18.*) pass "$host gets a fake IP from Hop's DNS" ;;
			"") fail "$host does not resolve" ;;
			*) fail "$host resolved to a real address under TUN: the system DNS is not entering the TUN" ;;
			esac
		done
		return
	fi
	if [ "$managed" = yes ] && [ -e "$backup" ]; then
		warn "TUN is off but the system DNS is still Hop's ($MANAGED_RESOLVERS) and a backup exists: the restore did not run"
	fi
	for host in www.google.com github.com www.baidu.com; do
		addr=$(dscacheutil -q host -a name "$host" | awk '/ip_address/{print $2; exit}')
		info "$host -> ${addr:-no answer}"
	done
}

check_traffic() {
	section "Traffic without any proxy variables (what TUN must carry)"
	for url in https://www.gstatic.com/generate_204 https://www.google.com/ https://github.com/ https://www.youtube.com/ https://www.baidu.com/; do
		results=""
		for i in 1 2 3; do
			r=$(env -u http_proxy -u https_proxy -u all_proxy -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY \
				curl -s -o /dev/null --noproxy '*' -m 10 -w '%{http_code}:%{time_appconnect}:%{time_total}' "$url")
			results="$results $r"
		done
		info "$url  code:tls:total =$results"
		case "$results" in
		*" 000:"*) fail "$url failed at least once" ;;
		*) pass "$url answered three times" ;;
		esac
	done
}

check_proxy() {
	section "System proxy"
	info "$(scutil --proxy | awk '/Enable|Proxy |Port/{printf "%s=%s ", $1, $3}')"
}

report() {
	section "Summary"
	printf '  %d passed, %d failed, %d warnings\n' "$passes" "$fails" "$warns"
}

run_all() {
	check_app
	check_helper
	check_routes
	check_dns
	check_proxy
	if helper_core_running; then check_traffic; fi
}

printf 'Hop TUN diagnosis, %s\n' "$(date '+%Y-%m-%d %H:%M:%S')"
run_all
report

if [ "${1:-}" = "--wait" ] && ! helper_core_running; then
	printf '\nWaiting up to %ds for the helper to run a core with TUN on (click 增强模式 in Hop)...\n' "$WAIT_LIMIT"
	waited=0
	while [ "$waited" -lt "$WAIT_LIMIT" ]; do
		sleep 5
		waited=$((waited + 5))
		if helper_core_running && [ "$(route_iface 1.1.1.1)" = "$HOP_DEVICE" ]; then
			printf '\nTUN came up after %ds; checking again.\n' "$waited"
			passes=0 fails=0 warns=0
			sleep 3
			run_all
			report
			exit "$fails"
		fi
		[ $((waited % 30)) -eq 0 ] && printf '  still waiting (%ds): helper socket %s, routes via %s\n' \
			"$waited" "$([ -S "$SOCK" ] && echo present || echo absent)" "$(route_iface 1.1.1.1)"
	done
	printf '\nTUN did not come up within %ds.\n' "$WAIT_LIMIT"
	exit 1
fi
exit "$fails"
