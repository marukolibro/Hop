//go:build !windows

package mihomo

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// commandLine returns the command line of process pid, or "" when no such
// process runs.
func commandLine(pid int) (string, error) {
	out, err := exec.Command("ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			// ps exits 1 when the process list is empty.
			return "", nil
		}
		return "", fmt.Errorf("read command line of pid %d: %w", pid, err)
	}
	return strings.TrimSpace(string(out)), nil
}
