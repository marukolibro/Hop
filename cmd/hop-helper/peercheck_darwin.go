//go:build darwin

package main

import (
	"fmt"
	"net"
)

// connectionAllowed reads the peer's uid from the unix socket: the helper
// serves root and the installing user only.
func connectionAllowed(conn net.Conn, allowUID int) (bool, string, error) {
	uid, err := peerUID(conn)
	if err != nil {
		return false, "", err
	}
	who := fmt.Sprintf("uid %d", uid)
	return uid == 0 || int(uid) == allowUID, who, nil
}
