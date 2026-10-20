//go:build !darwin

package main

import (
	"errors"
	"net"
)

// peerUID refuses every caller on platforms without the credential lookup:
// the helper is a macOS feature, and serving a caller it cannot identify
// would defeat the access check entirely.
func peerUID(net.Conn) (uint32, error) {
	return 0, errors.New("peer credentials can only be checked on darwin")
}
