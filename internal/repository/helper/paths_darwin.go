//go:build darwin

package helper

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
