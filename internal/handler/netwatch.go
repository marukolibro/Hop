package handler

import (
	"context"
	"log/slog"
	"time"

	"hop/internal/model"
)

// netStateEvery is how often the system proxy and the TUN routes are read
// again: other apps change both behind Hop's back, as Clash Verge does when it
// starts, quits or toggles its own TUN.
const netStateEvery = 3 * time.Second

// watchNetState keeps the system proxy and TUN pills on what the system holds
// now. A read that fails is logged, not shown: the next one tries again, and
// the actions themselves report their own failures. A pill whose change is
// running keeps its state until the change reports. Goroutine ownership:
// started by Start, it ends when ctx is cancelled.
func (d *Dashboard) watchNetState(ctx context.Context, win Updater) {
	ticker := time.NewTicker(netStateEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		proxy, proxyErr := d.services.SystemProxy.Status(ctx)
		tun, tunErr := d.services.Tun.Status(ctx)
		if proxyErr != nil {
			slog.Warn("could not read the system proxy", "error", proxyErr)
		}
		if tunErr != nil {
			slog.Warn("could not read the TUN state", "error", tunErr)
		}
		win.Update(func() { d.applyNetState(proxy, proxyErr, tun, tunErr) })
	}
}

// applyNetState shows what one read found, leaving any pill whose change is
// running as it is.
func (d *Dashboard) applyNetState(proxy model.SystemProxyStatus, proxyErr error, tun model.TunStatus, tunErr error) {
	if proxyErr == nil && !d.sysProxy.busy {
		d.applyProxyStatus(proxy, nil)
	}
	if tunErr == nil && !d.tun.busy {
		d.applyTunStatus(tun)
	}
}
