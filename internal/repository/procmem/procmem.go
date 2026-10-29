// Package procmem reads how much memory Hop's own process holds, measured as
// the core measures itself, so the two can be shown side by side.
package procmem

import (
	"fmt"
	"os"

	"github.com/metacubex/mihomo/component/memory"
)

// Reader reads the resident memory of the process it runs in.
type Reader struct{}

// Resident returns the bytes of physical memory the process holds now, read
// with the core's own reader (proc_pidinfo on macOS, GetProcessMemoryInfo on
// Windows), which is what the core reports of itself.
func (Reader) Resident() (uint64, error) {
	pid := os.Getpid()
	stat, err := memory.GetMemoryInfo(int32(pid))
	if err != nil {
		return 0, fmt.Errorf("read the memory of process %d: %w", pid, err)
	}
	return stat.RSS, nil
}
