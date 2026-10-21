//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"

	"hop/internal/repository/helper"
)

// secureDirs makes the socket directory's DACL the peer check: SYSTEM,
// Administrators and the installing user, nobody else. Windows enforces the
// DACL when a process connects, which is the equivalent of macOS's
// LOCAL_PEERCRED. The home holds the core's config and the controller
// secret, so while the helper runs as a service only SYSTEM and
// Administrators may read it.
func secureDirs(opts serverOptions) error {
	sid := opts.allowSID
	if sid == "" && !opts.serviceMode {
		// A helper run by hand in a terminal serves the user who ran it.
		current, err := helper.CurrentUserSID()
		if err != nil {
			return err
		}
		sid = current
	}
	socketDir := filepath.Dir(opts.socket)
	if err := os.MkdirAll(socketDir, 0o700); err != nil {
		return fmt.Errorf("create the helper's socket directory (dir=%q): %w", socketDir, err)
	}
	sddl, err := helper.SocketDirSDDL(sid)
	if err != nil {
		return err
	}
	if err := applyDirSDDL(socketDir, sddl); err != nil {
		return fmt.Errorf("guard the helper's socket directory (dir=%q): %w", socketDir, err)
	}
	if opts.serviceMode {
		if err := applyDirSDDL(opts.home, helper.HomeSDDL()); err != nil {
			return fmt.Errorf("guard the helper's home (dir=%q): %w", opts.home, err)
		}
	}
	return nil
}

// openSocketToCaller does nothing on Windows: the socket inherits the
// directory's DACL, and that is what admits the installing user.
func openSocketToCaller(string) error { return nil }

// applyDirSDDL replaces path's DACL with sddl, dropping what it inherited.
func applyDirSDDL(path string, sddl string) error {
	descriptor, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("parse the security descriptor (%q): %w", sddl, err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return fmt.Errorf("read the security descriptor's DACL: %w", err)
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil,
	); err != nil {
		return fmt.Errorf("set the DACL on %q: %w", path, err)
	}
	return nil
}
