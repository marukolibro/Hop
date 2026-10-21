//go:build windows

package handler

// tunPrivilegePrompt is how enabling TUN describes the one-time elevation
// Windows asks for.
const tunPrivilegePrompt = "系统会弹出一次 UAC 授权窗口"
