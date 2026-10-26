package handler

import (
	"errors"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
)

// appUpdateState is the version row's and the flyout's share of the app
// update check: whether this build can update itself, what was found, and
// how far a check or download has come.
type appUpdateState struct {
	enabled        bool
	disabledReason string
	checking       bool
	updating       bool
	update         model.AppUpdate
	progressDone   int64
	progressTotal  int64
}

// beginAppCheck asks GitHub for a newer release in the background through
// mygo's updater. A check changes nothing on disk; its failure is shown
// like any other settings action. ErrUpdatesDisabled hides the check
// instead of raising a notice.
func (d *Dashboard) beginAppCheck() {
	s := &d.appUpdate
	if !s.enabled || s.checking || s.updating || d.controlsLocked() {
		return
	}
	s.checking = true
	ctx, win := d.opCtx, d.opWin
	go func() {
		update, err := d.services.AppUpdate.Check(ctx)
		win.Update(func() {
			s.checking = false
			if errors.Is(err, mygo.ErrUpdatesDisabled) {
				s.enabled = false
				s.disabledReason = d.services.AppUpdate.DisabledReason()
				return
			}
			if err != nil {
				d.fail(actionCheckAppUpdate, err)
				return
			}
			s.update = update
		})
	}()
}

// beginAppInstall downloads and installs the newest release through mygo's
// updater, which checks again first so an offer left from days ago never
// installs an older version. It reports the bytes downloaded, then relaunches
// with mygo's normal quit. Installation before the swap changes nothing; a
// failure leaves the app as it is and the offer in place, unless the feed no
// longer offers anything.
func (d *Dashboard) beginAppInstall() {
	s := &d.appUpdate
	if !s.enabled || s.updating || s.checking || !s.update.Available || d.controlsLocked() {
		return
	}
	s.updating = true
	s.progressDone, s.progressTotal = 0, 0
	ctx, win := d.opCtx, d.opWin
	go func() {
		err := d.services.AppUpdate.Install(ctx, func(downloaded int64, total int64) {
			win.Update(func() { s.progressDone, s.progressTotal = downloaded, total })
		})
		win.Update(func() {
			s.updating = false
			if errors.Is(err, service.ErrNoAppUpdate) {
				s.update = model.AppUpdate{}
			}
			if err != nil {
				d.fail(actionInstallAppUpdate, err)
				return
			}
			s.update = model.AppUpdate{}
			mygo.App.Relaunch()
		})
	}()
}

// appVersionRow shows the running version and the update controls: 检查更新
// when nothing newer is known, the new version and 更新并重启 when one is,
// and a progress bar while the download runs. A build that cannot update
// itself shows why in short Chinese and offers neither button.
func (d *Dashboard) appVersionRow(c *ui.Context, t *ui.Theme) func() {
	s := &d.appUpdate
	return func() {
		note := ""
		if !s.enabled {
			note = s.disabledReason
			if note == "" {
				note = "此版本不能自动更新"
			}
		}
		settingRow(c, t, "版本", note, func() {
			ui.Column(c).Key("app-version").Gap(gapSmall).AlignItems(ui.End).Children(func() {
				ui.Row(c).Gap(gapMedium).AlignItems(ui.Center).Children(func() {
					version := d.appVersion
					if version == "" {
						version = "开发版"
					}
					mutedText(c, t, "Hop "+version).NoWrap().FontFeatures("tnum")
					if s.update.Available {
						ui.Text(c, "新版本 "+s.update.Version).FontSize(fontBody).FontWeight(weightMedium).
							TextColor(accentInk(t)).NoWrap()
						if s.enabled {
							label := "更新并重启"
							if s.updating {
								label = "更新中"
							}
							button(c, buttonSpec{label: label, style: primaryButton(t), busy: s.updating,
								disabled: d.controlsLocked()}, d.beginAppInstall)
						}
						return
					}
					if !s.enabled {
						return
					}
					label := "检查更新"
					if s.checking {
						label = "检查中"
					}
					button(c, buttonSpec{label: label, style: mainSoftButton(t), busy: s.checking,
						disabled: d.controlsLocked()}, d.beginAppCheck)
				})
				if s.updating && s.progressTotal > 0 {
					ratio := float32(s.progressDone) / float32(s.progressTotal)
					progressBar(c, t, max(0, min(1, ratio)), t.Accent)
				}
			})
		})
	}
}
