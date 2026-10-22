package handler

import (
	"context"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
)

// geodataState is what the 规则数据 row shows.
type geodataState struct {
	status model.GeodataStatus
	loaded bool
	failed bool
	busy   bool
}

// loadGeodata reads when the databases last updated.
func (d *Dashboard) loadGeodata(ctx context.Context, win Updater) {
	status, err := d.services.Geodata.Status()
	win.Update(func() {
		if err != nil {
			d.geodata.failed = true
			d.fail(actionReadGeodata, err)
			return
		}
		d.geodata.status, d.geodata.loaded, d.geodata.failed = status, true, false
	})
}

// beginGeodataUpdate downloads and applies the databases in the background.
// The core restarts on them, so the streams from the old one are ended first
// and started again once it answers.
func (d *Dashboard) beginGeodataUpdate() {
	s := &d.geodata
	if s.busy || d.controlsLocked() {
		return
	}
	s.busy = true
	d.stopWatchers()
	gen := d.nextViewGen()
	ctx, win := d.opCtx, d.opWin
	go func() {
		err := d.services.Geodata.Update(ctx)
		status, statusErr := d.services.Geodata.Status()
		win.Update(func() {
			s.busy = false
			if statusErr != nil {
				s.failed = true
				d.fail(actionReadGeodata, statusErr)
			} else {
				s.status, s.loaded, s.failed = status, true, false
			}
			if err != nil {
				d.fail(actionUpdateGeodata, err)
			}
			d.startWatchers(ctx, win)
			go d.startupTest(ctx, win, gen)
		})
	}()
}

// geodataRow is the settings row of the rule databases: when they last
// updated, or that they are the ones the app ships, and their 更新 button.
func (d *Dashboard) geodataRow(c *ui.Context, t *ui.Theme) func() {
	s := &d.geodata
	return func() {
		settingRow(c, t, "规则数据", "GeoSite 与 GeoIP 数据，每周自动更新；更新后内核重启一次", func() {
			ui.Row(c).Key("geodata").Gap(gapMedium).AlignItems(ui.Center).Children(func() {
				text := "读取中"
				switch {
				case s.busy:
					text = "更新中"
				case s.failed:
					text = "读取失败"
				case s.loaded && s.status.UpdatedAt.IsZero():
					text = "随应用自带"
				case s.loaded:
					text = "更新于 " + s.status.UpdatedAt.Format(usageDateStyle)
				}
				mutedText(c, t, text).NoWrap().FontFeatures("tnum")
				label := "更新"
				if s.busy {
					label = "更新中"
				}
				button(c, buttonSpec{label: label, icon: iconRefresh, style: mainSoftButton(t),
					disabled: d.controlsLocked(), busy: s.busy}, d.beginGeodataUpdate)
			})
		})
	}
}
