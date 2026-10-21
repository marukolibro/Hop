//go:build !windows

package mihomo

// portFreeAttempts covers macOS's TIME_WAIT (twice net.inet.tcp.msl, 30 s by
// default) with room to spare.
const portFreeAttempts = 140
