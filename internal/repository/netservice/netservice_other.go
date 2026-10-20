//go:build !darwin

package netservice

import (
	"context"
	"fmt"
	"runtime"
)

// List is a stub everywhere but darwin: networksetup is macOS's.
func List(context.Context) ([]string, error) {
	return nil, fmt.Errorf("listing network services is not supported on %s", runtime.GOOS)
}

// Run is the matching stub.
func Run(context.Context, []string) error {
	return fmt.Errorf("changing network services is not supported on %s", runtime.GOOS)
}
