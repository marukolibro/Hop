package handler

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"hop/internal/service"
)

const (
	portNote = "改后立即生效,系统代理会跟着换;填过旧端口的程序要自己改"
	// portSkippedNote says why Hop runs on another port than the one chosen.
	portSkippedNote = "上次选的端口 %d 被其他程序占用,这次先用 %d。腾出它后重启,或在这里换一个"
	portWidth       = 86
)

// portUIState is the proxy port row: what is typed, and whether a change
// runs.
type portUIState struct {
	input string
	busy  bool
}

// proxyAddress is where apps send traffic to use Hop, as host:port, on the
// port Hop listens on now.
func (d *Dashboard) proxyAddress() string {
	return net.JoinHostPort(d.proxyHost, strconv.Itoa(d.services.Port.Current()))
}

// beginPort moves the proxy to the typed port in the background. The row
// shows the port Hop ends up on, so a refused one is typed back to the old.
func (d *Dashboard) beginPort() {
	p := &d.port
	if p.busy || d.sysProxy.busy || d.controlsLocked() {
		return
	}
	port, err := strconv.Atoi(strings.TrimSpace(p.input))
	if err != nil {
		d.fail(actionPort, fmt.Errorf("代理端口要填 %d 到 %d 之间的整数", service.MixedPortMin, service.MixedPortMax))
		return
	}
	if port == d.services.Port.Current() {
		return
	}
	p.busy = true
	ctx, win := d.opCtx, d.opWin
	go func() {
		err := d.services.Port.Change(ctx, port)
		status, statusErr := d.services.SystemProxy.Status(ctx)
		win.Update(func() {
			p.busy = false
			p.input = strconv.Itoa(d.services.Port.Current())
			if statusErr == nil {
				d.applyProxyStatus(status, nil)
			}
			if err != nil {
				d.fail(actionPort, err)
				return
			}
			d.reloadViewAfterCore(ctx, win)
		})
	}()
}

// portRow is the settings row of the proxy port.
func (d *Dashboard) portRow(c *ui.Context, t *ui.Theme) func() {
	return func() {
		p := &d.port
		locked := p.busy || d.sysProxy.busy || d.controlsLocked()
		note := portNote
		if skipped := d.services.Port.Skipped(); skipped != 0 {
			note = fmt.Sprintf(portSkippedNote, skipped, d.services.Port.Current())
		}
		settingRow(c, t, "代理端口", note, func() {
			unchanged := strings.TrimSpace(p.input) == strconv.Itoa(d.services.Port.Current())
			ui.Row(c).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Disabled(locked).Children(func() {
					ui.TextInput(c.Key("proxy-port"), &p.input).Label("代理端口").Width(portWidth).
						Font("monospace").OnSubmit(d.beginPort)
				})
				button(c, buttonSpec{label: "应用", style: mainSoftButton(t), busy: p.busy,
					disabled: locked || unchanged}, d.beginPort)
			})
		})
	}
}
