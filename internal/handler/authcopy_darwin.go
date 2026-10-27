//go:build darwin

package handler

import "hop/locales"

// tunPrivilegePrompt is how enabling TUN describes the one-time elevation
// macOS asks for.
func tunPrivilegePrompt() string { return locales.TunPromptMac() }
