package handler

import (
	"context"
	"log/slog"
)

// considerReconnect asks the core again, once per call, while the dashboard
// holds it unreachable. Only a reply clears that state, and a core that was
// slow for a while (a long reload, a busy machine) answers again with nothing
// else asking: until then the header, the hero and the menu bar would say Hop
// carries no traffic while the core does, and the recovery after a network
// change and the service checks would stay off. A core that is gone does not
// answer, so the check fails quietly and the next one tries again; the failure
// itself was shown when it happened. It leaves the core alone while a change
// of the core runs, which reports for itself.
func (d *Dashboard) considerReconnect(ctx context.Context, win Updater) {
	if d.status != statusFailed || d.reconnecting || d.core.busy || ctx.Err() != nil {
		return
	}
	d.reconnecting = true
	go d.reconnect(ctx, win, d.viewGen)
}

// reconnect reads the view in the current epoch without taking a new one, so a
// load that another action started stays current. When the core answers, the
// view shows it and the streams start again on it: one that ended while the
// core was unreachable is not coming back on its own.
func (d *Dashboard) reconnect(ctx context.Context, win Updater, gen int) {
	view, err := d.services.Proxies.View(ctx, gen)
	win.Update(func() {
		d.reconnecting = false
		if err != nil || gen != d.viewGen || d.status != statusFailed || ctx.Err() != nil {
			return
		}
		slog.Info("the core answers again")
		d.applyView(view)
		d.stopWatchers()
		d.startWatchers(ctx, win)
	})
}
