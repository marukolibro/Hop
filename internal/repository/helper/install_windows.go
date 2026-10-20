//go:build windows

package helper

import (
	"context"
	"errors"
)

// ErrUnsupportedOnWindows says TUN mode, which needs the privileged helper,
// is not available on Windows yet.
var ErrUnsupportedOnWindows = errors.New("TUN is not supported on Windows yet")

// Installer is the platform's installer: a stub, on Windows.
type Installer struct{}

func (Installer) InstalledVersion(context.Context) (string, error) {
	return "", ErrUnsupportedOnWindows
}

func (Installer) Install(context.Context, string, string, string, int) error {
	return ErrUnsupportedOnWindows
}

func (Installer) Uninstall(context.Context) error { return ErrUnsupportedOnWindows }
