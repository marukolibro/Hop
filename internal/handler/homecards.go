package handler

import (
	"fmt"
	"strconv"
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
	// modeButtonHeight is a mode's button.
	modeButtonHeight float32 = 40
	// controlBarPad pads the control bar; controlBarMinWidth is the content
	// width from which it is one capsule, modeGroupBasis the width the outbound
	// mode asks for before the switches wrap under it, and controlDividerHeight
	// the divider between the two.
	controlBarPad        float32 = 8
	controlBarMinWidth   float32 = 760
	modeGroupBasis       float32 = 380
	controlDividerHeight float32 = 22
	captureGroupBasis    float32 = 220
	captureToggleBasis   float32 = 110
	// serviceTilesShown is how many services the services card shows; the
	// dialog behind it lists them all.
	serviceTilesShown = 4
	// servicesStripMinWidth is the content width from which the services card
	// is one strip; serviceTileBasis is the width a tile asks for before the
	// tiles wrap.
	servicesStripMinWidth float32 = 780
	serviceTileBasis      float32 = 104
	// serviceMarkRatio is a mark's glyph as a share of the tile it sits on.
	serviceMarkRatio float32 = 0.54
)

// nodesPaneWidth is the width of the content column beside the sidebar, less
// its gutters.
func nodesPaneWidth(c *ui.Context) float32 {
	width, _ := c.Size()
	return width - sidebarWidthFor(width) - hairlineWidth - 2*pagePadding
}

// homeCards is what sits between the hero and the nodes: the control bar, which
// says how traffic is handled, then the services card. The LAN proxy and the
// memory Hop holds are rarely used, so they live in the settings.
func (d *Dashboard) homeCards(c *ui.Context, t *ui.Theme) {
	d.controlBar(c, t)
	ui.Row(c.Key("homeCards")).Wrap().Gap(gapMedium).GapY(gapMedium).AlignItems(ui.Stretch).Children(func() {
		d.servicesCard(c, t)
	})
}

// homeCard is one card of the row: the panel glass, as round and as lifted as
// the hero, with room around it.
func homeCard(c *ui.Context, t *ui.Theme, key string) ui.Element {
	return panelCard(c, t).Key(key).Grow(1).Basis(homeCardMinWidth).MinWidth(0).
		Padding(homeCardPadY, homeCardPadX).Gap(gapMedium).Radius(heroRadius).Shadow(0, 3, 12, 0, glassCardShadow(t))
}

// cardTitle is a card's icon and its name.
func cardTitle(c *ui.Context, t *ui.Theme, icon *ui.SVG, title string) {
	ui.Icon(c, icon).Width(cardHeaderIcon).Height(cardHeaderIcon).TextColor(t.TextMuted)
	ui.Text(c, title).FontSize(fontSecondary).FontWeight(weightSemibold).TextColor(t.Text.Alpha(cardTitleInk)).NoWrap().
		Translate(0, -opticalLift)
}

// cardHeader is a card's title row: its icon, its name, and what trails it.
func cardHeader(c *ui.Context, t *ui.Theme, key string, icon *ui.SVG, title string, trailing func()) {
	ui.Row(c.Key(key)).Gap(gapSmall).Height(cardHeaderHeight).AlignItems(ui.Center).Children(func() {
		cardTitle(c, t, icon, title)
		ui.Box(c).Grow(1)
		if trailing != nil {
			trailing()
		}
	})
}

// controlBar is how traffic is handled, on one bar, in the order it happens:
// the two ways traffic reaches Hop as switches, then the outbound mode as
// three buttons, the chosen one filled with the accent. In a wide pane it is
// one capsule; in a narrow one the mode wraps under the switches.
func (d *Dashboard) controlBar(c *ui.Context, t *ui.Theme) {
	single := nodesPaneWidth(c) >= controlBarMinWidth
	radius := heroRadius
	if single {
		radius = (modeButtonHeight + 2*controlBarPad) / 2
	}
	bar := ui.Row(c.Key("controlBar")).Wrap().Gap(gapMedium).GapY(gapSmall).AlignItems(ui.Center).
		Padding(controlBarPad).Radius(radius).
		Border(hairlineWidth, glassMaterialLine(t)).Shadow(0, 3, 12, 0, glassCardShadow(t))
	if onGlass(t) {
		bar.Background(glassPanel(t))
	} else {
		bar.Background(t.Surface)
	}
	bar.Children(func() {
		// Wrapped, the switches share their line as the modes share theirs.
		group := ui.Row(c)
		if !single {
			group = group.Grow(1).Basis(captureGroupBasis).MinWidth(0)
		}
		d.captureGroup(c, t, group)
		if single {
			ui.Box(c.Key("divider")).Width(hairlineWidth).Height(controlDividerHeight).Background(glassStrongLine(t))
		}
		d.modeGroup(c, t)
	})
}

// modeGroup is the outbound mode: its name, then a button for each mode. Each
// button's tooltip says what the mode does.
func (d *Dashboard) modeGroup(c *ui.Context, t *ui.Theme) {
	disabled := d.modeLocked()
	group := ui.Row(c.Key("modeGroup")).Grow(1).Basis(modeGroupBasis).MinWidth(0).Gap(gapMedium).AlignItems(ui.Center).
		Disabled(disabled)
	if disabled {
		group.Opacity(disabledOpacity)
	}
	icons := []*ui.SVG{iconList, iconGlobe, iconDirect}
	group.Children(func() {
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
	b := ui.ButtonBase(c.Key("mode-" + strconv.Itoa(index))).Label(label).Grow(1).Basis(0).MinWidth(0).
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
	b.OnClick(func() { d.chooseMode(index) })
	b.Children(func() {
		ui.Icon(c, icon).Width(cardHeaderIcon + 2).Height(cardHeaderIcon + 2).TextColor(ink)
		weight := weightMedium
		if chosen {
			weight = weightSemibold
		}
		ui.Text(c, label).FontSize(fontBody).FontWeight(weight).TextColor(ink).NoWrap().Translate(0, -opticalLift)
	})
}

// chooseMode switches to the mode at index unless it is the one in use or the
// mode cannot change yet.
func (d *Dashboard) chooseMode(index int) {
	if d.modeIndex == index || d.modeLocked() {
		return
	}
	d.modeIndex = index
	d.setMode(service.Modes[index])
}

// captureGroup is how traffic reaches Hop: the system proxy and TUN, each a
// switch on a capsule, laid out in group.
func (d *Dashboard) captureGroup(c *ui.Context, t *ui.Theme, group ui.Element) {
	group.Key("captureGroup").Gap(gapSmall).AlignItems(ui.Center).Children(func() {
		d.systemProxyToggle(c, t)
		d.tunToggle(c, t)
	})
}

// systemProxyToggle turns the system proxy on and off.
func (d *Dashboard) systemProxyToggle(c *ui.Context, t *ui.Theme) {
	spec, tip := d.systemProxySpec()
	captureToggle(c, t, spec, tip, func() { d.beginSystemProxy(!d.sysProxy.on) })
}

// systemProxySpec is the system proxy's switch and its tooltip. While another
// app holds the system proxy it cannot be turned on, and the tooltip says who
// holds it.
func (d *Dashboard) systemProxySpec() (pillSpec, string) {
	s := &d.sysProxy
	tip := locales.SystemProxyHint()
	if holder := d.systemProxyHolder(); holder != "" {
		tip = holder
	}
	return pillSpec{
		label: locales.SystemProxy(), on: s.on, busy: s.busy, hint: d.trafficHint(),
		disabled: !s.loaded || (s.status.Owner == model.ProxyOwnerOther && !s.on),
	}, tip
}

// tunToggle turns TUN on and off.
func (d *Dashboard) tunToggle(c *ui.Context, t *ui.Theme) {
	spec, tip := d.tunSpec()
	captureToggle(c, t, spec, tip, func() { d.beginTun(!d.tun.on) })
}

// tunSpec is TUN's switch and its tooltip. While another client's TUN holds the
// default routes it cannot be turned on, and the tooltip says who holds them.
func (d *Dashboard) tunSpec() (pillSpec, string) {
	s := &d.tun
	tip := locales.TunHint()
	if holder := d.tunHolder(); holder != "" {
		tip = holder
	}
	return pillSpec{
		label: locales.Tun(), on: s.on, busy: s.busy,
		disabled: !s.loaded || (d.controlsLocked() && !s.busy) || (s.taken && !s.on),
	}, tip
}

// captureToggle is a capsule as tall as a mode button, with a switch before
// its name. It turns green while on, which is what marks a live proxy; a
// spinner takes the switch's place while the change runs, and the hint ring
// breathes around it while it is the one to turn on.
func captureToggle(c *ui.Context, t *ui.Theme, spec pillSpec, tip string, onClick func()) {
	b := ui.ButtonBase(c.Key(spec.label)).Label(spec.label).Tooltip(tip).Height(modeButtonHeight).
		Grow(1).Basis(captureToggleBasis).MinWidth(0).Padding(0, gapMedium).Gap(gapSmall).
		Radius(modeButtonHeight / 2).AlignItems(ui.Center).Justify(ui.Center).Disabled(spec.disabled)
	fill, line, weight := t.Text.Alpha(neutralFillAlpha), ui.Transparent, weightMedium
	switch {
	case spec.on:
		fill, line, weight = t.Success.Alpha(statusOnFillAlpha), t.Success.Alpha(statusOnLineAlpha), weightSemibold
	case b.Hovered() && !spec.disabled && !spec.busy:
		fill = t.Text.Alpha(2 * neutralFillAlpha)
	}
	b.Background(b.AnimateColor("fill", fill, ui.Smooth)).Border(1, b.AnimateColor("line", line, ui.Smooth))
	scale := float32(1)
	if b.Pressed() && !spec.busy {
		scale = pressedScale
	}
	b.AnimatePose(ui.Pose{Scale: scale}, ui.Snappy)
	if spec.disabled {
		b.Opacity(disabledOpacity)
	}
	if !spec.disabled && !spec.busy {
		b.Cursor(ui.CursorPointer)
	}
	b.OnClick(func() {
		if !spec.busy {
			onClick()
		}
	})
	b.Children(func() {
		if spec.hint {
			hintRing(c, t.Success, modeButtonHeight/2)
		}
		switchLead(c, t, spec)
		ui.Text(c, spec.label).FontSize(fontBody).FontWeight(weight).NoWrap().Translate(0, -opticalLift)
	})
}

// servicesCard is the services check at a glance: how many work, the check's
// button, a way to the dialog that lists every service, and the first few
// services with their time. In a wide pane it is one strip, the name, the
// services and the buttons side by side, so it takes the height of a row of
// tiles; in a narrow one the name and buttons sit over the tiles.
func (d *Dashboard) servicesCard(c *ui.Context, t *ui.Theme) {
	homeCard(c, t, "servicesCard").Children(func() {
		if nodesPaneWidth(c) >= servicesStripMinWidth {
			ui.Row(c.Key("servicesStrip")).Gap(gapMedium).AlignItems(ui.Center).Children(func() {
				ui.Row(c.Key("servicesName")).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
					cardTitle(c, t, iconShieldCheck, locales.ServicesTitle())
					d.servicesPill(c, t)
				})
				d.serviceTiles(c, t, ui.Row(c).Grow(1).Basis(0).MinWidth(0))
				d.servicesActions(c, t)
			})
			return
		}
		cardHeader(c, t, "servicesTitle", iconShieldCheck, locales.ServicesTitle(), func() {
			d.servicesPill(c, t)
			d.servicesActions(c, t)
		})
		d.serviceTiles(c, t, ui.Row(c))
	})
}

// servicesActions are the check's button and the way to the dialog that lists
// every service.
func (d *Dashboard) servicesActions(c *ui.Context, t *ui.Theme) {
	s := &d.unlock.services
	label, icon := locales.ServicesCheck(), iconBolt
	if s.running {
		label, icon = locales.UnlockStop(), iconClose
	}
	ui.Row(c.Key("servicesActions")).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
		iconButton(c, label, icon, mainSoftButton(t), d.toggleServices)
		iconButton(c, locales.ServicesAll(), iconChevronRight, mainSoftButton(t), func() { s.open = true })
	})
}

// serviceTiles lays the first few services out in row, wrapping when the
// tiles do not fit side by side.
func (d *Dashboard) serviceTiles(c *ui.Context, t *ui.Theme, row ui.Element) {
	s := &d.unlock.services
	row.Key("serviceTiles").Wrap().Gap(gapSmall).GapY(gapSmall).Children(func() {
		for _, svc := range model.UnlockServices[:serviceTilesShown] {
			r, checked := s.results[svc]
			serviceTile(c, t, svc, r, checked, func() { s.open = true })
		}
	})
}

// servicesPill is the services card's state: that none was checked yet, how
// far a check is, that every service works, or how many do.
func (d *Dashboard) servicesPill(c *ui.Context, t *ui.Theme) {
	text, fill, ink := d.servicesSummary(t)
	ui.Box(c.Key("servicesPill")).Height(heroDelayHeight).Padding(0, gapSmall).Radius(heroDelayHeight / 2).Center().
		Background(fill).Children(func() {
		ui.Text(c, text).FontSize(fontSecondary).FontWeight(weightSemibold).TextColor(ink).FontFeatures("tnum").NoWrap().
			Translate(0, -opticalLift).ContentTransition(ui.ContentTransition{Numeric: true})
	})
}

// servicesSummary is the services check in a few words, with the fill and ink
// that mark it: that none was checked yet, how far a check is, that every
// service works, or how many do.
func (d *Dashboard) servicesSummary(t *ui.Theme) (string, ui.Color, ui.Color) {
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
	return text, fill, ink
}

// serviceTile is one service on the card: its mark with a dot for its state,
// and its time, or its state when it does not work. Its tooltip names the
// region and the route.
func serviceTile(c *ui.Context, t *ui.Theme, svc model.UnlockService, r model.UnlockResult, checked bool, open func()) {
	tile := ui.Row(c.Key(string(svc))).Grow(1).Basis(serviceTileBasis).MinWidth(0).Gap(gapSmall).Padding(gapSmall, gapSmall).
		Radius(radiusTile).AlignItems(ui.Center).Background(t.Text.Alpha(neutralFillAlpha / 2)).Cursor(ui.CursorPointer).OnClick(open)
	if checked {
		tile.Tooltip(serviceTileTip(svc, r))
	} else {
		tile.Tooltip(serviceNames[svc])
	}
	tile.Children(func() {
		ui.Box(c.Key("markHolder")).Children(func() {
			unlockMarkSized(c, t, svc, serviceTileMark)
			dot := t.TextMuted.Alpha(statsDirectAlpha)
			if checked {
				dot = serviceStateColor(t, r)
			}
			ui.Box(c.Key("dot")).Absolute().Right(-2).Bottom(-2).Width(serviceDot).Height(serviceDot).
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

// memorySplit is one line of the legend: a dot, its name and its amount.
func memorySplit(c *ui.Context, t *ui.Theme, label string, bytes uint64, color ui.Color) {
	r := formatBytes(int64(bytes))
	ui.Row(c.Key(label)).Gap(gapTight).AlignItems(ui.Center).Children(func() {
		statsDot(c, color)
		mutedText(c, t, label)
		ui.Text(c, r.value).FontSize(fontSecondary).FontFeatures("tnum")
		captionText(c, t, r.unit)
	})
}
