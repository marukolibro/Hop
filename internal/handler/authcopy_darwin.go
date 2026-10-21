//go:build darwin

package handler

// tunPrivilegePrompt is how enabling TUN describes the one-time elevation
// macOS asks for.
const tunPrivilegePrompt = "系统会请求一次管理员密码"
