package handler

import (
	"context"
	"errors"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/locales"
)

// coreState is what the core page shows: the running core, the newest
// release once asked for, and an upgrade while one runs.
type coreState struct {
	info   model.CoreInfo
	loaded bool
	// latest is the newest release; nil until the user checked. newer says
	// whether the running core is older than it.
	latest *model.CoreRelease
	newer  bool
	// checking is set while the newest release is being asked for; busy
	// while an upgrade or a return to the bundled core runs.
	checking bool
	busy     bool
	progress model.CoreProgress
}

// loadCore reads which core runs.
func (d *Dashboard) loadCore(ctx context.Context, win Updater) {
	info, err := d.services.Core.Info(ctx)
	win.Update(func() {
		if err != nil {
			d.fail(actionReadCore, err)
			return
		}
		d.core.info, d.core.loaded = info, true
	})
}

// beginCheckCore asks for the newest release in the background.
func (d *Dashboard) beginCheckCore() {
	c := &d.core
	if c.checking || c.busy {
		return
	}
	c.checking = true
	ctx, win := d.opCtx, d.opWin
	go func() {
		release, newer, err := d.services.Core.CheckLatest(ctx)
		win.Update(func() {
			d.core.checking = false
			if err != nil {
				d.fail(actionCheckCore, err)
				return
			}
			d.core.latest, d.core.newer = &release, newer
		})
	}()
}

// beginCoreOp runs a change of the core in the background. The core restarts
// under it, so the streams that read from the old one are ended first and
// started again on the new one, and the node view is read anew.
func (d *Dashboard) beginCoreOp(a action, run func(ctx context.Context, report func(model.CoreProgress)) error) {
	c := &d.core
	if c.busy || d.controlsLocked() {
		return
	}
	c.busy, c.progress = true, model.CoreProgress{}
	d.stopWatchers()
	gen := d.nextViewGen()
	ctx, win := d.opCtx, d.opWin
	go func() {
		err := run(ctx, func(p model.CoreProgress) {
			win.Update(func() { d.core.progress = p })
		})
		info, infoErr := d.services.Core.Info(ctx)
		win.Update(func() {
			d.core.busy = false
			if err != nil {
				d.fail(a, err)
			}
			if infoErr == nil {
				d.core.info, d.core.latest, d.core.newer = info, nil, false
			}
			d.startWatchers(ctx, win)
			go d.startupTest(ctx, win, gen)
		})
	}()
}

func (d *Dashboard) beginUpgradeCore() {
	d.beginCoreOp(actionUpgradeCore, func(ctx context.Context, report func(model.CoreProgress)) error {
		return d.services.Core.Upgrade(ctx, report)
	})
}

func (d *Dashboard) beginRestoreCore() {
	d.beginCoreOp(actionRestoreCore, func(ctx context.Context, _ func(model.CoreProgress)) error {
		return d.services.Core.RestoreBundled(ctx)
	})
}

// coreRows are the settings rows of the core: which one runs with what can
// be done to it, and, once there is something to say, what the check found
// or how far a change has come.
func (d *Dashboard) coreRows(c *ui.Context, t *ui.Theme) []func() {
	s := &d.core
	rows := []func(){func() {
		ui.Row(c).Key("core").Gap(gapMedium).AlignItems(ui.Center).Children(func() {
			coreTile(c, t)
			ui.Column(c).Grow(1).Basis(0).Gap(gapLine).Children(func() {
				ui.Row(c).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "mihomo").FontSize(fontBody).FontWeight(weightSemibold)
					ui.Text(c, s.info.Version).FontSize(fontBody).FontWeight(weightMedium).FontFeatures("tnum").TextColor(t.TextMuted)
				})
				mutedText(c, t, originText(s.info.Origin))
			})
			if !s.busy {
				d.coreActions(c, t)
			}
		})
	}}
	switch {
	case s.busy:
		rows = append(rows, func() { coreProgress(c, t, s.progress) })
	case s.latest != nil && s.newer && s.info.Origin == model.CoreOverride:
		rows = append(rows, func() {
			mutedText(c, t, locales.CoreNewDev(s.latest.Tag))
		})
	case s.latest != nil && s.newer:
		rows = append(rows, func() {
			mutedText(c, t, locales.CoreNewUpgrade(s.latest.Tag))
		})
	case s.latest != nil:
		rows = append(rows, func() { mutedText(c, t, locales.CoreUpToDate()) })
	}
	if d.tun.on {
		rows = append(rows, func() {
			mutedText(c, t, locales.CoreLockedByTun())
		})
	}
	return rows
}

// coreTile is the core's icon on a soft square, in the accent.
func coreTile(c *ui.Context, t *ui.Theme) {
	ui.Box(c).Key("tile").Width(cardIconTile).Height(cardIconTile).Radius(radiusControl).Center().
		Background(t.Accent.Alpha(0.09)).Children(func() {
		ui.Icon(c, iconCore).Width(cardIconGlyph).Height(cardIconGlyph).TextColor(t.Accent)
	})
}

// originText says where the running core came from.
func originText(origin model.CoreOrigin) string {
	switch origin {
	case model.CoreInstalled:
		return locales.CoreSourceUpgraded()
	case model.CoreOverride:
		return locales.CoreSourceDev()
	default:
		return locales.CoreSourceBundled()
	}
}

// coreActions are the buttons of an idle core: check, and what the check
// found.
func (d *Dashboard) coreActions(c *ui.Context, t *ui.Theme) {
	s := &d.core
	canChange := s.info.Origin != model.CoreOverride && !d.controlsLocked() && !d.tun.on
	ui.Row(c).Key("actions").Gap(gapSmall).AlignItems(ui.Center).Children(func() {
		if s.info.Origin == model.CoreInstalled {
			button(c, buttonSpec{label: locales.CoreRestoreBundled(), style: mainSoftButton(t), disabled: !canChange}, d.beginRestoreCore)
		}
		button(c, buttonSpec{label: locales.CheckForUpdates(), icon: iconRefresh, style: mainSoftButton(t), busy: s.checking}, d.beginCheckCore)
		if s.latest != nil && s.newer {
			button(c, buttonSpec{label: locales.CoreUpgradeTo(s.latest.Tag), icon: iconUpgrade, style: primaryButton(t), disabled: !canChange}, d.beginUpgradeCore)
		}
	})
}

// coreProgress says which step of the change is running, with a bar while
// the download has a size to measure by.
func coreProgress(c *ui.Context, t *ui.Theme, p model.CoreProgress) {
	ui.Column(c).Key("progress").Gap(gapSmall).Children(func() {
		ui.Row(c).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
			spinner(c, iconInline, t.Accent)
			ui.Text(c, stageText(p)).FontSize(fontSecondary).FontWeight(weightMedium).FontFeatures("tnum")
		})
		if p.Stage == model.CoreStageDownload && p.Total > 0 {
			progressBar(c, t, min(1, float32(p.Done)/float32(p.Total)), t.Accent)
		}
	})
}

func stageText(p model.CoreProgress) string {
	switch p.Stage {
	case model.CoreStageDownload:
		if p.Total <= 0 {
			return locales.CoreDownloading()
		}
		done, total := formatBytes(p.Done), formatBytes(p.Total)
		return locales.CoreDownloadingProgress(done.value+" "+done.unit, total.value+" "+total.unit)
	case model.CoreStageVerify:
		return locales.CoreChecking()
	case model.CoreStageRestart:
		return locales.CoreRestarting()
	default:
		return locales.CorePreparing()
	}
}

// startWatchers begins the four streams from the core on a context of
// their own, which stopWatchers can end.
func (d *Dashboard) startWatchers(ctx context.Context, win Updater) {
	watchCtx, cancel := context.WithCancel(ctx)
	d.watchCancel = cancel
	go d.watchTraffic(watchCtx, win)
	go d.watchConnections(watchCtx, win)
	go d.watchLogs(watchCtx, win)
	go d.watchMemory(watchCtx, win)
}

// stopWatchers ends the streams quietly: a stream that ends because it was
// cancelled reports nothing.
func (d *Dashboard) stopWatchers() {
	if d.watchCancel != nil {
		d.watchCancel()
		d.watchCancel = nil
	}
}

// ReportCoreRestart is where the runner's supervision reports to; it is
// safe to call from any goroutine, and Start applies the events on the UI
// thread.
func (d *Dashboard) ReportCoreRestart(r model.CoreRestart) {
	d.coreRestarts <- r
}

// watchCoreRestarts applies what the runner did after the core left on its
// own: the streams of the core that left are gone, so they start anew once
// a core is back, and giving up is shown like any other core failure.
func (d *Dashboard) watchCoreRestarts(ctx context.Context, win Updater) {
	for {
		select {
		case <-ctx.Done():
			return
		case r := <-d.coreRestarts:
			win.Update(func() { d.applyCoreRestart(r) })
		}
	}
}

// applyCoreRestart shows a supervised restart, or that supervision gave up,
// and brings the dashboard back onto the core that came back. A core change
// the user asked for restarts the streams itself, so while one runs, the
// events are left to it.
func (d *Dashboard) applyCoreRestart(r model.CoreRestart) {
	if d.core.busy || d.opCtx.Err() != nil {
		return
	}
	if r.GaveUp {
		d.stopWatchers()
		d.failConnection(actionCoreGaveUp, errors.New(r.Exit))
		d.releaseSystemProxy()
		return
	}
	d.fail(actionCoreRestarted, errors.New(locales.CoreQuitRepeatedly(r.InARow, r.Exit)))
	d.stopWatchers()
	gen := d.nextViewGen()
	d.startWatchers(d.opCtx, d.opWin)
	go d.startupTest(d.opCtx, d.opWin, gen)
}
