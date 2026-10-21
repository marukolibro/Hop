//go:build !darwin && !windows

package handler

// tunPrivilegePrompt is how enabling TUN describes the one-time elevation;
// this platform has no privileged helper.
const tunPrivilegePrompt = "系统会请求一次管理员授权"
