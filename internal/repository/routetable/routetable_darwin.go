//go:build darwin

package routetable

import (
	"context"
	"fmt"
	"os/exec"
)

// Reader reads the table through IPv4; it is the shape the service layer
// depends on.
type Reader struct{}

// Read returns the Internet routing table.
func (Reader) Read(ctx context.Context) (Table, error) {
	return IPv4(ctx)
}

// IPv4 reads the Internet routing table.
func IPv4(ctx context.Context) (Table, error) {
	out, err := exec.CommandContext(ctx, "netstat", "-rn", "-f", "inet").Output()
	if err != nil {
		return nil, fmt.Errorf("read the routing table (netstat -rn -f inet): %w", err)
	}
	return ParseNetstat(string(out)), nil
}
