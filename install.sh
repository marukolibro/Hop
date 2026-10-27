#!/bin/sh
# The install script is made by `mygo build` and published with every release;
# this file only keeps the address of older instructions working. In a shell on
# Windows (Git Bash, MSYS2) it hands over to install.ps1.
#
# usage: curl -fsSL https://github.com/marukolibro/Hop/releases/latest/download/install.sh | sh
set -eu

release=https://github.com/marukolibro/Hop/releases/latest/download

case "$(uname -s)" in
MINGW* | MSYS* | CYGWIN*)
	command -v powershell.exe >/dev/null 2>&1 || { echo "hop install: powershell.exe is required" >&2; exit 1; }
	exec powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "irm $release/install.ps1 | iex"
	;;
esac

command -v curl >/dev/null 2>&1 || { echo "hop install: curl is required" >&2; exit 1; }
exec sh -c "curl -fsSL $release/install.sh | sh"
