// Package helper talks to Hop's privileged helper (cmd/hop-helper): the
// root-run process that owns the TUN core, over a unix socket, and installs
// and removes its LaunchDaemon.
package helper

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"hop/internal/model"
)

// Version is the helper's own version, what GET /version reports. It moves
// only when the socket protocol changes; version 2 added the DNS takeover
// and the dashboards' dnsManaged.
const Version = "5"

// Production paths, as the LaunchDaemon runs the helper. Every one of them
// reaches the helper as a flag, so tests can run it unprivileged in temp
// dirs instead.
const (
	// SocketPath is the unix socket Hop talks to the helper on.
	SocketPath = "/var/run/io.github.marukolibro.hop.helper.sock"
	// HomeDir holds the helper's own files: the core's config, pid file and
	// geodata.
	HomeDir = "/Library/Application Support/io.github.marukolibro.hop.helper"
	// CorePath is the mihomo binary the helper starts.
	CorePath = HomeDir + "/mihomo"
	// ToolPath is where the helper binary itself is installed.
	ToolPath = "/Library/PrivilegedHelperTools/io.github.marukolibro.hop.helper"
	// Label is the LaunchDaemon's job label.
	Label = "io.github.marukolibro.hop.helper"
	// PlistPath is the LaunchDaemon's definition.
	PlistPath = "/Library/LaunchDaemons/io.github.marukolibro.hop.helper.plist"
)

// BundledToolPath locates the helper binary that ships with the app: in the
// packaged resources' bin, or in the platform directory of the source tree
// (scripts/build-helper.sh puts it there). An app built without one has no
// TUN: enabling it then fails, naming what is missing.
func BundledToolPath(resourcesDir string) (string, error) {
	candidates := []string{
		filepath.Join(resourcesDir, "bin", "hop-helper"),
		filepath.Join(resourcesDir, runtime.GOOS+"-"+runtime.GOARCH, "bin", "hop-helper"),
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("the app ships no hop-helper (tried %v); build it with scripts/build-helper.sh", candidates)
}

// Status is what GET /status reports about the core the helper runs.
type Status struct {
	Running bool `json:"running"`
	// Pid is the core's process id, 0 when none runs.
	Pid int `json:"pid"`
	// DNSManaged says whether the helper holds the system DNS now: it points
	// every enabled network service at its own resolvers while a TUN core
	// runs.
	DNSManaged bool `json:"dnsManaged"`
	// LastRestart is what the helper's supervision did after the core last
	// left on its own, nil when it never did.
	LastRestart *model.CoreRestart `json:"lastRestart"`
}

// VersionResponse is GET /version's body.
type VersionResponse struct {
	Version string `json:"version"`
}

// InstallRequest is POST /core/install's body: Path is the mihomo binary to
// copy in, Geodata the directory the geodata databases are seeded from ("" to
// skip seeding).
type InstallRequest struct {
	Path    string `json:"path"`
	Geodata string `json:"geodata"`
}

// StartRequest is POST /core/start's body: the whole runtime config as YAML.
type StartRequest struct {
	Config string `json:"config"`
}

// ApplyRequest is POST /core/apply's body: the runtime config the helper's
// running core should switch to.
type ApplyRequest struct {
	Config string `json:"config"`
}

// ErrorResponse is the body of every non-2xx answer.
type ErrorResponse struct {
	Error string `json:"error"`
}
