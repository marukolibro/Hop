#!/bin/sh
# Measures what a TUN costs, so Hop's TUN and another client's can be
# compared on the same machine and the same exit node. It only reads and
# sends test requests: no sudo, no launchctl, no settings changed.
#
# usage: scripts/bench-tun.sh <label> [--wait-other <iface>]
#   <label>               names the run in the output, e.g. hop or verge
#   --wait-other <iface>  first wait (up to 15 minutes) until the default
#                         routes move to a utun other than <iface>, i.e. the
#                         user switched to the other client's TUN
set -u

RUNS=8
TARGETS="https://www.gstatic.com/generate_204 https://www.google.com/ https://www.youtube.com/ https://github.com/ https://chatgpt.com/ https://www.baidu.com/"
WAIT_LIMIT=900

if [ "$#" -lt 1 ]; then
	echo "usage: $0 <label> [--wait-other <iface>]" >&2
	exit 2
fi
label=$1

route_iface() {
	route -n get 1.1.1.1 2>/dev/null | awk '/interface:/{print $2}'
}

if [ "${2:-}" = "--wait-other" ]; then
	from=${3:?--wait-other needs an interface}
	printf 'Waiting up to %ds for the routes to leave %s for another TUN...\n' "$WAIT_LIMIT" "$from"
	waited=0
	while :; do
		now=$(route_iface)
		case "$now" in
		utun*) [ "$now" != "$from" ] && break ;;
		esac
		[ "$waited" -ge "$WAIT_LIMIT" ] && { echo "the routes never moved (still $now)"; exit 1; }
		[ $((waited % 30)) -eq 0 ] && printf '  still waiting (%ds): routes via %s\n' "$waited" "$now"
		sleep 5
		waited=$((waited + 5))
	done
	printf 'Routes now via %s; letting it settle.\n' "$now"
	sleep 10
fi

nocurl() {
	env -u http_proxy -u https_proxy -u all_proxy -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY \
		curl --noproxy '*' "$@"
}

printf '\n# bench %s, %s\n' "$label" "$(date '+%Y-%m-%d %H:%M:%S')"
printf 'routes via: %s\n' "$(route_iface)"
printf 'system resolvers: %s\n' "$(scutil --dns | awk '/nameserver\[0\]/{print $3}' | sort -u | tr '\n' ' ')"
trace=$(nocurl -s -m 10 https://www.cloudflare.com/cdn-cgi/trace)
printf 'exit: ip=%s colo=%s loc=%s\n' \
	"$(echo "$trace" | awk -F= '$1=="ip"{print $2}')" \
	"$(echo "$trace" | awk -F= '$1=="colo"{print $2}')" \
	"$(echo "$trace" | awk -F= '$1=="loc"{print $2}')"

printf '\nmedians over %d fresh connections, milliseconds (failures counted apart)\n' "$RUNS"
printf '%-40s %6s %6s %6s %6s %6s %5s\n' target dns tcp tls ttfb total fail
for url in $TARGETS; do
	samples=""
	for i in $(seq 1 "$RUNS"); do
		s=$(nocurl -s -o /dev/null -m 10 -w '%{http_code} %{time_namelookup} %{time_connect} %{time_appconnect} %{time_starttransfer} %{time_total}' "$url")
		samples="$samples$s
"
	done
	printf '%s' "$samples" | python3 -c '
import sys, statistics
url = sys.argv[1]
rows = [l.split() for l in sys.stdin if l.strip()]
ok = [[float(x) * 1000 for x in r[1:]] for r in rows if r[0] != "000"]
fail = len(rows) - len(ok)
if not ok:
    dash = "-"
    print(f"{url:40s} {dash:>6} {dash:>6} {dash:>6} {dash:>6} {dash:>6} {fail:5d}")
    sys.exit()
# curl reports cumulative times; each phase is the step from the previous.
cols = list(zip(*ok))
med = [statistics.median(c) for c in cols]
dns, tcp, tls, ttfb, total = med[0], med[1] - med[0], med[2] - med[1], med[3] - med[2], med[4]
print(f"{url:40s} {dns:6.0f} {tcp:6.0f} {tls:6.0f} {ttfb:6.0f} {total:6.0f} {fail:5d}")
' "$url"
done
