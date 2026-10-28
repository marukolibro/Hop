//go:build !windows

package plainfile

import (
	"fmt"
	"os"
	"syscall"
)

// linkCount is how many names the open file has.
func linkCount(file *os.File) (uint64, error) {
	info, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("look at the opened file (path=%q): %w", file.Name(), err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("the file's status carries no link count (path=%q)", file.Name())
	}
	return uint64(stat.Nlink), nil
}
