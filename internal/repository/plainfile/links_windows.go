//go:build windows

package plainfile

import (
	"fmt"
	"os"
	"syscall"
)

// linkCount is how many names the open file has.
func linkCount(file *os.File) (uint64, error) {
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(file.Fd()), &info); err != nil {
		return 0, fmt.Errorf("read the file's link count (path=%q): %w", file.Name(), err)
	}
	return uint64(info.NumberOfLinks), nil
}
