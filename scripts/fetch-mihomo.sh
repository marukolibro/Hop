#!/bin/sh
# Downloads what Hop ships of the mihomo core into resources/: the core for
# one platform into resources/<platform>/bin, where `mygo dev` and
# `mygo build` pick it up, the geodata databases the generated configs can
# need into resources/geodata, and the core's license (GPL-3.0) beside the binary.
# Versions and hashes are pinned in mihomo.version. It only downloads and
# unpacks: it never starts the core and touches nothing outside this
# repository.
#
# usage: scripts/fetch-mihomo.sh darwin-arm64 | darwin-amd64 | windows-amd64
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
darwin-arm64)
	asset="mihomo-darwin-arm64-$MIHOMO_VERSION.gz"
	want=$MIHOMO_SHA256_DARWIN_ARM64
	binary=mihomo
	;;
darwin-amd64)
	asset="mihomo-darwin-amd64-compatible-$MIHOMO_VERSION.gz"
	want=$MIHOMO_SHA256_DARWIN_AMD64
	binary=mihomo
	;;
windows-amd64)
	asset="mihomo-windows-amd64-compatible-$MIHOMO_VERSION.zip"
	want=$MIHOMO_SHA256_WINDOWS_AMD64
	binary=mihomo.exe
	;;
*)
	echo "unsupported platform: $platform (want darwin-arm64, darwin-amd64 or windows-amd64)" >&2
	exit 2
	;;
esac

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# sha256_of prints a file's SHA-256: macOS has shasum, Git Bash on Windows
# has sha256sum.
sha256_of() {
	if command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d' ' -f1
	else
		sha256sum "$1" | cut -d' ' -f1
	fi
}

# fetch_pinned downloads url into dest when its SHA-256 is want.
fetch_pinned() {
	url=$1
	want=$2
	out=$3
	name=${out##*/}

	echo "downloading $url"
	curl --fail --location --silent --show-error --output "$work/$name" "$url"
	got=$(sha256_of "$work/$name")
	if [ "$got" != "$want" ]; then
		echo "sha256 mismatch for $name: got $got, want $want" >&2
		exit 1
	fi
	mkdir -p "${out%/*}"
	mv "$work/$name" "$out"
}

dest="$root/resources/$platform/bin"
mkdir -p "$dest"
fetch_pinned \
	"https://github.com/MetaCubeX/mihomo/releases/download/$MIHOMO_VERSION/$asset" \
	"$want" "$work/core-archive"

case "$asset" in
*.gz)
	gunzip --stdout "$work/core-archive" >"$dest/$binary"
	chmod 755 "$dest/$binary"
	;;
*.zip)
	unzip -o -q "$work/core-archive" -d "$work/unpacked"
	# The archive holds one executable, named after the asset.
	found=$(find "$work/unpacked" -type f -name '*.exe' | head -n 1)
	if [ -z "$found" ]; then
		echo "no executable inside $asset" >&2
		exit 1
	fi
	cp "$found" "$dest/$binary"
	;;
esac
echo "installed $dest/$binary ($MIHOMO_VERSION)"

# The core's LICENSE, as it stands at the pinned tag, ships beside it: the
# file of the tree the bundled binary was built from is the one that governs
# it (GPL-3.0 for v1.19.33; the project's main branch carries an MIT text
# instead, which the release tags do not).
curl --fail --location --silent --show-error \
	--output "$dest/LICENSE" \
	"https://raw.githubusercontent.com/MetaCubeX/mihomo/$MIHOMO_VERSION/LICENSE"
echo "installed $dest/LICENSE"

# Geodata, under the names the core looks for in its home dir.
fetch_pinned "https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/$GEODATA_COMMIT/geosite.dat" \
	"$GEODATA_SHA256_GEOSITE_DAT" "$root/resources/geodata/GeoSite.dat"
fetch_pinned "https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/$GEODATA_COMMIT/geoip.metadb" \
	"$GEODATA_SHA256_GEOIP_METADB" "$root/resources/geodata/geoip.metadb"
echo "installed $root/resources/geodata (GeoSite.dat, geoip.metadb)"
