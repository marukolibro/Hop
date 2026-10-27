//go:build windows

package handler

import "hop/locales"

// tunPrivilegePrompt is how enabling TUN describes the one-time elevation
// Windows asks for.
func tunPrivilegePrompt() string { return locales.TunPromptWindows() }
