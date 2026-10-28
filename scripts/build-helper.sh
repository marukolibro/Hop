#!/bin/sh
# Builds Hop's privileged helper (cmd/hop-helper) into
# resources/<platform>/bin, beside the bundled mihomo core, so mygo dev and
# mygo build ship and sign it with the app. TUN mode needs it: an app built
# without one turns TUN on only as far as saying what is missing. The
# helper is stamped with the bundled core's release from mihomo.version: it
# installs no older core.
#
# usage: scripts/build-helper.sh darwin-arm64 | darwin-amd64 | windows-amd64
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=../mihomo.version
. "$root/mihomo.version"

if [ "$#" -ne 1 ]; then
	echo "usage: $0 darwin-arm64 | darwin-amd64 | windows-amd64" >&2
	exit 2
fi
platform=$1

case "$platform" in
darwin-arm64) goos=darwin; goarch=arm64; name=hop-helper ;;
darwin-amd64) goos=darwin; goarch=amd64; name=hop-helper ;;
windows-amd64) goos=windows; goarch=amd64; name=hop-helper.exe ;;
*)
	echo "usage: $0 darwin-arm64 | darwin-amd64 | windows-amd64" >&2
	exit 2
	;;
esac

out=$root/resources/$platform/bin/$name
GOOS=$goos GOARCH=$goarch go build -trimpath -ldflags "-X main.bundledCoreVersion=$MIHOMO_VERSION" -o "$out" ./cmd/hop-helper
echo "built $out"
