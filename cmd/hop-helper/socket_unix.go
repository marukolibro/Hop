//go:build !windows

package main

import "os"

// secureDirs leaves the helper's directories as the installer or the test
// made them: the LaunchDaemon runs the helper as root, and the socket's
// mode plus the per-connection uid check guard access.
func secureDirs(serverOptions) error { return nil }

// openSocketToCaller lets the installing user's processes connect; the
// handler checks each peer's uid.
func openSocketToCaller(path string) error { return os.Chmod(path, 0o666) }
