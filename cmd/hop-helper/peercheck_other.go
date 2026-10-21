//go:build !darwin && !windows

package main

import (
	"errors"
	"net"
)

// connectionAllowed refuses every caller on platforms without a defined
// peer check: serving a caller the helper cannot identify would defeat the
// access check entirely. Windows has its own ACL-based check beside this.
func connectionAllowed(net.Conn, int) (bool, string, error) {
	return false, "", errors.New("the helper's peer check is not defined on this platform")
}
