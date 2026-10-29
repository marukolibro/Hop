package handler

import (
	"fmt"
	"net"
	"time"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/locales"
)

const (
	// homeCardPadY and homeCardPadX pad a card of the home row.
	homeCardPadY float32 = 14
	homeCardPadX float32 = 16
	// serviceTilesShown is how many services the services card shows; the
	// dialog behind it lists them all.
	serviceTilesShown = 4
	// serviceMarkRatio is a mark's glyph as a share of the tile it sits on.
	serviceMarkRatio float32 = 0.54
	// lanSwitchPad pads the row that holds the LAN proxy's switch.
	lanSwitchPad float32 = 9
)

// homeCardColumns is how many cards the row holds in width: four in a wide
// window, then two, then one.
func homeCardColumns(width float32) int {
	switch {
	case width >= 4*homeCardMinWidth+3*gapMedium:
		return 4
	case width >= 2*homeCardMinWidth+gapMedium:
		return 2
	}
	return 1
}

// nodesPaneWidth is the width of the content column beside the sidebar, less
// its gutters.
func nodesPaneWidth(c *ui.Context) float32 {
	width, _ := c.Size()
	return width - sidebarWidthFor(width) - hairlineWidth - 2*pagePadding
}

// homeCards is the row of four cards under the hero: the outbound mode, the
// address other devices use with the LAN proxy's switch, the services, and
// the memory Hop and its core hold.
func (d *Dashboard) homeCards(c *ui.Context, t *ui.Theme) {
	paneWidth := nodesPaneWidth(c)
	columns := homeCardColumns(paneWidth)
	cardWidth := (paneWidth - float32(columns-1)*gapMedium) / float32(columns)
	ui.Grid(c).Key("homeCards").Columns(columns).Gap(gapMedium).Children(func() {
		d.modeCard(c, t, cardWidth)
		d.lanCard(c, t)
		d.servicesCard(c, t)
		d.memoryCard(c, t)
	})
}

// homeCard is one card of the row: the panel glass with room around it.
func homeCard(c *ui.Context, t *ui.Theme, key string) ui.Element {
	return panelCard(c, t).Key(key).Padding(homeCardPadY, homeCardPadX).Gap(gapMedium)
}

// cardTitle is a card's name.
func cardTitle(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(fontEmphasis).FontWeight(weightSemibold).NoWrap()
}

// modeCard chooses the outbound mode, with what the mode does under it.
func (d *Dashboard) modeCard(c *ui.Context, t *ui.Theme, cardWidth float32) {
	homeCard(c, t, "modeCard").Children(func() {
		cardTitle(c, locales.OutboundMode())
		segment := (cardWidth - 2*homeCardPadX - 2*switchInset) / float32(len(modeLabels()))
		d.modeSwitch(c, pageSwitchStyle(t, segment))
		captionText(c, t, modeHints()[d.modeIndex]).MaxLines(2)
	})
}

// lanAddress is the address a device on the network reaches this computer at,
// and the port the proxy listens on; both "" while the computer is on no
// network or the access is not read yet.
func lanAddress(status model.LANStatus, loaded bool) (host string, port string) {
	if !loaded || len(status.Addresses) == 0 {
		return "", ""
	}
	host, port, err := net.SplitHostPort(status.Addresses[0])
	if err != nil {
		return status.Addresses[0], ""
	}
	return host, port
}

// lanCard shows this computer's address on the network, with a button that
// copies it, over the LAN proxy's switch; it is the same switch as the
// settings', which keeps the account and the other details.
func (d *Dashboard) lanCard(c *ui.Context, t *ui.Theme) {
	l := &d.lan
	host, port := lanAddress(l.status, l.loaded)
	locked := !l.loaded || l.busy || d.controlsLocked()
	homeCard(c, t, "lanCard").Children(func() {
		ui.Row(c).Key("lanTitle").AlignItems(ui.Center).Children(func() {
			cardTitle(c, locales.LanIP())
			ui.Box(c).Grow(1)
			if host != "" {
				iconButton(c, locales.Copy(), iconCopy, mainSoftButton(t), func() {
					c.WriteClipboard(host)
					c.Toast(locales.Copied() + host)
				})
			}
		})
		if host == "" {
			mutedText(c, t, lanMissing(l.loaded))
		} else {
			ui.Text(c, host).Font("monospace").FontSize(fontHeadline).FontWeight(weightSemibold).NoWrap()
		}
		ui.Row(c).Key("lanSwitch").Gap(gapMedium).Padding(lanSwitchPad).Radius(radiusTile).AlignItems(ui.Center).
			Background(t.Text.Alpha(neutralFillAlpha / 2)).Children(func() {
			ui.Box(c).Key("lanIcon").Width(serviceMarkTile).Height(serviceMarkTile).Radius(serviceMarkTile / 2).Center().
				Background(t.Text.Alpha(neutralFillAlpha)).Children(func() {
				ui.Icon(c, iconWifi).Width(serviceMarkGlyph).Height(serviceMarkGlyph).TextColor(t.TextMuted)
			})
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(1).Children(func() {
				ui.Text(c, locales.LanProxy()).FontSize(fontBody).FontWeight(weightSemibold).MaxLines(1)
				captionText(c, t, lanState(l.status.Enabled, port)).MaxLines(1)
			})
			on := l.status.Enabled
			ui.Box(c).Disabled(locked).Children(func() {
				ui.Switch(c.Key("lan-home"), &on).Label(locales.LanProxy()).OnChange(func() {
					d.beginLAN(!l.status.Enabled, !d.settings.settings.LANNoPassword)
				})
			})
		})
	})
}

// lanMissing says why there is no address to show.
func lanMissing(loaded bool) string {
	if !loaded {
		return "—"
	}
	return locales.LanNoNetwork()
}

// lanState is the LAN proxy's state in words, with its port while it is on.
func lanState(enabled bool, port string) string {
	if !enabled {
		return locales.LanStateOff()
	}
	if port == "" {
		return locales.LanStateOn()
	}
	return locales.LanStateOnPort(port)
}

// servicesCard is the services check at a glance: how many work, the check's
// button, a way to the dialog that lists every service, and the first few
// services with their time.
func (d *Dashboard) servicesCard(c *ui.Context, t *ui.Theme) {
	s := &d.unlock.services
	homeCard(c, t, "servicesCard").Children(func() {
		ui.Row(c).Key("servicesTitle").Gap(gapSmall).AlignItems(ui.Center).Children(func() {
			cardTitle(c, locales.ServicesTitle())
			ui.Box(c).Grow(1)
			d.servicesPill(c, t)
			label, icon := locales.ServicesCheck(), iconBolt
			if s.running {
				label, icon = locales.UnlockStop(), iconClose
			}
			iconButton(c, label, icon, mainSoftButton(t), d.toggleServices)
			iconButton(c, locales.ServicesAll(), iconChevronRight, mainSoftButton(t), func() { s.open = true })
		})
		ui.Grid(c).Key("serviceTiles").Columns(2).Gap(gapSmall).Children(func() {
			for _, svc := range model.UnlockServices[:serviceTilesShown] {
				r, checked := s.results[svc]
				serviceTile(c, t, svc, r, checked, func() { s.open = true })
			}
		})
	})
}

// servicesPill is the services card's state: that none was checked yet, how
// far a check is, that every service works, or how many do.
func (d *Dashboard) servicesPill(c *ui.Context, t *ui.Theme) {
	s := &d.unlock.services
	results := make([]model.UnlockResult, 0, len(s.results))
	for _, r := range s.results {
		results = append(results, r)
	}
	total := len(model.UnlockServices)
	working := workingCount(results)
	text, fill, ink := locales.ServicesIdle(), t.Text.Alpha(neutralFillAlpha), t.TextMuted
	switch {
	case s.running:
		text = fmt.Sprintf("%d/%d", len(results), total)
	case len(results) == 0:
	case working == total:
		text, fill, ink = locales.ServicesAllWork(), t.Accent.Alpha(heroDelayFill), textOn(t, accentInk(t))
	default:
		text, fill, ink = fmt.Sprintf("%d/%d", working, total), t.Warning.Alpha(heroDelayFill), textOn(t, t.Warning)
	}
	ui.Box(c).Key("servicesPill").Height(heroDelayHeight).Padding(0, gapSmall).Radius(heroDelayHeight / 2).Center().
		Background(fill).Children(func() {
		ui.Text(c, text).FontSize(fontSecondary).FontWeight(weightSemibold).TextColor(ink).FontFeatures("tnum").NoWrap().
			Translate(0, -opticalLift).ContentTransition(ui.ContentTransition{Numeric: true})
	})
}

// serviceTile is one service on the card: its mark with a dot for its state,
// and its time, or its state when it does not work. Its tooltip names the
// region and the route.
func serviceTile(c *ui.Context, t *ui.Theme, svc model.UnlockService, r model.UnlockResult, checked bool, open func()) {
	tile := ui.Row(c).Key(string(svc)).Gap(gapSmall).Padding(gapSmall, gapSmall).Radius(radiusTile).AlignItems(ui.Center).
		Background(t.Text.Alpha(neutralFillAlpha / 2)).Cursor(ui.CursorPointer).OnClick(open)
	if checked {
		tile.Tooltip(serviceTileTip(svc, r))
	} else {
		tile.Tooltip(serviceNames[svc])
	}
	tile.Children(func() {
		ui.Box(c).Key("markHolder").Children(func() {
			unlockMarkSized(c, t, svc, serviceTileMark)
			dot := t.TextMuted.Alpha(statsDirectAlpha)
			if checked {
				dot = serviceStateColor(t, r)
			}
			ui.Box(c).Key("dot").Absolute().Right(-2).Bottom(-2).Width(serviceDot).Height(serviceDot).
				Radius(serviceDot/2).Background(dot).Border(1.5, overlaySurface(t))
		})
		switch {
		case checked && works(r) && r.Latency > 0:
			ui.Text(c, delayText(model.Delay{Millis: int(r.Latency / time.Millisecond), Measured: true})).
				FontSize(fontSecondary).FontWeight(weightMedium).FontFeatures("tnum").NoWrap()
		case checked:
			ui.Text(c, serviceState(r)).FontSize(fontSecondary).FontWeight(weightMedium).
				TextColor(serviceStateColor(t, r)).MaxLines(1)
		default:
			captionText(c, t, "—")
		}
	})
}

// memoryCard is the physical memory Hop and its core hold now: the sum, and a
// bar split between the two with each one's amount under it.
func (d *Dashboard) memoryCard(c *ui.Context, t *ui.Theme) {
	m := d.memory
	total := m.App + m.Core
	homeCard(c, t, "memoryCard").Children(func() {
		ui.Row(c).Key("memoryTitle").AlignItems(ui.End).Children(func() {
			cardTitle(c, locales.StatsMemory())
			ui.Box(c).Grow(1)
			if d.memoryKnown {
				statsAmount(c, t, int64(total), fontHeadline)
			} else {
				mutedText(c, t, "—")
			}
		})
		share := float32(0)
		if total > 0 {
			share = float32(m.App) / float32(total)
		}
		ui.Box(c).Key("memoryBar").Height(statMemoryBar).Label(locales.StatsMemory()).Draw(func(p *ui.Painter, r ui.Rect) {
			drawMemoryBar(p, r, share, t)
		})
		if d.memoryKnown {
			ui.Column(c).Key("memorySplit").Gap(gapTight).Children(func() {
				statsSplit(c, t, "Hop", int64(m.App), t.Accent)
				statsSplit(c, t, locales.StatsMemoryCore(), int64(m.Core), t.TextMuted.Alpha(statsDirectAlpha))
			})
		}
	})
}

// drawMemoryBar fills the bar with Hop's share in the accent and the core's
// after it, with a small gap between the two.
func drawMemoryBar(p *ui.Painter, r ui.Rect, share float32, t *ui.Theme) {
	radius := r.H / 2
	split := r.W * share
	core := t.TextMuted.Alpha(statsDirectAlpha)
	switch {
	case split <= statMemoryGap:
		p.Fill(r, core, radius)
	case split >= r.W-statMemoryGap:
		p.Fill(r, t.Accent, radius)
	default:
		p.Fill(ui.Rect{X: r.X, Y: r.Y, W: split - statMemoryGap/2, H: r.H}, t.Accent, radius)
		p.Fill(ui.Rect{X: r.X + split + statMemoryGap/2, Y: r.Y, W: r.W - split - statMemoryGap/2, H: r.H}, core, radius)
	}
}
