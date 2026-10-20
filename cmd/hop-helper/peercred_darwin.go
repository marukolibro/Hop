//go:build darwin

package main

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// peerUID says which user is on the other end of a connected unix socket.
// macOS records the connecting peer's credentials at connect time, where
// LOCAL_PEERCRED reads them back.
func peerUID(conn net.Conn) (uint32, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, fmt.Errorf("the helper's connection is not a unix socket (%T)", conn)
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("reach the helper's socket descriptor: %w", err)
	}
	var uid uint32
	var sockErr error
	ctrlErr := raw.Control(func(fd uintptr) {
		cred, err := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if err != nil {
			sockErr = fmt.Errorf("read the peer's credentials: %w", err)
			return
		}
		uid = cred.Uid
	})
	if ctrlErr != nil {
		return 0, fmt.Errorf("enter the socket control path: %w", ctrlErr)
	}
	if sockErr != nil {
		return 0, sockErr
	}
	return uid, nil
}
