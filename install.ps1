# Installs or updates Hop on Windows from the latest release: the same
# installer as the release page, run silently for the current user (no
# administrator rights).
# The update manifest carries no checksum of the installer, so this script
# relies on HTTPS for the download and checks that the installer is an
# executable and that the version it installed is the one it asked for. The
# app's own updater verifies the signature of every later update.
#
# Testing on a local build: HOP_UPDATE_BASE points at a URL holding
# update-windows-amd64.json.
#
# usage: irm https://raw.githubusercontent.com/marukolibro/Hop/main/install.ps1 | iex

function Install-Hop {
	$ErrorActionPreference = 'Stop'
	$ProgressPreference = 'SilentlyContinue'
	[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

	if (-not [Environment]::Is64BitOperatingSystem) {
		throw 'Hop supports 64-bit Windows only'
	}
	if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64' -or $env:PROCESSOR_ARCHITEW6432 -eq 'ARM64') {
		Write-Host 'This is an ARM PC: Hop is built for x64 and runs through Windows emulation.'
	}

	$repo = 'marukolibro/Hop'
	$base = $env:HOP_UPDATE_BASE
	if (-not $base) {
		$base = "https://github.com/$repo/releases/latest/download"
	}
	$uninstallKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\io.github.marukolibro.hop'

	try {
		$manifest = Invoke-RestMethod -Uri "$base/update-windows-amd64.json"
	} catch {
		throw "could not read the latest update manifest: $($_.Exception.Message)"
	}
	if (-not $manifest.version -or -not $manifest.url) {
		throw 'the update manifest has no version or archive'
	}
	$version = [string]$manifest.version

	$current = $null
	if (Test-Path $uninstallKey) {
		$current = (Get-ItemProperty $uninstallKey).DisplayVersion
	}
	if ($current -and ([version]$version -le [version]$current)) {
		Write-Host "Hop $current is already installed; not downgrading to $version."
		return
	}

	# The installer sits beside the update archive. MyGo 0.7 renamed it from
	# "Hop Setup <version> amd64.exe" (dotted by GitHub) to "hop-setup-<version>-amd64.exe";
	# 0.4.12 is the last release with the old name.
	if ([version]$version -le [version]'0.4.12') {
		$installerName = "Hop.Setup.$version.amd64.exe"
	} else {
		$installerName = "hop-setup-$version-amd64.exe"
	}
	$installerUrl = ($manifest.url -replace '/[^/]+$', '') + "/$installerName"
	$work = Join-Path ([IO.Path]::GetTempPath()) ("hop-install-" + [Guid]::NewGuid().ToString('N'))
	New-Item -ItemType Directory -Path $work | Out-Null
	try {
		$installer = Join-Path $work 'Hop-Setup.exe'
		Write-Host "Downloading Hop $version..."
		try {
			Invoke-WebRequest -Uri $installerUrl -OutFile $installer -UseBasicParsing
		} catch {
			throw "could not download $installerUrl : $($_.Exception.Message)"
		}
		$bytes = [IO.File]::ReadAllBytes($installer)
		if ($bytes.Length -lt 1MB -or $bytes[0] -ne 0x4D -or $bytes[1] -ne 0x5A) {
			throw 'the download is not a Windows installer'
		}

		$running = Get-Process -Name 'Hop' -ErrorAction SilentlyContinue
		if ($running) {
			# Closing the window lets Hop run its own quit work (it puts the system
			# proxy back); never kill it, which would leave that proxy set.
			Write-Host 'Quitting Hop...'
			$running | ForEach-Object { [void]$_.CloseMainWindow() }
			$running | ForEach-Object { [void]$_.WaitForExit(20000) }
			if (Get-Process -Name 'Hop' -ErrorAction SilentlyContinue) {
				throw 'Hop did not quit; quit it from its tray icon and run this again'
			}
		}

		Write-Host 'Installing...'
		$install = Start-Process -FilePath $installer -ArgumentList '/S' -Wait -PassThru
		if ($install.ExitCode -ne 0) {
			throw "the installer failed with exit code $($install.ExitCode)"
		}
	} finally {
		Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue
	}

	$installed = $null
	if (Test-Path $uninstallKey) {
		$installed = (Get-ItemProperty $uninstallKey).DisplayVersion
	}
	if ($installed -ne $version) {
		throw "the installer finished, but Hop $version is not the installed version (found: $installed)"
	}
	$exe = Join-Path (Get-ItemProperty $uninstallKey).InstallLocation 'Hop.exe'
	Write-Host "Installed Hop $version to $(Split-Path $exe)."
	Start-Process -FilePath $exe
}

Install-Hop
