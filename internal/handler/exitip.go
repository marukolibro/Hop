package handler

import (
	"log/slog"

	"github.com/egoist/mygo/ui"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"

	"hop/internal/model"
)

const (
	exitIPLoadingText = "检测出口 IP…"
	exitIPFailedText  = "未检测到出口 IP"
	exitIPCopyTip     = "默认线路的出口 IP。AI 等走专用线路的网站可能不同。点击复制。"
	exitIPRetryTip    = "点击重试"
	exitIPCopiedText  = "已复制出口 IP"
)

// exitIPStatus is what the route bar's exit IP line shows.
type exitIPStatus uint8

const (
	exitIPIdle exitIPStatus = iota
	exitIPLoading
	exitIPReady
	exitIPFailed
)

// exitIPTarget is the route whose IP is wanted: its outbound mode, its
// resolved exit and the core epoch, so a restart asks again even when the
// same node is selected.
type exitIPTarget struct {
	mode string
	exit string
	core int
}

// exitIPState is the exit IP lookup. want is the latest target; seq drops a
// reply for an older one, as viewGen drops an older runtime's replies.
type exitIPState struct {
	status  exitIPStatus
	ip      model.ExitIP
	want    exitIPTarget
	seq     uint64
	running bool
}

// exitIPTarget reads the resolved exit and mode the route now has.
func (d *Dashboard) exitIPTarget() exitIPTarget {
	target := exitIPTarget{mode: d.view.Mode, core: d.coreEpoch}
	if len(d.view.Route) > 0 {
		target.exit = d.view.Route[len(d.view.Route)-1].Name
	}
	return target
}

// considerExitIP asks for the exit IP when the resolved exit or mode changed
// since the last ask, or the core epoch did. It runs on the UI thread.
func (d *Dashboard) considerExitIP() {
	if d.services.ExitIP == nil {
		return
	}
	target := d.exitIPTarget()
	if target.exit == "" {
		if d.exitIP.status != exitIPIdle || d.exitIP.want != target {
			d.exitIP.seq++
			d.exitIP.want = target
			d.exitIP.status = exitIPIdle
			d.exitIP.ip = model.ExitIP{}
		}
		return
	}
	if target == d.exitIP.want {
		return
	}
	d.exitIP.want = target
	d.exitIP.seq++
	d.exitIP.status = exitIPLoading
	d.exitIP.ip = model.ExitIP{}
	if !d.exitIP.running {
		d.startExitIP(target, d.exitIP.seq)
	}
}

// retryExitIP asks again after a failure, or after a click while one runs:
// the older reply is dropped and the wanted exit is asked for once it ends.
func (d *Dashboard) retryExitIP() {
	if d.exitIP.want.exit == "" {
		return
	}
	d.exitIP.seq++
	d.exitIP.status = exitIPLoading
	d.exitIP.ip = model.ExitIP{}
	if !d.exitIP.running {
		d.startExitIP(d.exitIP.want, d.exitIP.seq)
	}
}

// startExitIP runs one lookup. Only one runs at a time; a changed target
// waits for it and starts after the stale reply is dropped.
func (d *Dashboard) startExitIP(target exitIPTarget, seq uint64) {
	if d.services.ExitIP == nil || d.opCtx == nil || d.opWin == nil {
		d.exitIP.status = exitIPFailed
		return
	}
	d.exitIP.running = true
	ctx, win := d.opCtx, d.opWin
	go func() {
		ip, err := d.services.ExitIP.Lookup(ctx)
		win.Update(func() {
			d.exitIP.running = false
			if seq != d.exitIP.seq || target != d.exitIP.want {
				d.startWantedExitIP()
				return
			}
			if err != nil {
				slog.Warn("exit IP lookup failed", "error", err)
				d.exitIP.status = exitIPFailed
				d.exitIP.ip = model.ExitIP{}
				return
			}
			d.exitIP.status = exitIPReady
			d.exitIP.ip = ip
		})
	}()
}

// startWantedExitIP starts the latest target after an older reply ended.
func (d *Dashboard) startWantedExitIP() {
	if d.exitIP.running || d.exitIP.want.exit == "" || d.exitIP.status != exitIPLoading {
		return
	}
	d.startExitIP(d.exitIP.want, d.exitIP.seq)
}

// exitIPLine shows the exit's public IP and country under the route, on the
// nodes page only: loading and failed are muted, and the ready line copies
// the full IP; the failed line asks again.
func (d *Dashboard) exitIPLine(c *ui.Context, t *ui.Theme) {
	switch d.exitIP.status {
	case exitIPLoading:
		ui.Box(c).Key("exitIP").Label("出口 IP").Children(func() {
			captionText(c, t, exitIPLoadingText)
		})
	case exitIPFailed:
		ui.Box(c).Key("exitIP").Label("出口 IP").Tooltip(exitIPRetryTip).Cursor(ui.CursorPointer).
			OnClick(func() { d.retryExitIP() }).Children(func() {
			captionText(c, t, exitIPFailedText)
		})
	case exitIPReady:
		ip := d.exitIP.ip
		line := ui.Row(c).Key("exitIP").Label("出口 IP").MaxWidth(exitWidth).Clip().
			AlignItems(ui.End).Cursor(ui.CursorPointer).Tooltip(exitIPCopyTip)
		line.OnClick(func() {
			c.WriteClipboard(ip.Address.String())
			c.Toast(exitIPCopiedText)
		})
		line.Children(func() {
			value := ui.Text(c, ip.Address.String()).Font("monospace").FontSize(fontCaption).TextColor(t.Text).NoWrap()
			if onGlass(t) {
				value.FontWeight(weightMedium)
			}
			captionText(c, t, " · "+countryName(ip.Country)).MaxLines(1)
		})
	}
}

// unknownCountry is what CLDR calls a region it does not know; a code with
// that name is not a country Hop can name, so the code itself shows.
var unknownCountry = display.SimplifiedChinese.Regions().Name(language.MustParseRegion("ZZ"))

// countryName is the Chinese name of an ISO 3166 alpha-2 code, or the code
// itself when CLDR names none.
func countryName(code string) string {
	region, err := language.ParseRegion(code)
	if err != nil {
		return code
	}
	name := display.SimplifiedChinese.Regions().Name(region)
	if name == "" || name == unknownCountry {
		return code
	}
	return name
}
