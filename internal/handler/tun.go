package handler

import (
	"context"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
)

// tunState is what the TUN switch shows: whether the helper's core runs,
// whether the helper is installed, and which interface holds the default
// routes when another client's TUN does.
type tunState struct {
	on     bool
	busy   bool
	loaded bool
	// helper says the helper daemon answers, helperOutdated that it must be
	// replaced before TUN can use it.
	helper         bool
	helperOutdated bool
	// taken names the interface another client's TUN holds the routes with.
	taken   bool
	takenBy string
	// installOpen shows the one-time explanation of what enabling needs.
	installOpen bool
}

// startTun reads TUN's state, which main's startup already settled: adopted,
// enabled or refused. Goroutine ownership: started by Start, it ends after
// one read.
func (d *Dashboard) startTun(ctx context.Context, win Updater) {
	status, err := d.services.Tun.Status(ctx)
	win.Update(func() {
		if err != nil {
			d.fail(actionReadTun, err)
			return
		}
		d.applyTunStatus(status)
		if d.helperUpdateWanted(status) {
			d.tun.installOpen = true
		}
	})
}

// helperUpdateWanted says whether the update dialog opens by itself at start:
// the user had TUN on, and the installed helper is another version, so main's
// startup left TUN off until it is replaced. Asking now keeps TUN from
// staying off with no sign of why; the replacement itself still waits for the
// user's click and administrator password.
func (d *Dashboard) helperUpdateWanted(status model.TunStatus) bool {
	return d.settings.settings.Tun && status.HelperInstalled && status.HelperOutdated &&
		!status.CoreRunning && !status.Taken
}

// applyTunStatus shows the state the service reports. TUN is on exactly when
// the helper's core runs: only Hop's TUN runs there.
func (d *Dashboard) applyTunStatus(status model.TunStatus) {
	s := &d.tun
	s.loaded = true
	s.helper = status.HelperInstalled
	s.helperOutdated = status.HelperOutdated
	s.on = status.CoreRunning
	s.taken, s.takenBy = status.Taken, status.TakenBy
}

// beginTun turns TUN on or off. Turning it on with no helper, or one this
// app cannot use, first opens the one-time explanation of what that needs.
func (d *Dashboard) beginTun(on bool) {
	s := &d.tun
	if s.busy || !s.loaded || d.controlsLocked() {
		return
	}
	if on && (!s.helper || s.helperOutdated) {
		s.installOpen = true
		return
	}
	s.busy = true
	d.runTun(on)
}

// submitTunInstall goes ahead from the install dialog: install the helper
// and turn TUN on, with the pill busy while it runs.
func (d *Dashboard) submitTunInstall() {
	s := &d.tun
	s.installOpen = false
	s.busy = true
	d.runTun(true)
}

// runTun applies on in the background. Whichever core comes out of it serves
// the same controller, so the streams and the views are read anew under it.
func (d *Dashboard) runTun(on bool) {
	s := &d.tun
	d.stopWatchers()
	gen := d.nextViewGen()
	ctx, win := d.opCtx, d.opWin
	go func() {
		var err error
		a := actionTunOff
		if on {
			a = actionTunOn
			err = d.services.Tun.Enable(ctx)
		} else {
			err = d.services.Tun.Disable(ctx)
		}
		status, statusErr := d.services.Tun.Status(ctx)
		win.Update(func() {
			s.busy = false
			if statusErr != nil {
				d.fail(actionReadTun, statusErr)
			} else {
				d.applyTunStatus(status)
			}
			if err != nil {
				d.fail(a, err)
			} else {
				next := d.settings.settings
				next.Tun = on
				d.saveSettings(next)
			}
			d.startWatchers(ctx, win)
			go d.startupTest(ctx, win, gen)
			go d.loadCore(ctx, win)
		})
	}()
}

// beginUninstallHelper removes the helper from the settings page. A TUN that
// is on is turned off first, so Hop's own core carries the traffic again
// before the helper goes.
func (d *Dashboard) beginUninstallHelper() {
	s := &d.tun
	if s.busy || !s.loaded || d.controlsLocked() {
		return
	}
	s.busy = true
	wasOn := s.on
	if wasOn {
		d.stopWatchers()
	}
	gen := d.nextViewGen()
	ctx, win := d.opCtx, d.opWin
	go func() {
		err := d.services.Tun.Uninstall(ctx)
		status, statusErr := d.services.Tun.Status(ctx)
		win.Update(func() {
			s.busy = false
			if statusErr != nil {
				d.fail(actionReadTun, statusErr)
			} else {
				d.applyTunStatus(status)
			}
			if err != nil {
				d.fail(actionUninstallHelper, err)
			}
			if wasOn {
				d.startWatchers(ctx, win)
				go d.startupTest(ctx, win, gen)
				go d.loadCore(ctx, win)
			}
		})
	}()
}

// tunHint says why the TUN pill cannot be turned on, or nothing when it can.
func (d *Dashboard) tunHint() string {
	s := &d.tun
	if s.loaded && !s.on && s.taken {
		return "TUN 已被 " + s.takenBy + " 占用"
	}
	return ""
}

// tunDialog explains, once, what turning TUN on without a helper installed
// needs: an administrator password, for the helper Hop then runs its core
// under. 取消 leaves everything as it is; 安装并开启 installs and turns TUN
// on, with the pill busy while it runs. A helper of another version is
// updated instead, and the copy says so.
func (d *Dashboard) tunDialog(c *ui.Context, t *ui.Theme) {
	s := &d.tun
	description := "增强模式会接管全部流量,需要先安装 Hop 的增强组件。" + tunPrivilegePrompt + ",之后可随时在设置里卸载。"
	submit := "安装并开启"
	if s.helperOutdated {
		description = "增强模式会接管全部流量,需要更新 Hop 的增强组件。" + tunPrivilegePrompt + ",之后可随时在设置里卸载。"
		submit = "更新并开启"
	}
	animatedDialog(c, "tun", &s.installOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(dialogScrim)
		panel.Width(dialogWidth).Padding(pagePadding).Gap(gapLarge).Radius(radiusPanel).
			Background(overlaySurface(t)).Border(1, t.Border).Shadow(0, 10, 30, 0, dialogShadow)
		panel.Children(func() {
			ui.Column(c).Gap(gapSection).Children(func() {
				ui.Column(c).Gap(gapSmall).Children(func() {
					ui.Row(c).Gap(gapMedium).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, iconShieldCheck).Width(iconTitle).Height(iconTitle).TextColor(t.Accent)
						ui.Text(c, "开启增强模式").FontSize(fontHeadline).FontWeight(weightSemibold).Grow(1)
						iconButton(c, "关闭", iconClose, softButton(t), func() { s.installOpen = false })
					})
					mutedText(c, t, description)
				})
				ui.Row(c).Gap(gapSmall).Justify(ui.End).Children(func() {
					button(c, buttonSpec{label: "取消", style: softButton(t), disabled: s.busy}, func() { s.installOpen = false })
					button(c, buttonSpec{label: submit, style: primaryButton(t), busy: s.busy}, d.submitTunInstall)
				})
			})
		})
	})
}

// helperRows are the settings row of the privileged helper: what it is for,
// whether it needs updating, and 卸载 when one is installed.
func (d *Dashboard) helperRows(c *ui.Context, t *ui.Theme) []func() {
	s := &d.tun
	if !s.loaded {
		return nil
	}
	// The protocol version means nothing to the user; only whether the
	// installed component is this app's is worth saying.
	note := "未安装。开启增强模式时会请求安装。"
	switch {
	case s.helper && s.helperOutdated:
		note = "需要更新。下次开启增强模式时会请求更新。"
	case s.helper:
		note = "已安装,增强模式下的内核由它以系统权限运行"
	}
	return []func(){func() {
		ui.Row(c).Key("helper").Gap(gapMedium).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Key("tile").Width(cardIconTile).Height(cardIconTile).Radius(radiusControl).Center().
				Background(t.Accent.Alpha(0.09)).Children(func() {
				ui.Icon(c, iconShieldCheck).Width(cardIconGlyph).Height(cardIconGlyph).TextColor(t.Accent)
			})
			ui.Column(c).Grow(1).Basis(0).Gap(gapLine).Children(func() {
				ui.Text(c, "增强组件").FontSize(fontBody).FontWeight(weightSemibold)
				mutedText(c, t, note)
			})
			if s.helper {
				button(c, buttonSpec{label: "卸载", style: softButton(t), busy: s.busy, disabled: d.controlsLocked()}, d.beginUninstallHelper)
			}
		})
	}}
}
