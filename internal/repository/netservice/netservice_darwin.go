// Package netservice runs networksetup, the macOS tool that lists the
// network services and changes their settings. The system proxy and the
// helper's DNS takeover share it, so both follow the same rules about its
// output.
package netservice

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Command is networksetup's absolute path: the helper runs under launchd,
// whose PATH does not carry /usr/sbin.
const Command = "/usr/sbin/networksetup"

// List returns the enabled network services. networksetup prints a note first
// and marks a disabled service with a leading "*".
func List(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(ctx, Command, "-listallnetworkservices").Output()
	if err != nil {
		return nil, fmt.Errorf("list network services (networksetup -listallnetworkservices): %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var services []string
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "*") {
			continue
		}
		services = append(services, line)
	}
	return services, nil
}

// Run runs one networksetup change. It reports some failures, such as a
// missing privilege, only on its output with a zero exit status, so any
// output is an error.
func Run(ctx context.Context, args []string) error {
	out, err := exec.CommandContext(ctx, Command, args...).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return fmt.Errorf("networksetup %s: %w (output=%q)", strings.Join(args, " "), err, text)
	}
	if text != "" {
		return fmt.Errorf("networksetup %s: %s", strings.Join(args, " "), text)
	}
	return nil
}
