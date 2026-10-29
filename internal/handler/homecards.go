package handler

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
	"hop/locales"
)

const (
	// homeCardPadY and homeCardPadX pad a card of the row.
	homeCardPadY float32 = 16
	homeCardPadX float32 = 18
	// cardHeaderHeight is a card's title row, as tall as the icon buttons at
	// its end; cardHeaderIcon is the icon before the title and cardTitleInk
	// how dark the title is against the card.
	cardHeaderHeight float32 = 24
	cardHeaderIcon   float32 = 14
	cardTitleInk     float32 = 0.72
	// modeButtonHeight is a mode's button; the bar holds them with
	// modeBarPad around.
	modeButtonHeight float32 = 40
	modeBarPad       float32 = 8
	// serviceTilesShown is how many services the services card shows; the
	// dialog behind it lists them all.
	serviceTilesShown = 4
	// serviceMarkRatio is a mark's glyph as a share of the tile it sits on.
	serviceMarkRatio float32 = 0.54
	// lanHalfWidth is what each half of the LAN card asks for, so they sit side
	// by side once the card is wide enough for both.
	lanHalfWidth float32 = 190
	// lanSwitchPad pads the row that holds the LAN proxy's switch, and
	// lanOnFill is how much of the accent that row takes while it is on.
	lanSwitchPad float32 = 9
	lanOnFill    float32 = 0.14
)

// nodesPaneWidth is the width of the content column beside the sidebar, less
// its gutters.
func nodesPaneWidth(c *ui.Context) float32 {
	width, _ := c.Size()
	return width - sidebarWidthFor(width) - hairlineWidth - 2*pagePadding
}

// homeCards is what sits between the hero and the nodes: the outbound mode as
// one bar across, then the cards for the address other devices use with the
// LAN proxy's switch, the services, and the memory Hop and its core hold. The
// cards wrap as the window narrows, and a card left alone on a row grows to
// fill it.
func (d *Dashboard) homeCards(c *ui.Context, t *ui.Theme) {
	d.modeBar(c, t)
	ui.Row(c).Key("homeCards").Wrap().Gap(gapMedium).GapY(gapMedium).AlignItems(ui.Stretch).Children(func() {
		d.lanCard(c, t)
		d.servicesCard(c, t)
		d.memoryCard(c, t)
	})
}

// homeCard is one card of the row: the panel glass, as round and as lifted as
// the hero, with room around it.
func homeCard(c *ui.Context, t *ui.Theme, key string) ui.Element {
	return homeCardSized(c, t, key, 1, homeCardMinWidth)
}

// homeCardSized is homeCard asking for basis of the row's width and growing
// by grow against the others, for a card that has more to show.
func homeCardSized(c *ui.Context, t *ui.Theme, key string, grow float32, basis float32) ui.Element {
	return panelCard(c, t).Key(key).Grow(grow).Basis(basis).MinWidth(0).
		Padding(homeCardPadY, homeCardPadX).Gap(gapMedium).Radius(heroRadius).Shadow(0, 3, 12, 0, glassCardShadow(t))
}

// cardHeader is a card's title row: its icon, its name, and what trails it.
func cardHeader(c *ui.Context, t *ui.Theme, key string, icon *ui.SVG, title string, trailing func()) {
	ui.Row(c).Key(key).Gap(gapSmall).Height(cardHeaderHeight).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icon).Width(cardHeaderIcon).Height(cardHeaderIcon).TextColor(t.TextMuted)
		ui.Text(c, title).FontSize(fontSecondary).FontWeight(weightSemibold).TextColor(t.Text.Alpha(cardTitleInk)).NoWrap().
			Translate(0, -opticalLift)
		ui.Box(c).Grow(1)
		if trailing != nil {
			trailing()
		}
	})
}

// modeBar chooses the outbound mode: its name, then a button for each mode
// across the row, the chosen one filled with the accent. Each button's
// tooltip says what the mode does.
func (d *Dashboard) modeBar(c *ui.Context, t *ui.Theme) {
	disabled := d.modeLocked()
	bar := ui.Row(c).Key("modeBar").Gap(gapMedium).AlignItems(ui.Center).Disabled(disabled).
		Padding(modeBarPad, modeBarPad, modeBarPad, homeCardPadX).Radius((modeButtonHeight+2*modeBarPad)/2).
		Border(hairlineWidth, glassMaterialLine(t)).Shadow(0, 3, 12, 0, glassCardShadow(t))
	if onGlass(t) {
		bar.Background(glassPanel(t))
	} else {
		bar.Background(t.Surface)
	}
	if disabled {
		bar.Opacity(disabledOpacity)
	}
	icons := []*ui.SVG{iconList, iconGlobe, iconDirect}
	bar.Children(func() {
		ui.Icon(c, iconRoute).Width(cardHeaderIcon).Height(cardHeaderIcon).TextColor(t.TextMuted)
		ui.Text(c, locales.OutboundMode()).FontSize(fontSecondary).FontWeight(weightSemibold).
			TextColor(t.Text.Alpha(cardTitleInk)).NoWrap().Translate(0, -opticalLift)
		for i, label := range modeLabels() {
			d.modeButton(c, t, i, label, icons[i])
		}
	})
}

// modeButton is one mode on the bar.
func (d *Dashboard) modeButton(c *ui.Context, t *ui.Theme, index int, label string, icon *ui.SVG) {
	chosen := d.modeIndex == index
	b := ui.ButtonBase(c).Key("mode-" + strconv.Itoa(index)).Label(label).Grow(1).Basis(0).MinWidth(0).
		Height(modeButtonHeight).Radius(modeButtonHeight / 2).Gap(gapSmall).Cursor(ui.CursorPointer).
		Tooltip(modeHints()[index])
	fill, ink := t.Text.Alpha(neutralFillAlpha), t.Text
	switch {
	case chosen:
		fill, ink = t.Accent, t.AccentText
	case b.Hovered():
		fill = t.Text.Alpha(2 * neutralFillAlpha)
	}
	b.Background(b.AnimateColor("fill", fill, ui.Smooth))
	b.OnClick(func() {
		if d.modeIndex == index || d.modeLocked() {
			return
		}
		d.modeIndex = index
		d.setMode(service.Modes[index])
	})
	b.Children(func() {
		ui.Icon(c, icon).Width(cardHeaderIcon + 2).Height(cardHeaderIcon + 2).TextColor(ink)
		weight := weightMedium
		if chosen {
			weight = weightSemibold
		}
		ui.Text(c, label).FontSize(fontBody).FontWeight(weight).TextColor(ink).NoWrap().Translate(0, -opticalLift)
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

// lanShareText is what the card's copy button puts on the clipboard: the
// address, and once the proxy is on its port and, when an account is asked
// for, the account and its password, so a device can be set up from it.
func lanShareText(status model.LANStatus, host string, port string) string {
	lines := []string{"IP: " + host}
	if !status.Enabled {
		return lines[0]
	}
	if port != "" {
		lines = append(lines, locales.LanPort()+": "+port)
	}
	if account := status.Account; account != (model.LANCredential{}) {
		lines = append(lines, locales.LanUsername()+": "+account.Username, locales.LanPassword()+": "+account.Password)
	}
	return strings.Join(lines, "\n")
}

// lanCard shows this computer's address on the network and the LAN proxy's
// switch. Once the proxy is on the card widens and shows its account and
// password beside them, says they can be turned off, and leads to the
// settings, which keep the whole of it; the copy button takes everything a
// device needs.
func (d *Dashboard) lanCard(c *ui.Context, t *ui.Theme) {
	l := &d.lan
	host, port := lanAddress(l.status, l.loaded)
	locked := !l.loaded || l.busy || d.controlsLocked()
	grow, basis := float32(1), homeCardMinWidth
	if l.status.Enabled {
		grow, basis = 2, 2*lanHalfWidth+gapLarge+2*homeCardPadX
	}
	homeCardSized(c, t, "lanCard", grow, basis).Children(func() {
		cardHeader(c, t, "lanTitle", iconLaptop, locales.LanIP(), func() {
			if host != "" {
				iconButton(c, locales.Copy(), iconCopy, mainSoftButton(t), func() {
					c.WriteClipboard(lanShareText(l.status, host, port))
					if l.status.Enabled {
						c.Toast(locales.LanCopiedAll())
						return
					}
					c.Toast(locales.Copied() + host)
				})
			}
		})
		// The two halves sit side by side, and one under the other where the
		// card is narrow.
		ui.Row(c).Key("lanBody").Grow(1).Wrap().Gap(gapLarge).GapY(gapMedium).AlignItems(ui.Stretch).Children(func() {
			ui.Column(c).Key("lanMain").Grow(1).Basis(lanHalfWidth).MinWidth(0).Gap(gapMedium).Children(func() {
				if host == "" {
					mutedText(c, t, lanMissing(l.loaded))
				} else {
					ui.Text(c, host).FontSize(fontExit).FontWeight(weightSemibold).FontFeatures("tnum").LetterSpacing(-0.4).NoWrap()
				}
				ui.Box(c).Grow(1)
				// While the proxy is on, its row and its icon take the accent.
				rowFill, badgeFill, badgeInk := t.Text.Alpha(neutralFillAlpha/2), t.Text.Alpha(neutralFillAlpha), t.TextMuted
				if l.status.Enabled {
					rowFill, badgeFill, badgeInk = t.Accent.Alpha(lanOnFill), t.Accent, t.AccentText
				}
				ui.Row(c).Key("lanSwitch").Gap(gapMedium).Padding(lanSwitchPad).Radius(radiusTile).AlignItems(ui.Center).
					Background(rowFill).Children(func() {
					ui.Box(c).Key("lanIcon").Width(serviceMarkTile).Height(serviceMarkTile).Radius(serviceMarkTile / 2).Center().
						Background(badgeFill).Children(func() {
						ui.Icon(c, iconWifi).Width(serviceMarkGlyph).Height(serviceMarkGlyph).TextColor(badgeInk)
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
			if l.status.Enabled {
				d.lanAccount(c, t)
			}
		})
	})
}

// lanAccount is what the card adds while the proxy is on: the account and its
// password when devices must give them, a line saying they can be turned off
// (or, with none asked for, that anyone on the network can connect), and the
// way to the settings.
func (d *Dashboard) lanAccount(c *ui.Context, t *ui.Theme) {
	account := d.lan.status.Account
	asks := account != (model.LANCredential{})
	ui.Column(c).Key("lanAccount").Grow(1).Basis(lanHalfWidth).MinWidth(0).Gap(gapSmall).Children(func() {
		if asks {
			lanValueRow(c, t, locales.LanUsername(), account.Username)
			lanValueRow(c, t, locales.LanPassword(), account.Password)
		}
		hint := locales.LanHintAccount()
		if !asks {
			hint = locales.LanOpenWarning()
		}
		captionText(c, t, hint)
		textButton(c, t, locales.LanOpenSettings(), iconChevronRight, false, false, d.openLANSettings).AlignSelf(ui.Start)
	})
}

// lanValueRow is a name and its value, the value in the monospace face.
func lanValueRow(c *ui.Context, t *ui.Theme, name string, value string) {
	ui.Row(c).Key(name).Gap(gapMedium).AlignItems(ui.Center).Children(func() {
		mutedText(c, t, name).NoWrap()
		ui.Box(c).Grow(1)
		ui.Text(c, value).Font("monospace").FontSize(fontSecondary).FontWeight(weightMedium).MaxLines(1)
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
		cardHeader(c, t, "servicesTitle", iconShieldCheck, locales.ServicesTitle(), func() {
			d.servicesPill(c, t)
			label, icon := locales.ServicesCheck(), iconBolt
			if s.running {
				label, icon = locales.UnlockStop(), iconClose
			}
			iconButton(c, label, icon, mainSoftButton(t), d.toggleServices)
			iconButton(c, locales.ServicesAll(), iconChevronRight, mainSoftButton(t), func() { s.open = true })
		})
		ui.Box(c).Grow(1)
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
		text = fmt.Sprintf("%d/%d", s.done, total)
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

// memoryCard is the physical memory Hop and its core hold now: the sum large,
// and under it a bar split between the two with each one's amount.
func (d *Dashboard) memoryCard(c *ui.Context, t *ui.Theme) {
	m := d.memory
	total := m.App + m.Core
	homeCard(c, t, "memoryCard").Children(func() {
		cardHeader(c, t, "memoryTitle", iconMemory, locales.StatsMemory(), nil)
		if d.memoryKnown {
			memoryAmount(c, t, total, fontExit)
		} else {
			mutedText(c, t, "—")
		}
		ui.Box(c).Grow(1)
		share := float32(0)
		if total > 0 {
			share = float32(m.App) / float32(total)
		}
		ui.Box(c).Key("memoryBar").Height(statMemoryBar).Label(locales.StatsMemory()).Draw(func(p *ui.Painter, r ui.Rect) {
			drawMemoryBar(p, r, share, t)
		})
		ui.Row(c).Key("memorySplit").Wrap().Gap(gapLarge).GapY(gapTight).Height(cardHeaderHeight - gapSmall).Children(func() {
			if d.memoryKnown {
				memorySplit(c, t, "Hop", m.App, t.Accent)
				memorySplit(c, t, locales.StatsMemoryCore(), m.Core, t.TextMuted.Alpha(statsDirectAlpha))
			}
		})
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

// memoryAmount is an amount of memory with its unit. The figure changes every
// second, so its digits do not roll as the statistics' do.
func memoryAmount(c *ui.Context, t *ui.Theme, bytes uint64, size float32) {
	r := formatBytes(int64(bytes))
	ui.Row(c).Gap(4).AlignItems(ui.End).Children(func() {
		ui.Text(c, r.value).FontSize(size).FontWeight(weightSemibold).FontFeatures("tnum")
		captionText(c, t, r.unit)
	})
}

// memorySplit is one line of the legend: a dot, its name and its amount.
func memorySplit(c *ui.Context, t *ui.Theme, label string, bytes uint64, color ui.Color) {
	r := formatBytes(int64(bytes))
	ui.Row(c).Key(label).Gap(gapTight).AlignItems(ui.Center).Children(func() {
		statsDot(c, color)
		mutedText(c, t, label)
		ui.Text(c, r.value).FontSize(fontSecondary).FontFeatures("tnum")
		captionText(c, t, r.unit)
	})
}
