//go:build !windows

package mihomo

import (
	"context"
	"os/exec"
)

// coreCommand preserves the platform's normal process behavior.
func coreCommand(ctx context.Context, binary string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, binary, args...)
}
