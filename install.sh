#!/bin/sh
# Installs or updates Hop from the latest signed release: one universal app
# for Apple silicon and Intel Macs. In a shell on Windows (Git Bash, MSYS2) it
# hands over to install.ps1.
# The manifest carries the archive's size and an Ed25519 signature, but no
# SHA-256. Verifying Ed25519 in POSIX sh is not practical, so this script
# checks the size and relies on HTTPS for the download; the app's own updater
# verifies the signature of every later update. It also checks the extracted
# app's version and code signature before replacing anything.
#
# Testing on a local build: HOP_UPDATE_BASE points at a directory (or URL)
# holding update-darwin-universal.json and its archive; HOP_INSTALL_DIR changes
# where Hop.app is installed.
#
# usage: curl -fsSL https://raw.githubusercontent.com/marukolibro/Hop/main/install.sh | sh
set -eu

repo=marukolibro/Hop
base=${HOP_UPDATE_BASE:-https://github.com/$repo/releases/latest/download}
install_dir=${HOP_INSTALL_DIR:-}
target=

die() {
	echo "hop install: $*" >&2
	exit 1
}

say() {
	echo "$*"
}

case "$(uname -s)" in
Darwin) ;;
MINGW* | MSYS* | CYGWIN*)
	# A shell on Windows (Git Bash, MSYS2): the Windows installer is a
	# PowerShell script.
	command -v powershell.exe >/dev/null 2>&1 || die "powershell.exe is required"
	exec powershell.exe -NoProfile -ExecutionPolicy Bypass -Command \
		"irm https://raw.githubusercontent.com/$repo/main/install.ps1 | iex"
	;;
*) die "Hop 只支持 macOS 和 Windows" ;;
esac

command -v curl >/dev/null 2>&1 || die "curl is required"
command -v plutil >/dev/null 2>&1 || die "plutil is required"
command -v tar >/dev/null 2>&1 || die "tar is required"

if [ -z "$install_dir" ]; then
	if [ -w /Applications ]; then
		install_dir=/Applications
	else
		install_dir=$HOME/Applications
	fi
fi
target="$install_dir/Hop.app"

work=$(mktemp -d "${TMPDIR:-/tmp}/hop-install.XXXXXX")
trap 'rm -rf "$work"' EXIT
umask 077

manifest="$work/manifest.json"
curl -fsSL "$base/update-darwin-universal.json" -o "$manifest" 2>/dev/null \
	|| die "could not read the latest update manifest"

version=$(plutil -extract version raw -o - "$manifest" 2>/dev/null) || die "the update manifest is not valid JSON"
archive_url=$(plutil -extract url raw -o - "$manifest" 2>/dev/null) || die "the update manifest has no archive"
archive_size=$(plutil -extract size raw -o - "$manifest" 2>/dev/null) || die "the update manifest has no size"
[ -n "$version" ] || die "the update manifest has no version"
[ -n "$archive_url" ] || die "the update manifest has no archive"
[ -n "$archive_size" ] || die "the update manifest has no size"

if pgrep -x 'Hop Dev' >/dev/null 2>&1; then
	die "Hop Dev 正在运行,请先退出开发版再安装发布版"
fi

# compare_versions prints -1, 0 or 1 for $1 against $2.
compare_versions() {
	awk -v a="$1" -v b="$2" 'BEGIN {
		na = split(a, aa, "."); nb = split(b, bb, ".");
		for (i = 1; i <= na || i <= nb; i++) {
			x = i <= na ? aa[i] + 0 : 0;
			y = i <= nb ? bb[i] + 0 : 0;
			if (x < y) { print -1; exit }
			if (x > y) { print 1; exit }
		}
		print 0
	}'
}

current=
if [ -d "$target" ]; then
	current=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$target/Contents/Info.plist" 2>/dev/null || true)
fi
if [ -n "$current" ] && [ "$(compare_versions "$version" "$current")" -le 0 ]; then
	say "Hop $current 已安装,未降级到 ${version}。"
	open "$target" 2>/dev/null || true
	exit 0
fi

archive="$work/update.tar.gz"
say "正在下载 Hop $version..."
curl -fL --silent --show-error -o "$archive" "$archive_url" || die "could not download the update"
actual_size=$(stat -f%z "$archive")
[ "$actual_size" = "$archive_size" ] || die "the update's size does not match its manifest"

mkdir "$work/unpacked"
tar -xzf "$archive" -C "$work/unpacked" || die "the update archive is not readable"
app=$(find "$work/unpacked" -maxdepth 3 -type d -name 'Hop.app' -print -quit)
[ -n "$app" ] || die "the update archive holds no Hop.app"
app_version=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$app/Contents/Info.plist" 2>/dev/null || true)
[ "$app_version" = "$version" ] || die "the app in the update is not version $version"
if command -v codesign >/dev/null 2>&1; then
	codesign --verify --deep --strict "$app" >/dev/null 2>&1 || die "the app's code signature is damaged"
fi

if pgrep -x Hop >/dev/null 2>&1; then
	say "正在退出 Hop..."
	osascript -e 'tell application id "io.github.marukolibro.hop" to quit' >/dev/null 2>&1 || true
	i=0
	while pgrep -x Hop >/dev/null 2>&1; do
		i=$((i + 1))
		[ "$i" -lt 20 ] || die "Hop did not quit; quit it and run this again"
		sleep 1
	done
fi

mkdir -p "$install_dir"
backup="$target.previous"
rm -rf "$backup"
if [ -d "$target" ]; then
	mv "$target" "$backup" || die "could not move the installed app aside"
fi
if ! ditto "$app" "$target"; then
	rm -rf "$target"
	if [ -d "$backup" ]; then
		mv "$backup" "$target"
	fi
	die "could not install Hop"
fi
rm -rf "$backup"
xattr -dr com.apple.quarantine "$target" 2>/dev/null || true
say "已安装 Hop $version 到 ${install_dir}。"
open "$target" 2>/dev/null || true
