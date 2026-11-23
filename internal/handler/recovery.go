package handler

import (
	"context"
	"log/slog"
	"time"

	"github.com/egoist/mygo"

	"hop/internal/service"
)

// recoverySettleDelay waits briefly after wake or a network transition so DHCP
// and Wi-Fi routes settle before probing nodes and exit IP.
const recoverySettleDelay = 2 * time.Second

// recoveryState coordinates debounced self-healing after system wake or a
// network path change. All fields are touched on the UI thread only.
type recoveryState struct {
	timer     *time.Timer
	seq       uint64
	delay     time.Duration
	netKnown  bool
	netStatus mygo.NetworkStatus
}

// HandlePowerResume schedules a debounced refresh when the computer wakes from
// sleep so stale DNS entries, exit IP, and node latencies self-heal.
func (d *Dashboard) HandlePowerResume() {
	d.scheduleNetworkRecovery("resume")
}

// HandleNetworkChange reacts to OS connectivity transitions. Going offline
// cancels any pending probe; coming back online or switching path characteristics
// schedules a debounced refresh.
func (d *Dashboard) HandleNetworkChange(status mygo.NetworkStatus) {
	prevKnown := d.recovery.netKnown
	prev := d.recovery.netStatus
	d.recovery.netKnown = true
	d.recovery.netStatus = status

	if !status.Online {
		d.cancelPendingRecovery()
		return
	}
	if prevKnown && prev == status {
		return
	}
	d.scheduleNetworkRecovery("network-change")
}

func (d *Dashboard) cancelPendingRecovery() {
	d.recovery.seq++
	if d.recovery.timer != nil {
		d.recovery.timer.Stop()
		d.recovery.timer = nil
	}
}

// stopNetworkRecovery cancels any scheduled wake/network recovery at quit.
func (d *Dashboard) stopNetworkRecovery() {
	d.cancelPendingRecovery()
}

func (d *Dashboard) scheduleNetworkRecovery(reason string) {
	if d.opCtx == nil || d.opWin == nil {
		return
	}
	d.cancelPendingRecovery()
	seq := d.recovery.seq
	wait := d.recovery.delay
	if wait <= 0 {
		wait = recoverySettleDelay
	}
	win := d.opWin
	d.recovery.timer = time.AfterFunc(wait, func() {
		win.Update(func() {
			if seq != d.recovery.seq {
				return
			}
			d.recovery.timer = nil
			d.runNetworkRecovery(reason)
		})
	})
}

// runNetworkRecovery clears the core's DNS cache, forces a fresh exit IP
// lookup, re-tests auto groups, refreshes LAN addresses, and refreshes or
// invalidates service unlock checks. It never modifies OS DNS, routes, TUN, or
// system proxy settings.
func (d *Dashboard) runNetworkRecovery(reason string) {
	if d.opCtx == nil || d.opWin == nil || d.opCtx.Err() != nil {
		return
	}
	if d.status != statusConnected || d.controlsLocked() || d.testing != "" {
		return
	}
	slog.Info("refreshing core state after system event", "reason", reason)
	ctx, win := d.opCtx, d.opWin

	// Bump coreEpoch so exit IP is re-checked even when the same node stays active.
	d.coreEpoch++
	d.considerExitIP()

	if d.foreground {
		d.refreshServices(true)
	} else {
		// Mark cached service checks stale so the next window open re-checks at once.
		d.unlock.services.lastRun = time.Time{}
	}
	if d.services.LAN != nil {
		go d.readLAN(win)
	}

	if d.services.Proxies == nil {
		return
	}
	gen := d.nextViewGen()
	testCtx, cancel := context.WithCancel(ctx)
	d.delayCancel = cancel
	d.startTesting(testingAll)
	timeout := service.DelayTimeout(d.settings.settings)
	testURL := service.DelayTestURL(d.settings.settings)

	go func() {
		defer cancel()
		if err := d.services.Proxies.FlushDNSCache(testCtx); err != nil && testCtx.Err() == nil {
			slog.Warn("could not flush core DNS cache after system event", "reason", reason, "error", err)
		}
		stop := d.refreshWhileTesting(testCtx, win, gen)
		err := d.services.Proxies.TestAutoGroups(testCtx, testURL, timeout)
		stop()
		d.finishDelayTest(testCtx, win, gen, err)
	}()
}
