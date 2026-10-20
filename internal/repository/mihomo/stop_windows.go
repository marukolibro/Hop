//go:build windows

package mihomo

import "os"

// requestStop ends the core: Windows has no signal that asks a console-less
// process to shut down, so this is the only stop it offers.
func requestStop(p *os.Process) error { return p.Kill() }
