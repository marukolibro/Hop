package handler

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/egoist/mygo"
)

const (
	// updateCheckEvery is how often the dashboard asks what is due. The
	// check reads the stored times, so a Mac that slept through a ticker
	// period catches up at the next one instead of waiting another full
	// interval.
	updateCheckEvery = 5 * time.Minute
	// updateFirstCheck lets the core and the profile load before the first
	// check.
	updateFirstCheck = time.Minute
)

// runUpdateScheduler refreshes what is due in the background: stored
// subscriptions by the chosen interval, and the rule databases weekly.
// Goroutine ownership: started by Start, it ends when the window's context
// does. It only asks the UI thread to begin a pass, so it never reads UI
// state itself, and a pass never starts while another change runs.
func (d *Dashboard) runUpdateScheduler(ctx context.Context, win Updater) {
	timer := time.NewTimer(updateFirstCheck)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		d.requestAutoUpdate(ctx, win)
		timer.Reset(updateCheckEvery)
	}
}

// requestAutoUpdate starts one automatic pass on the UI thread, when nothing
// else is changing the core or the store.
func (d *Dashboard) requestAutoUpdate(ctx context.Context, win Updater) {
	win.Update(func() {
		dialogOpen := d.subscription.editorOpen || d.subscription.deleteOpen || d.tun.installOpen || d.rules.editorOpen || d.fakeIP.editorOpen
		if d.autoBusy || d.controlsLocked() || d.testing != "" || dialogOpen || ctx.Err() != nil {
			return
		}
		d.autoBusy = true
		go d.autoUpdate(ctx, win)
	})
}

// autoUpdate checks for a new Hop first, then runs the subscription pass and
// asks on the UI thread whether the databases are due before touching them:
// their update restarts the core, so the streams from the old one are ended
// first and started again after. A subscription refresh only reloads the
// view, because the core stays up.
func (d *Dashboard) autoUpdate(ctx context.Context, win Updater) {
	if due, err := d.services.AppUpdate.Due(); err != nil {
		slog.Warn("could not read the app update check time", "error", err)
	} else if due {
		update, err := d.services.AppUpdate.Check(ctx)
		switch {
		case errors.Is(err, mygo.ErrUpdatesDisabled):
			win.Update(func() {
				d.appUpdate.enabled = false
				d.appUpdate.disabledReason = d.services.AppUpdate.DisabledReason()
			})
		case err != nil:
			slog.Warn("the automatic app update check failed", "error", err)
		default:
			win.Update(func() { d.appUpdate.update = update })
		}
	}

	var subsChanged bool
	if settings, err := d.services.Settings.Load(); err != nil {
		slog.Error("could not read settings for the automatic updates", "error", err)
	} else {
		var subErr error
		subsChanged, subErr = d.services.Subscriptions.AutoUpdate(ctx, time.Duration(settings.SubscriptionUpdateHours)*time.Hour)
		if subErr != nil {
			slog.Warn("the automatic subscription update had failures", "error", subErr)
		}
		if subsChanged {
			d.loadSubscriptions(ctx, win)
		}
	}

	win.Update(func() {
		if ctx.Err() != nil {
			d.autoBusy = false
			return
		}
		due, err := d.services.Geodata.Due()
		if err != nil {
			slog.Warn("could not tell whether the geodata is due", "error", err)
		}
		if !due {
			d.autoBusy = false
			if subsChanged {
				gen := d.nextViewGen()
				go d.loadView(ctx, win, gen)
			}
			return
		}
		d.stopWatchers()
		gen := d.nextViewGen()
		go d.autoUpdateGeodata(ctx, win, gen, subsChanged)
	})
}

// autoUpdateGeodata updates the databases and brings the dashboard back onto
// the restarted core: the streams start again, and the node view is read
// either after the restart or, if nothing changed after all, when the
// subscription pass changed the running profile.
func (d *Dashboard) autoUpdateGeodata(ctx context.Context, win Updater, gen int, subsChanged bool) {
	geoChanged, geoErr := d.services.Geodata.AutoUpdate(ctx)
	if geoErr != nil {
		slog.Warn("the automatic geodata update failed", "error", geoErr)
	}
	status, statusErr := d.services.Geodata.Status()
	win.Update(func() {
		d.autoBusy = false
		if statusErr != nil {
			d.geodata.failed = true
			slog.Warn("could not read the geodata status", "error", statusErr)
		} else {
			d.geodata.status, d.geodata.loaded, d.geodata.failed = status, true, false
		}
		if ctx.Err() != nil {
			return
		}
		d.startWatchers(ctx, win)
		switch {
		case geoChanged:
			go d.startupTest(ctx, win, gen)
		case subsChanged:
			go d.loadView(ctx, win, gen)
		}
	})
}
