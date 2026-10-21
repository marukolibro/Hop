#!/bin/sh
# Builds Hop's signed release for Apple silicon. --dry-run builds and lists
# the files; --publish also uploads them as a draft release and prints the
# command that publishes it. The user reviews the draft, then runs that
# command. The update private key is required: from MYGO_UPDATER_PRIVATE_KEY,
# or from the path in mygo.json's updates.privateKey. The key never appears
# in the repository or this output.
#
# It builds one universal app: mygo combines the arm64 and amd64 cores and
# helpers, both fetched and built here.
#
# usage: scripts/release.sh --dry-run | --publish
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

die() {
	echo "release: $*" >&2
	exit 1
}

if [ "$#" -ne 1 ]; then
	echo "usage: $0 --dry-run | --publish" >&2
	exit 2
fi
case "$1" in
--dry-run | --publish) mode=$1 ;;
*)
	echo "usage: $0 --dry-run | --publish" >&2
	exit 2
	;;
esac

command -v jq >/dev/null 2>&1 || die "jq is required"

version=$(jq -r '.version // empty' mygo.json)
[ -n "$version" ] || die "mygo.json has no version"
github=$(jq -r '.updates.github // empty' mygo.json)
[ -n "$github" ] || die "mygo.json has no updates.github"
tag="v$version"

grep -Fqx "## $version" CHANGELOG.md || die "CHANGELOG.md has no \"## $version\" section"
notes=$(awk -v v="$version" '
	$0 == "## " v { inside = 1; next }
	inside && /^## / { exit }
	inside { print }
' CHANGELOG.md)
[ -n "$(printf '%s' "$notes" | tr -d '[:space:]')" ] || die "the CHANGELOG.md section for $version is empty"

[ -z "$(git status --porcelain)" ] || die "the working tree has changes; commit or stash them first"
if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
	die "tag $tag already exists; bump the version or remove the tag"
fi
if command -v gh >/dev/null 2>&1 && gh release view "$tag" --repo "$github" >/dev/null 2>&1; then
	die "release $tag already exists on GitHub; bump the version"
fi

if [ -z "${MYGO_UPDATER_PRIVATE_KEY:-}" ]; then
	key=$(jq -r '.updates.privateKey // empty' mygo.json)
	[ -n "$key" ] || die "mygo.json has no updates.privateKey and MYGO_UPDATER_PRIVATE_KEY is not set"
	case "$key" in
	"~/"*) key="$HOME/${key#"~/"}" ;;
	/*) ;;
	*) key="$root/$key" ;;
	esac
	[ -f "$key" ] || die "the update private key is missing (updates.privateKey in mygo.json)"
fi

# The key signs the archive and manifest; mygo needs it to make the update.
scripts/fetch-mihomo.sh darwin-arm64
scripts/fetch-mihomo.sh darwin-amd64
scripts/build-helper.sh darwin-arm64
scripts/build-helper.sh darwin-amd64

if [ "$mode" = --dry-run ]; then
	go tool mygo build -platform darwin/universal -skip-dmg
else
	go tool mygo build -platform darwin/universal -skip-dmg -upload
fi

out=$(jq -r '.out // "build"' mygo.json)
dir="$root/$out/darwin-universal"
app="$dir/Hop.app"
archive=$(find "$dir" -maxdepth 1 -name '*.tar.gz' -print -quit)
manifest="$dir/update-darwin-universal.json"
[ -d "$app" ] || die "the build made no Hop.app"
[ -n "$archive" ] || die "the build made no update archive"
[ -f "$manifest" ] || die "the build made no update manifest"
manifest_version=$(plutil -extract version raw -o - "$manifest")
[ "$manifest_version" = "$version" ] || die "the update manifest is for $manifest_version, not $version"
[ -n "$(plutil -extract signature raw -o - "$manifest")" ] || die "the update archive is not signed; check the update private key"

echo "Built Hop $version"
echo "  $app"
echo "  $archive"
echo "  $manifest"

if [ "$mode" = --dry-run ]; then
	echo "Dry run: nothing was uploaded."
	exit 0
fi

echo "The draft release $tag is uploaded. Review it, then publish with:"
echo "  gh release edit $tag --draft=false"
