//go:build windows

package mihomo

// portFreeAttempts covers Windows's TIME_WAIT, two minutes by default, with
// room to spare: a port the previous core's connections closed first stays
// unbindable until then.
const portFreeAttempts = 480
