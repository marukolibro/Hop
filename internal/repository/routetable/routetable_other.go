//go:build !darwin && !windows

package routetable

import (
	"context"
	"fmt"
	"runtime"
)

// Reader is the stub, like the package function beside it.
type Reader struct{}

// Read always fails on a platform Hop's TUN mode does not run on.
func (Reader) Read(ctx context.Context) (Table, error) {
	return IPv4(ctx)
}

// IPv4 is a stub everywhere but darwin and windows: Hop's TUN mode runs
// there only.
func IPv4(context.Context) (Table, error) {
	return nil, fmt.Errorf("reading the routing table is not supported on %s", runtime.GOOS)
}
