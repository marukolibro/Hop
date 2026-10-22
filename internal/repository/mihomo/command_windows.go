//go:build windows

package mihomo

import (
	"context"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// coreCommand keeps the console core and its checks in the background when
// launched by the GUI, while preserving their output and cancellation.
func coreCommand(ctx context.Context, binary string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
	return cmd
}
