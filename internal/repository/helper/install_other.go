//go:build !darwin && !windows

package helper

import (
	"context"
	"errors"
)

// ErrUnsupportedHere says TUN mode, which needs the privileged helper, is
// not available on this platform.
var ErrUnsupportedHere = errors.New("TUN is not supported on this platform yet")

// Installer is the platform's installer: a stub, anywhere but darwin.
type Installer struct{}

func (Installer) InstalledVersion(context.Context) (string, error) {
	return "", ErrUnsupportedHere
}

func (Installer) Install(context.Context, string, string, string, int) error {
	return ErrUnsupportedHere
}

func (Installer) Uninstall(context.Context) error { return ErrUnsupportedHere }
