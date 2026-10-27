//go:build !darwin && !windows

package handler

import "hop/locales"

// tunPrivilegePrompt is how enabling TUN describes the one-time elevation;
// this platform has no privileged helper.
func tunPrivilegePrompt() string { return locales.TunPromptOther() }
