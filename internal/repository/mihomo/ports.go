package mihomo

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"time"

	"hop/config"
)

// The wait covers the platform's TIME_WAIT (portFreeAttempts, per platform):
// a connection the previous core closed first keeps its port from a core run
// by another user until then, though nothing listens on it.
const portFreeInterval = 250 * time.Millisecond

// waitPortsFree waits until the core's controller and mixed port can be
// bound. A core that just stopped can hold them a moment longer, and mihomo
// does not retry a controller it could not bind: started too early, it runs
// without a controller and never answers. Each failed attempt is warned
// about; the last error is returned.
func waitPortsFree(ctx context.Context, cfg config.Config) error {
	addrs := []string{cfg.ControllerAddr}
	if cfg.NodeDNSAddr != "" {
		addrs = append(addrs, cfg.NodeDNSAddr)
	}
	if cfg.MixedPort > 0 {
		addrs = append(addrs, net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.MixedPort)))
	}
	var last error
	for attempt := 1; attempt <= portFreeAttempts; attempt++ {
		last = bindable(addrs)
		if last == nil {
			return nil
		}
		slog.Warn("the core's ports are not free yet", "attempt", attempt, "max_attempts", portFreeAttempts, "error", last)
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for the core's ports: %w", ctx.Err())
		case <-time.After(portFreeInterval):
		}
	}
	return fmt.Errorf("the core's ports stayed taken after %d attempts: %w", portFreeAttempts, last)
}

// bindable says whether every address can be listened on now, releasing
// each listener at once.
func bindable(addrs []string) error {
	for _, addr := range addrs {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("bind %s: %w", addr, err)
		}
		if err := ln.Close(); err != nil {
			return fmt.Errorf("release %s: %w", addr, err)
		}
	}
	return nil
}
