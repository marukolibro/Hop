//go:build windows

package main

import "net"

// connectionAllowed admits every connection on Windows: the socket lives in
// a directory whose DACL names SYSTEM, Administrators and the installing
// user, and Windows enforces that DACL when a process connects, so a
// connected peer is already one of those. There is no peer credential to
// read; the ACL is the check.
func connectionAllowed(net.Conn, int) (bool, string, error) {
	return true, "a caller admitted by the socket ACL", nil
}
