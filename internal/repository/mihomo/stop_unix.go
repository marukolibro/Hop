//go:build !windows

package mihomo

import (
	"os"
	"syscall"
)

// requestStop asks the core to shut down cleanly.
func requestStop(p *os.Process) error { return p.Signal(syscall.SIGTERM) }
