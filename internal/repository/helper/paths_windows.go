//go:build windows

package helper

import (
	"os"
	"path/filepath"
)

// ServiceName is the Windows service the helper runs as, LocalSystem.
const ServiceName = "HopHelper"

// installRoot is %ProgramFiles%, where the helper and its core live. The
// service's own files are admin-only: the core's config carries the
// subscription's nodes and the controller secret, and an ordinary user has
// no reason to change either. Program Files is chosen over ProgramData,
// which every user may create files in by default; the one directory users
// must reach (run, for the socket) gets its own ACL, installed below.
func installRoot() string {
	if dir := os.Getenv("ProgramFiles"); dir != "" {
		return dir
	}
	drive := os.Getenv("SystemDrive")
	if drive == "" {
		drive = `C:`
	}
	return filepath.Join(drive+string(filepath.Separator), "Program Files")
}

var (
	// InstallDir is the directory the helper installs itself, its core and
	// its home into.
	InstallDir = filepath.Join(installRoot(), "Hop", "helper")
	// HomeDir is the helper's own home: the core's config, pid file and
	// geodata, admin-only.
	HomeDir = filepath.Join(InstallDir, "home")
	// CorePath is the mihomo binary the helper starts.
	CorePath = filepath.Join(InstallDir, "bin", "mihomo.exe")
	// ToolPath is where the helper binary itself is installed.
	ToolPath = filepath.Join(InstallDir, "hop-helper.exe")
	// SocketPath is the AF_UNIX socket Hop talks to the helper on. Its
	// directory's ACL is what replaces macOS's peer-credential check.
	SocketPath = filepath.Join(InstallDir, "run", "hop-helper.sock")
)

// InstallLogPath is the fixed record of an elevated install or uninstall,
// readable by the installing user: Hop shows it when the elevated helper
// exits with an error, and the path is fixed so the elevated process is
// never made to write anywhere a caller chooses.
func InstallLogPath() string {
	root := os.Getenv("ProgramData")
	if root == "" {
		root = `C:\ProgramData`
	}
	return filepath.Join(root, "Hop", "helper-install.log")
}
