package handler

import (
	"context"

	"hop/internal/model"
)

// sysProxyState is what the system proxy switch shows. on is whether the
// system proxy points at Hop, as last read; the switch is bound to it.
type sysProxyState struct {
	on     bool
	busy   bool
	status model.SystemProxyStatus
	loaded bool
}

// startSystemProxy reads who holds the system proxy and, when the user left
// it on, points it at Hop again. Goroutine ownership: started by Start, it
// ends after one read or change.
func (d *Dashboard) startSystemProxy(ctx context.Context, win Updater, wanted bool) {
	var err error
	if wanted {
		err = d.services.SystemProxy.Enable(ctx)
	}
	status, statusErr := d.services.SystemProxy.Status(ctx)
	win.Update(func() {
		if err != nil {
			d.fail(actionSystemProxyOn, err)
		}
		d.applyProxyStatus(status, statusErr)
	})
}

// applyProxyStatus shows status, or why it could not be read.
func (d *Dashboard) applyProxyStatus(status model.SystemProxyStatus, err error) {
	s := &d.sysProxy
	if err != nil {
		d.fail(actionReadSystemProxy, err)
		return
	}
	s.status, s.loaded = status, true
	s.on = status.Owner == model.ProxyOwnerHop
}

// beginSystemProxy points the system proxy at Hop or clears it, in the
// background, and keeps the choice for the next start once it took.
// Clearing it also ends the connections still open through Hop.
func (d *Dashboard) beginSystemProxy(on bool) {
	s := &d.sysProxy
	if s.busy {
		return
	}
	s.busy = true
	ctx, win := d.opCtx, d.opWin
	go func() {
		var err error
		a := actionSystemProxyOn
		if on {
			err = d.services.SystemProxy.Enable(ctx)
		} else {
			a = actionSystemProxyOff
			err = d.services.SystemProxy.Disable(ctx)
		}
		var closeErr error
		if !on && err == nil {
			// Connections apps opened through the proxy outlive it: cut
			// them, so the apps reconnect directly and no traffic keeps
			// flowing through Hop once it is off.
			closeErr = d.services.Monitor.CloseAllConnections(ctx)
		}
		status, statusErr := d.services.SystemProxy.Status(ctx)
		win.Update(func() {
			s.busy = false
			d.applyProxyStatus(status, statusErr)
			if err != nil {
				d.fail(a, err)
				return
			}
			if closeErr != nil {
				d.fail(actionCloseConnection, closeErr)
			}
			next := d.settings.settings
			next.SystemProxy = on
			d.saveSettings(next)
		})
	}()
}

// releaseSystemProxy clears the system proxy when it points at Hop, for a
// core that is gone: apps would otherwise send their traffic nowhere. The
// saved choice stays, so the next start points it at Hop again.
func (d *Dashboard) releaseSystemProxy() {
	ctx, win := d.opCtx, d.opWin
	go func() {
		err := d.services.SystemProxy.Disable(ctx)
		status, statusErr := d.services.SystemProxy.Status(ctx)
		win.Update(func() {
			if err != nil {
				d.fail(actionSystemProxyOff, err)
			}
			d.applyProxyStatus(status, statusErr)
		})
	}()
}

// systemProxyHint says why the system proxy pill cannot be turned on, or
// nothing when it can.
func (d *Dashboard) systemProxyHint() string {
	s := &d.sysProxy
	if s.loaded && !s.on && s.status.Owner == model.ProxyOwnerOther {
		return "已被 " + s.status.Other + " 占用"
	}
	return ""
}
