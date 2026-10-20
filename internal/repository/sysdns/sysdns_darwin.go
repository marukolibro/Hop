// Package sysdns reads and sets the DNS servers of the network services, and
// keeps the backup the privileged helper restores them from. Reading is
// harmless anywhere; only the helper writes, and only while it holds the
// system DNS for TUN.
package sysdns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"hop/internal/repository/netservice"
)

const (
	// Empty is the argument Write passes for an empty list: networksetup then
	// unsets the service's servers and DHCP or the router provides them.
	Empty = "Empty"
	// dscacheutilCommand is the flush tool's absolute path, for the same
	// reason networksetup's is absolute.
	dscacheutilCommand = "/usr/bin/dscacheutil"
)

// Read returns the DNS servers set on service. networksetup's "There aren't
// any DNS Servers set on ..." answer means none are set on the service
// itself: DHCP or the router provides them.
func Read(ctx context.Context, service string) ([]string, error) {
	out, err := exec.CommandContext(ctx, netservice.Command, "-getdnsservers", service).Output()
	if err != nil {
		return nil, fmt.Errorf("read the DNS servers of %q (networksetup -getdnsservers): %w", service, err)
	}
	return parseServers(string(out)), nil
}

// parseServers reads networksetup's answer: one server per line, or the
// sentence that says none are set.
func parseServers(out string) []string {
	servers := []string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "There aren't any DNS Servers") {
			continue
		}
		servers = append(servers, line)
	}
	return servers
}

// Write sets the DNS servers of service; an empty list unsets them, so DHCP
// or the router provides them again.
func Write(ctx context.Context, service string, servers []string) error {
	return netservice.Run(ctx, setArgs(service, servers))
}

// setArgs is the argument list Write passes to networksetup.
func setArgs(service string, servers []string) []string {
	args := []string{"-setdnsservers", service}
	if len(servers) == 0 {
		return append(args, Empty)
	}
	return append(args, servers...)
}

// FlushCache drops what macOS cached for the old resolvers. It never signals
// any process.
func FlushCache(ctx context.Context) error {
	out, err := exec.CommandContext(ctx, dscacheutilCommand, "-flushcache").CombinedOutput()
	if err != nil {
		return fmt.Errorf("flush the DNS cache (dscacheutil -flushcache): %w (output=%q)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Backup is what the network services' DNS was before Hop took it over, by
// service name. An empty list means the service had none of its own: DHCP or
// the router provided them.
type Backup struct {
	Services map[string][]string `json:"services"`
}

// Encode returns the backup as its file's bytes.
func (b Backup) Encode() ([]byte, error) {
	raw, err := json.MarshalIndent(b, "", "\t")
	if err != nil {
		return nil, fmt.Errorf("encode the DNS backup: %w", err)
	}
	return append(raw, '\n'), nil
}

// DecodeBackup reads the backup file's bytes back, refusing a file that names
// no services: a backup that cannot restore anything is corrupt.
func DecodeBackup(raw []byte) (Backup, error) {
	var backup Backup
	if err := json.Unmarshal(raw, &backup); err != nil {
		return Backup{}, fmt.Errorf("decode the DNS backup: %w", err)
	}
	if backup.Services == nil {
		return Backup{}, errors.New("the DNS backup names no services")
	}
	return backup, nil
}
