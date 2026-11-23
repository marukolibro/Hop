package handler

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
	"hop/locales"
)

// homeRightTab values select what the right column of the frosted shelf shows.
const (
	homeTabServices = iota
	homeTabLive
)

// homeTier classifies the current profile so the left shelf column adapts
// cleanly without clutter or empty space.
type homeTier int

const (
	tierEmpty homeTier = iota
	tierSingleVPS
	tierFewNodes
	tierMultiAirport
)

// horizonHome is Hop's main home page: an open-air ocean horizon stage with a
// subtle live throughput tide, a single-layer frosted lower shelf, and a
// centered floating full-pill capsule dock straddling the exact seam between
// the two.
func (d *Dashboard) horizonHome(c *ui.Context, t *ui.Theme) {
	width, _ := c.Size()
	contentW := min(max(0, width-2*pagePadding), homeContentMaxWidth)
	ui.Column(c.Key("horizonHome")).Grow(1).Basis(0).MinHeight(0).Children(func() {
		d.horizonStage(c, t, contentW)
		d.horizonShelf(c, t, width, contentW)
		d.floatingDockOverlay(c, t, width)
	})
}

// headerSubscriptionPill sits in the title bar beside the status capsule,
// showing the active subscription and its short usage readout, and opens the
// unified Subscription Switcher palette on click.
func (d *Dashboard) headerSubscriptionPill(c *ui.Context, t *ui.Theme) {
	s := &d.subscription
	if !s.loaded {
		return
	}
	width, _ := c.Size()
	active, ok := s.active()
	label := locales.SubAdd()
	meta := ""
	if ok {
		label = active.Name
		if width >= homeHeaderMetaMinW {
			meta = d.subscriptionShortMeta(active)
		}
	}
	b := ui.ButtonBase(c.Key("headerSubPill")).Label(locales.HomeSwitchSubscription()).
		Height(homeHeaderPillHeight).Padding(0, 10).Gap(gapTight).
		Radius(homeHeaderPillHeight / 2).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	fill, line, ink := horizonPillIdle(t), ui.Transparent, t.Text
	switch {
	case d.subPickerOpen:
		fill, line, ink = floatingDockFill(t), t.Accent, accentInk(t)
	case b.Hovered():
		fill = horizonPillHover(t)
	}
	b.Background(b.AnimateColor("fill", fill, ui.Smooth)).Border(hairlineWidth, b.AnimateColor("line", line, ui.Smooth))
	b.OnClick(func() {
		if ok || len(s.view.Items) > 0 {
			d.subPickerOpen = !d.subPickerOpen
			return
		}
		d.openEditor(editorAdd, "")
	})
	b.Children(func() {
		ui.Text(c, label).FontSize(fontSecondary).FontWeight(weightSemibold).TextColor(ink).
			MaxLines(1).MaxWidth(120).Translate(0, -opticalLift)
		if meta != "" {
			captionText(c, t, meta).FontFeatures("tnum").NoWrap().Translate(0, -opticalLift)
		}
		if ok || len(s.view.Items) > 0 {
			ui.Icon(c, iconChevron).Width(iconInline).Height(iconInline).TextColor(t.TextMuted)
		}
	})
}

// subscriptionShortMeta formats a concise usage label for the header capsule.
func (d *Dashboard) subscriptionShortMeta(item model.SubscriptionSummary) string {
	if item.Source == model.SourceRemote && item.HasUsage && item.Usage.Total > 0 {
		used := formatBytes(item.Usage.Upload + item.Usage.Download)
		total := formatBytes(item.Usage.Total)
		if used.unit == total.unit {
			return fmt.Sprintf("%s / %s %s", used.value, total.value, total.unit)
		}
		return fmt.Sprintf("%s %s / %s %s", used.value, used.unit, total.value, total.unit)
	}
	monthly := service.MonthlyProxyForSource(d.stats, item.ID)
	used := formatBytes(monthly.Upload + monthly.Download)
	return used.value + " " + used.unit
}

// horizonStage is the compact open-air upper sky: a soft radial light behind
// the gauge, the centered exit identity + quick actions on the left, and
// open-air live throughput + rhythm bars on the right.
func (d *Dashboard) horizonStage(c *ui.Context, t *ui.Theme, contentW float32) {
	route := d.view.Route
	tier := d.classifyHomeTier()
	stage := ui.Box(c.Key("horizonStage")).Height(homeHorizonHeight).AlignItems(ui.Center).Justify(ui.Center)
	if onGlass(t) {
		alpha := stage.AnimateTo("glowAlpha", d.heroGlowAlpha(flowIntensity(d.last.Down))*0.85, ui.Smooth)
		color := stage.AnimateColor("glowColor", d.heroGlowColor(t), ui.Smooth)
		stage.RadialGradient(ui.RadialGradient{
			Stops:  []ui.GradientStop{{Color: color.Alpha(alpha), At: 0}, {Color: color.Alpha(0), At: 1}},
			Center: ui.Point{X: 0.20, Y: 0.52},
			Radius: 165,
		})
	} else {
		stage.LinearGradient(ui.LinearGradient{
			Angle: 180,
			From:  t.Background.Mix(t.Accent, glassHorizonSkyTint),
			To:    t.Background,
		})
	}
	stage.Children(func() {
		ui.Row(c.Key("horizonContent")).Width(contentW).Padding(homeHorizonPadTop, 0, homeHorizonPadBottom, 0).
			Gap(gapLarge).AlignItems(ui.Center).Children(func() {
			d.horizonIdentity(c, t, route, tier)
			ui.Box(c).Grow(1)
			d.horizonTelemetry(c, t)
		})
	})
}

// horizonBarSamples returns a normalized 0..1 series from trafficHistory for
// the compact rhythm strip, or nil while idle.
func (d *Dashboard) horizonBarSamples() []float32 {
	const count = homeTrafficBarsCount
	hasTraffic := d.last.Down > 0
	if !hasTraffic {
		for _, v := range d.trafficHistory {
			if v > 0 {
				hasTraffic = true
				break
			}
		}
	}
	if !hasTraffic {
		return nil
	}
	out := make([]float32, count)
	peak := max(d.peakDown, d.last.Down)
	if peak < 64*1024 {
		peak = 64 * 1024
	}
	start := max(0, len(d.trafficHistory)-count)
	src := d.trafficHistory[start:]
	pad := count - len(src)
	for i := 0; i < pad; i++ {
		out[i] = 0
	}
	for i, v := range src {
		ratio := float32(v) / float32(peak)
		if ratio > 1 {
			ratio = 1
		}
		out[pad+i] = ratio
	}
	return out
}

// horizonIdentity is the left cluster of the horizon stage: dual-arc gauge,
// exit name + inline latency, quiet protocol/IP line, and frosted action pills.
func (d *Dashboard) horizonIdentity(c *ui.Context, t *ui.Theme, route []model.RouteHop, tier homeTier) {
	ui.Row(c.Key("horizonIdentity")).Gap(gapLarge).AlignItems(ui.Center).Shrink(1).MinWidth(0).Children(func() {
		if len(route) > 0 {
			routeGauge(c, t, route[len(route)-1], homeHorizonGaugeSize, d.last)
		}
		ui.Column(c.Key("horizonExit")).Gap(gapTight).Shrink(1).MinWidth(0).Children(func() {
			if len(route) == 0 {
				ui.Text(c, locales.HeroEmpty()).FontSize(fontExit).FontWeight(weightSemibold).LetterSpacing(-0.4)
				d.horizonActions(c, t, tier)
				return
			}
			hop := route[len(route)-1]
			ui.Row(c.Key("exitTitleRow")).Gap(gapSmall).AlignItems(ui.End).Children(func() {
				ui.Text(c, horizonExitTitle(hop)).FontSize(fontExit).FontWeight(weightSemibold).LetterSpacing(-0.5).
					MaxLines(1).Shrink(1)
				ui.Text(c, delayText(hop.Node.Delay)).FontSize(fontEmphasis).FontWeight(weightSemibold).
					TextColor(delayColor(t, hop.Node.Delay)).FontFeatures("tnum").NoWrap().
					ContentTransition(ui.ContentTransition{Numeric: true})
			})
			d.horizonMetaRow(c, t, route, hop)
			d.horizonActions(c, t, tier)
		})
	})
}

// horizonExitTitle names the exit without repeating the flag already centered
// inside the dual-arc gauge.
func horizonExitTitle(hop model.RouteHop) string {
	node := hop.Node
	switch {
	case hop.Group:
		return shownName(hop.Name)
	case node.PlanNote:
		return locales.PlanInfoNode()
	case node.Via != "":
		return locales.RouteDirect()
	}
	name := strings.TrimSpace(node.Region + " " + node.Title)
	if name == "" {
		return shownName(node.Name)
	}
	return name
}

// horizonMetaRow shows protocol facts and the clickable exit IP readout on one
// quiet line under the exit title.
func (d *Dashboard) horizonMetaRow(c *ui.Context, t *ui.Theme, route []model.RouteHop, hop model.RouteHop) {
	var parts []string
	if via := heroVia(route, hop); via != "" && len(route) > 2 {
		parts = append(parts, via)
	}
	if !hop.Group && !hop.Node.PlanNote && hop.Node.Via == "" {
		parts = append(parts, nodeLabels(hop.Node)...)
	}
	ui.Row(c.Key("horizonMeta")).Gap(gapTight).AlignItems(ui.Center).Wrap().Children(func() {
		if len(parts) > 0 {
			captionText(c, t, strings.Join(parts, " · ")).MaxLines(1)
		}
		if d.exitIP.status != exitIPIdle {
			if len(parts) > 0 {
				captionText(c, t, "·")
			}
			d.exitIPLine(c, t)
		}
	})
}

// horizonActions renders the primary glass action pills under the exit line.
func (d *Dashboard) horizonActions(c *ui.Context, t *ui.Theme, tier homeTier) {
	locked := d.controlsLocked()
	ui.Row(c.Key("horizonActions")).Gap(gapSmall).Margin(2, 0, 0, 0).AlignItems(ui.Center).Children(func() {
		switch tier {
		case tierEmpty:
			horizonPillButton(c, t, "addSub", locales.SubAddAndUse(), iconPlus, nil, true, locked, false, func() {
				d.openEditor(editorAdd, "")
			})
		case tierSingleVPS:
			d.horizonTestButton(c, t, true)
			horizonPillButton(c, t, "manageSub", locales.SubManage(), iconList, nil, false, false, false, func() {
				d.subscription.managing = true
			})
		default:
			horizonPillButton(c, t, "switchNode", locales.HomeSwitchNode(), nil, iconChevron, true, locked, false, func() {
				d.nodePickerQuery = ""
				d.nodePickerOpen = true
			})
			d.horizonTestButton(c, t, false)
		}
	})
}

// horizonTestButton tests latency for the active group.
func (d *Dashboard) horizonTestButton(c *ui.Context, t *ui.Theme, primary bool) {
	group := d.activeGroup
	if group == "" && len(d.view.Groups) > 0 {
		group = d.view.Groups[0].Name
	}
	label := locales.NodesTestDelay()
	if d.testing != "" {
		label = locales.NodesTesting()
	}
	horizonPillButton(c, t, "testDelay", label, iconBolt, nil, primary, d.controlsLocked() || group == "", d.testing != "", func() {
		if group != "" {
			d.testDelay(group)
		}
	})
}

// horizonPillButton is a frosted rounded pill action for the open-air horizon.
func horizonPillButton(c *ui.Context, t *ui.Theme, key, label string, leadIcon, trailIcon *ui.SVG, primary, disabled, busy bool, onClick func()) {
	b := ui.ButtonBase(c.Key(key)).Label(label).Height(homeHeroActionHeight).Padding(0, 12).Gap(5).
		Radius(homeHeroActionHeight / 2).AlignItems(ui.Center).Disabled(disabled || busy)
	fill, hover, line, ink := horizonPillIdle(t), horizonPillHover(t), ui.Transparent, t.Text
	if primary {
		fill = floatingDockFill(t)
		hover = floatingDockFill(t).Mix(t.Accent, 0.08)
		line = glassMaterialLine(t)
	}
	cur := fill
	if b.Hovered() && !disabled && !busy {
		cur = hover
	}
	b.Background(b.AnimateColor("fill", cur, ui.Smooth))
	if line.A > 0 {
		b.Border(hairlineWidth, line).Shadow(0, 2, 8, 0, glassButtonShadow(t))
	}
	if disabled {
		b.Opacity(disabledOpacity)
	} else if !busy {
		b.Cursor(ui.CursorPointer)
	}
	scale := float32(1)
	if b.Pressed() && !busy {
		scale = pressedScale
	}
	b.AnimatePose(ui.Pose{Scale: scale}, ui.Snappy)
	b.OnClick(onClick)
	b.Children(func() {
		switch {
		case busy:
			spinner(c, iconInline, ink)
		case leadIcon != nil:
			ui.Icon(c, leadIcon).Width(iconInline).Height(iconInline).TextColor(ink)
		}
		weight := weightMedium
		if primary {
			weight = weightSemibold
		}
		ui.Text(c, label).FontSize(fontSecondary).FontWeight(weight).TextColor(ink).NoWrap().Translate(0, -opticalLift)
		if trailIcon != nil {
			ui.Icon(c, trailIcon).Width(iconInline).Height(iconInline).TextColor(t.TextMuted)
		}
	})
}

// horizonTelemetry sits on the right side of the open-air sky: real-time
// download/upload speeds, session peak, and an 18-bar live rhythm strip.
func (d *Dashboard) horizonTelemetry(c *ui.Context, t *ui.Theme) {
	down := formatRate(d.last.Down)
	up := formatRate(d.last.Up)
	peak := formatRate(max(d.peakDown, d.last.Down))
	samples := d.horizonBarSamples()
	ui.Column(c.Key("horizonTelemetry")).Gap(5).AlignItems(ui.End).Children(func() {
		ui.Row(c.Key("downSpeed")).Gap(gapTight).AlignItems(ui.End).Children(func() {
			ui.Text(c, "↓").FontSize(fontEmphasis).FontWeight(weightSemibold).TextColor(accentInk(t))
			ui.Text(c, down.value).FontSize(fontExit).FontWeight(weightSemibold).LetterSpacing(-0.5).
				FontFeatures("tnum").NoWrap().ContentTransition(ui.ContentTransition{Numeric: true})
			captionText(c, t, down.unit).NoWrap()
		})
		ui.Row(c.Key("upSpeed")).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
			ui.Row(c.Key("upVal")).Gap(3).AlignItems(ui.End).Children(func() {
				ui.Text(c, "↑").FontSize(fontCaption).FontWeight(weightMedium).TextColor(uploadColor(t))
				ui.Text(c, up.value+" "+up.unit).FontSize(fontSecondary).FontWeight(weightMedium).
					TextColor(t.TextMuted).FontFeatures("tnum").NoWrap()
			})
			if d.peakDown > 0 || d.last.Down > 0 {
				captionText(c, t, "·").NoWrap()
				captionText(c, t, locales.HomePeakRate(peak.value+" "+peak.unit)).FontFeatures("tnum").NoWrap()
			}
		})
		barsW := float32(homeTrafficBarsCount)*homeTrafficBarWidth + float32(homeTrafficBarsCount-1)*homeTrafficBarGap
		ui.Box(c.Key("trafficBars")).Width(barsW).Height(homeTrafficBarHeight).Margin(2, 0, 0, 0).Draw(func(p *ui.Painter, r ui.Rect) {
			drawHorizonBars(p, r, samples, t)
		})
	})
}

// drawHorizonBars paints the compact 18-bar live traffic rhythm strip.
func drawHorizonBars(p *ui.Painter, r ui.Rect, samples []float32, t *ui.Theme) {
	n := homeTrafficBarsCount
	for i := 0; i < n; i++ {
		v := float32(0)
		if len(samples) > 0 {
			idx := i * len(samples) / n
			if idx >= len(samples) {
				idx = len(samples) - 1
			}
			v = samples[idx]
		}
		barH := max(3, v*r.H)
		x := r.X + float32(i)*(homeTrafficBarWidth+homeTrafficBarGap)
		y := r.Y + r.H - barH
		alpha := float32(0.18) + float32(i)/float32(n)*0.52
		if len(samples) == 0 {
			alpha = 0.18
		} else if i >= n-2 {
			alpha = 0.85
		}
		p.Fill(ui.Rect{X: x, Y: y, W: homeTrafficBarWidth, H: barH}, t.Accent.Alpha(alpha), homeTrafficBarWidth/2)
	}
}

// floatingDockOverlay positions the centered content-hugging full-pill capsule
// dock directly over the horizontal seam between horizonStage and horizonShelf.
func (d *Dashboard) floatingDockOverlay(c *ui.Context, t *ui.Theme, windowWidth float32) {
	half := homeDockHeight / 2
	ui.Row(c.Key("floatingDockOverlay")).Absolute().Left(0).Right(0).
		Top(homeHorizonHeight - half).Height(homeDockHeight).
		Justify(ui.Center).AlignItems(ui.Center).Children(func() {
		dock := ui.Row(c.Key("floatingDock")).Height(homeDockHeight).Padding(homeDockPad).Gap(10).
			Radius(half).AlignItems(ui.Center).
			Material(glass.Glass{Tint: floatingDockGlassTint(t)}).
			Shadow(0, 10, 28, 0, floatingDockShadow(t))
		dock.Children(func() {
			spSpec, spTip := d.systemProxySpec()
			dockCapturePill(c, t, "dockSysProxy", spSpec, spTip, func() { d.beginSystemProxy(!d.sysProxy.on) })
			tunSpec, tunTip := d.tunSpec()
			dockCapturePill(c, t, "dockTun", tunSpec, tunTip, func() { d.beginTun(!d.tun.on) })
			ui.Box(c.Key("dockSep1")).Width(hairlineWidth).Height(homeDockSepHeight).Background(glassStrongLine(t))
			d.dockModeSegment(c, t)
			if windowWidth >= homeDockSvcMinWidth {
				ui.Box(c.Key("dockSep2")).Width(hairlineWidth).Height(homeDockSepHeight).Background(glassStrongLine(t))
				d.dockServicesButton(c, t)
			}
		})
	})
}

// dockCapturePill is a compact emerald switch pill inside the floating dock.
func dockCapturePill(c *ui.Context, t *ui.Theme, key string, spec pillSpec, tip string, onClick func()) {
	b := ui.ButtonBase(c.Key(key)).Label(spec.label).Tooltip(tip).Height(homeDockInnerHeight).
		Padding(0, 14).Gap(gapSmall).Radius(homeDockInnerHeight / 2).
		AlignItems(ui.Center).Disabled(spec.disabled)
	fill, line, weight := ui.Transparent, ui.Transparent, weightMedium
	switch {
	case spec.on:
		fill, line, weight = t.Success.Alpha(statusOnFillAlpha), t.Success.Alpha(statusOnLineAlpha), weightSemibold
	case b.Hovered() && !spec.disabled && !spec.busy:
		fill = horizonPillIdle(t)
	}
	b.Background(b.AnimateColor("fill", fill, ui.Smooth)).Border(hairlineWidth, b.AnimateColor("line", line, ui.Smooth))
	scale := float32(1)
	if b.Pressed() && !spec.busy {
		scale = pressedScale
	}
	b.AnimatePose(ui.Pose{Scale: scale}, ui.Snappy)
	if spec.disabled {
		b.Opacity(disabledOpacity)
	} else if !spec.busy {
		b.Cursor(ui.CursorPointer)
	}
	b.OnClick(func() {
		if !spec.busy {
			onClick()
		}
	})
	b.Children(func() {
		if spec.hint {
			hintRing(c, t.Success, homeDockInnerHeight/2)
		}
		switchLead(c, t, spec)
		ui.Text(c, spec.label).FontSize(fontBody).FontWeight(weight).NoWrap().Translate(0, -opticalLift)
	})
}

// dockModeSegment renders the compact, fixed-proportion Rule / Global / Direct
// pill switcher with the same spring-sliding thumb motion as pageTabs, kept in
// the blue accent palette.
func (d *Dashboard) dockModeSegment(c *ui.Context, t *ui.Theme) {
	inset := (homeDockInnerHeight - homeDockModeHeight) / 2
	labels := modeLabels()
	hints := modeHints()
	segW := fitSegment(c, labels, homeDockModeMinWidth, fontBody, weightSemibold, 14)
	disabled := d.modeLocked()
	track := ui.Box(c.Key("dockModes")).Label(locales.OutboundMode()).
		Width(segW*float32(len(labels)) + 2*inset).Height(homeDockInnerHeight).
		Radius(homeDockInnerHeight / 2).Background(horizonPillIdle(t)).Disabled(disabled)
	if disabled {
		track.Opacity(disabledOpacity)
	}
	x := track.AnimateTo("thumb", float32(d.modeIndex)*segW, ui.Snappy)
	track.Children(func() {
		ui.Box(c.Key("dockModeThumb")).Absolute().Left(inset+x).Top(inset).Width(segW).Height(homeDockModeHeight).
			Radius(homeDockModeHeight/2).Background(t.Accent).Shadow(0, 1, 4, 0, t.Accent.Alpha(0.28))
		ui.Row(c.Key("dockModeLabels")).Absolute().Left(inset).Top(inset).Children(func() {
			for i, label := range labels {
				idx := i
				chosen := d.modeIndex == idx
				seg := ui.ButtonBase(c.Key("dockMode-" + strconv.Itoa(idx))).Label(label).Tooltip(hints[idx]).
					Width(segW).Height(homeDockModeHeight).Radius(homeDockModeHeight / 2).Center().
					Background(ui.Transparent).Cursor(ui.CursorPointer)
				ink, weight := t.TextMuted, weightMedium
				switch {
				case chosen:
					ink, weight = t.AccentText, weightSemibold
				case seg.Hovered():
					ink = t.Text
				}
				color := seg.AnimateColor("ink", ink, ui.Smooth)
				seg.OnClick(func() { d.chooseMode(idx) })
				seg.Children(func() {
					ui.Text(c, label).FontSize(fontBody).FontWeight(weight).TextColor(color).NoWrap().Translate(0, -opticalLift)
				})
			}
		})
	})
}

// dockServicesButton shows the compact live services summary inside the dock
// and opens the full services dialog on click.
func (d *Dashboard) dockServicesButton(c *ui.Context, t *ui.Theme) {
	summary, dotColor := d.homeServicesDockState(t)
	label := locales.HomeServicesDock(summary)
	b := ui.ButtonBase(c.Key("dockServices")).Label(label).Height(homeDockInnerHeight).
		Padding(0, 14).Gap(gapSmall).Radius(homeDockInnerHeight / 2).
		AlignItems(ui.Center).Cursor(ui.CursorPointer)
	fill := ui.Transparent
	if b.Hovered() {
		fill = horizonPillIdle(t)
	}
	b.Background(b.AnimateColor("fill", fill, ui.Smooth))
	b.OnClick(func() { d.unlock.services.open = true })
	b.Children(func() {
		ui.Box(c.Key("dot")).Width(homeShelfDotSize + 1).Height(homeShelfDotSize + 1).Radius((homeShelfDotSize + 1) / 2).Background(dotColor)
		ui.Text(c, label).FontSize(fontBody).FontWeight(weightMedium).TextColor(t.Text).
			FontFeatures("tnum").NoWrap().Translate(0, -opticalLift)
	})
}

// homeServicesDockState returns the concise count string and indicator dot
// color for the dock's services pill.
func (d *Dashboard) homeServicesDockState(t *ui.Theme) (string, ui.Color) {
	s := &d.unlock.services
	total := len(model.UnlockServices)
	if s.running {
		return fmt.Sprintf("%d/%d", s.done, total), t.Accent
	}
	if len(s.results) == 0 {
		return locales.ServicesIdle(), t.TextMuted
	}
	results := make([]model.UnlockResult, 0, len(s.results))
	for _, r := range s.results {
		results = append(results, r)
	}
	working := workingCount(results)
	color := t.Success
	if working < total {
		color = t.Warning
	}
	if working == 0 {
		color = t.Danger
	}
	return fmt.Sprintf("%d/%d", working, total), color
}

// horizonShelf is the quiet single-layer frosted lower half of the Home page.
// Above homeTwoColMinWidth it renders two aligned borderless columns; below it
// stacks cleanly.
func (d *Dashboard) horizonShelf(c *ui.Context, t *ui.Theme, windowWidth, contentW float32) {
	shelfTopPad := homeDockHeight/2 + homeShelfPadTop
	ui.Column(c.Key("horizonShelfWrap")).Grow(1).Basis(0).MinHeight(0).Background(horizonShelfFill(t)).Children(func() {
		ui.Box(c.Key("shelfTopRim")).Height(hairlineWidth).Background(t.Border)
		ui.Scroll(c.Key("horizonShelf")).Grow(1).Basis(0).MinHeight(0).Children(func() {
			ui.Column(c.Key("shelfCenter")).AlignItems(ui.Center).Padding(shelfTopPad, pagePadding, scrollEnd, pagePadding).Children(func() {
				if windowWidth >= homeTwoColMinWidth {
					ui.Row(c.Key("shelfCols")).Width(contentW).Gap(homeShelfGap).AlignItems(ui.Start).Children(func() {
						ui.Column(c.Key("shelfLeftCol")).Grow(1.06).Basis(0).MinWidth(0).Gap(gapMedium).Children(func() {
							d.shelfLeft(c, t)
						})
						ui.Column(c.Key("shelfRightCol")).Grow(1).Basis(0).MinWidth(0).Gap(gapMedium).Children(func() {
							d.shelfRight(c, t)
						})
					})
					return
				}
				ui.Column(c.Key("shelfStacked")).Width(contentW).Gap(gapSection).Children(func() {
					d.shelfLeft(c, t)
					horizontalHairline(c, t)
					d.shelfRight(c, t)
				})
			})
		})
	})
}

// classifyHomeTier inspects the current view and groups to pick the cleanest
// presentation for the left shelf column.
func (d *Dashboard) classifyHomeTier() homeTier {
	if len(d.view.Groups) == 0 {
		return tierEmpty
	}
	mainGroup, ok := d.mainSelectorGroup()
	if !ok {
		return tierMultiAirport
	}
	nodes := groupRealNodes(mainGroup)
	subGroups := 0
	for _, g := range d.view.Groups {
		if g.Name != mainGroup.Name && g.Kind != model.GroupKindRouting {
			subGroups++
		}
	}
	if len(nodes) <= 1 && subGroups == 0 && len(mainGroup.Others) <= 1 {
		return tierSingleVPS
	}
	if len(nodes) <= homeFewNodesLimit && subGroups == 0 {
		return tierFewNodes
	}
	return tierMultiAirport
}

// mainSelectorGroup returns the primary selectable group (Main first, then the
// first manual selector, then any selectable group, else the first group).
func (d *Dashboard) mainSelectorGroup() (model.GroupView, bool) {
	for _, g := range d.view.Groups {
		if g.Kind == model.GroupKindMain {
			return g, true
		}
	}
	for _, g := range d.view.Groups {
		if g.Type == model.GroupSelector {
			return g, true
		}
	}
	for _, g := range d.view.Groups {
		if g.Selectable {
			return g, true
		}
	}
	if len(d.view.Groups) > 0 {
		return d.view.Groups[0], true
	}
	return model.GroupView{}, false
}

// groupRealNodes flattens all proxy nodes in a group's regions.
func groupRealNodes(group model.GroupView) []model.NodeView {
	var nodes []model.NodeView
	for _, region := range group.Regions {
		nodes = append(nodes, region.Nodes...)
	}
	return nodes
}

// totalProfileNodes counts unique real proxy nodes across groups (using the
// largest group's region list so duplicates across routing groups aren't
// double-counted).
func (d *Dashboard) totalProfileNodes() int {
	seen := map[string]struct{}{}
	for _, g := range d.view.Groups {
		for _, r := range g.Regions {
			for _, n := range r.Nodes {
				seen[n.Name] = struct{}{}
			}
		}
	}
	return len(seen)
}

// splitRuleGroups returns selectable routing groups for the compact bottom
// split-rule pills on VPS profiles.
func (d *Dashboard) splitRuleGroups() []model.GroupView {
	var out []model.GroupView
	for _, g := range d.view.Groups {
		if g.Kind == model.GroupKindRouting && g.Selectable {
			out = append(out, g)
		}
	}
	return out
}

// shelfLeft renders the adaptive left column of the frosted shelf.
func (d *Dashboard) shelfLeft(c *ui.Context, t *ui.Theme) {
	switch d.classifyHomeTier() {
	case tierEmpty:
		d.shelfLeftEmpty(c, t)
	case tierSingleVPS:
		d.shelfLeftSingleVPS(c, t)
	case tierFewNodes:
		d.shelfLeftFewNodes(c, t)
	default:
		d.shelfLeftMultiAirport(c, t)
	}
}

func (d *Dashboard) shelfLeftEmpty(c *ui.Context, t *ui.Theme) {
	ui.Column(c.Key("shelfEmpty")).Gap(gapMedium).Padding(gapSmall, 0).Children(func() {
		ui.Text(c, locales.SubEmptyTitle()).FontSize(fontEmphasis).FontWeight(weightSemibold)
		mutedText(c, t, locales.SubEmptyNote())
		button(c, buttonSpec{label: locales.SubAddAndUse(), icon: iconPlus, style: primaryButton(t), disabled: d.controlsLocked()}, func() {
			d.openEditor(editorAdd, "")
		})
	})
}

// shelfLeftSingleVPS renders borderless key-value line details for a 1-node
// VPS profile plus compact split-rule pills when routing groups exist.
func (d *Dashboard) shelfLeftSingleVPS(c *ui.Context, t *ui.Theme) {
	shelfSectionHeader(c, t, "vpsHead", locales.HomeCurrentLine(), "", nil)
	var proto, location, cfgLabel string
	if len(d.view.Route) > 0 {
		hop := d.view.Route[len(d.view.Route)-1]
		labels := nodeLabels(hop.Node)
		if len(labels) > 0 {
			proto = strings.Join(labels, " · ")
		}
		if hop.Node.Region != "" {
			location = strings.TrimSpace(shownFlag(hop.Node.Flag) + " " + hop.Node.Region)
		}
	}
	if d.exitIP.status == exitIPReady {
		country := service.CountryName(d.exitIP.ip.Country)
		if location == "" {
			location = country
		}
		if country != "" {
			location = d.exitIP.ip.Address.String() + " · " + country
		}
	}
	if proto == "" {
		proto = "—"
	}
	if location == "" {
		location = "—"
	}
	if active, ok := d.subscription.active(); ok {
		if active.Source == model.SourceLocal {
			cfgLabel = active.Name + " · " + locales.HomeSingleNodeLocal()
		} else {
			cfgLabel = active.Name + " · " + active.Host
		}
	} else {
		cfgLabel = "—"
	}
	ui.Column(c.Key("vpsFacts")).Gap(2).Children(func() {
		shelfFactRow(c, t, "proto", locales.HomeProtocol(), proto, false)
		horizontalHairline(c, t)
		shelfFactRow(c, t, "loc", locales.HomeExitLocation(), location, true)
		horizontalHairline(c, t)
		shelfFactRow(c, t, "cfg", locales.HomeCurrentConfig(), cfgLabel, false)
	})
	d.shelfSplitRulesBar(c, t)
}

func shelfFactRow(c *ui.Context, t *ui.Theme, key, label, value string, mono bool) {
	ui.Row(c.Key(key)).Padding(homeShelfRowPadY, 2).Gap(gapMedium).AlignItems(ui.Center).Children(func() {
		mutedText(c, t, label).NoWrap()
		ui.Box(c).Grow(1)
		v := ui.Text(c, value).FontSize(fontSecondary).FontWeight(weightMedium).TextColor(t.Text).MaxLines(1).Shrink(1)
		if mono {
			v.FontFeatures("tnum")
		}
	})
}

// shelfLeftFewNodes renders borderless 1-click node rows for 2-4 node setups.
func (d *Dashboard) shelfLeftFewNodes(c *ui.Context, t *ui.Theme) {
	group, ok := d.mainSelectorGroup()
	if !ok {
		return
	}
	nodes := groupRealNodes(group)
	shelfSectionHeader(c, t, "fewHead", locales.HomeAvailableNodes(len(nodes)), locales.HomeExpandList(), func() {
		d.activeGroup = group.Name
		d.nodePickerQuery = ""
		d.nodePickerOpen = true
	})
	ui.Column(c.Key("fewNodesList")).Gap(4).Disabled(d.controlsLocked()).Children(func() {
		for _, node := range nodes {
			d.shelfNodeRow(c, t, group, node)
		}
	})
	d.shelfSplitRulesBar(c, t)
}

// shelfNodeRow is one borderless 1-click selectable node row in the frosted
// shelf.
func (d *Dashboard) shelfNodeRow(c *ui.Context, t *ui.Theme, group model.GroupView, node model.NodeView) {
	isActive := node.Name == group.Selected
	groupName, nodeName := group.Name, node.Name
	row := ui.Row(c.Key("shelfNode:"+nodeName)).Padding(homeShelfRowPadY, homeShelfRowPadX).Gap(gapSmall).
		Radius(homeShelfRowRadius).AlignItems(ui.Center)
	fill := ui.Transparent
	switch {
	case isActive:
		fill = t.Accent.Alpha(0.11)
	case group.Selectable && row.Hovered():
		fill = horizonPillIdle(t)
	}
	row.Background(row.AnimateColor("fill", fill, ui.Smooth))
	if group.Selectable {
		row.Cursor(ui.CursorPointer).OnClick(func() {
			if nodeName != group.Selected {
				d.selectProxy(groupName, nodeName)
			}
		})
	}
	row.Children(func() {
		if flag := shownFlag(node.Flag); flag != "" {
			ui.Text(c, flag).FontSize(fontEmphasis)
		}
		ui.Column(c.Key("info")).Grow(1).Basis(0).MinWidth(0).Gap(2).Children(func() {
			titleInk, weight := t.Text, weightMedium
			if isActive {
				titleInk, weight = accentInk(t), weightSemibold
			}
			ui.Text(c, tileTitle(node)).FontSize(fontBody).FontWeight(weight).TextColor(titleInk).MaxLines(1)
			if labels := nodeLabels(node); len(labels) > 0 {
				captionText(c, t, strings.Join(labels, " · ")).MaxLines(1)
			}
		})
		latency := delayText(node.Delay)
		latencyInk := delayColor(t, node.Delay)
		if d.testing != "" {
			latency = locales.NodesTesting()
			latencyInk = accentInk(t)
		}
		ui.Text(c, latency).FontSize(fontSecondary).FontWeight(weightMedium).TextColor(latencyInk).
			FontFeatures("tnum").NoWrap().ContentTransition(ui.ContentTransition{Numeric: true})
		if isActive {
			ui.Icon(c.Key("check"), iconCheck).Width(iconInline).Height(iconInline).TextColor(accentInk(t))
		}
	})
}

// shelfLeftMultiAirport renders borderless strategy group rows for multi-node
// airport profiles; clicking any row opens the Node Picker Palette on that
// group.
func (d *Dashboard) shelfLeftMultiAirport(c *ui.Context, t *ui.Theme) {
	totalNodes := d.totalProfileNodes()
	shelfSectionHeader(c, t, "groupsHead", locales.HomeRouteGroups(), locales.HomeAllNodesCount(totalNodes), func() {
		d.nodePickerQuery = ""
		d.nodePickerOpen = true
	})
	groups := d.homePreviewGroups()
	ui.Column(c.Key("airportGroups")).Gap(4).Children(func() {
		for _, g := range groups {
			d.shelfGroupRow(c, t, g)
		}
	})
}

// homePreviewGroups selects the most useful strategy groups (Main, Auto, then
// Routing groups, up to homeGroupsPreview) for the Home shelf.
func (d *Dashboard) homePreviewGroups() []model.GroupView {
	var mainAndAuto, routing, rest []model.GroupView
	for _, g := range d.view.Groups {
		switch g.Kind {
		case model.GroupKindMain, model.GroupKindAuto:
			mainAndAuto = append(mainAndAuto, g)
		case model.GroupKindRouting:
			routing = append(routing, g)
		default:
			rest = append(rest, g)
		}
	}
	ordered := append(mainAndAuto, routing...)
	ordered = append(ordered, rest...)
	if len(ordered) > homeGroupsPreview {
		return ordered[:homeGroupsPreview]
	}
	return ordered
}

// shelfGroupRow is one borderless strategy group row; clicking opens the
// unified Node Picker Palette focused on that group.
func (d *Dashboard) shelfGroupRow(c *ui.Context, t *ui.Theme, group model.GroupView) {
	groupName := group.Name
	isActive := groupName == d.activeGroup
	row := ui.Row(c.Key("shelfGroup:"+groupName)).Padding(homeShelfRowPadY, homeShelfRowPadX).Gap(gapSmall).
		Radius(homeShelfRowRadius).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	fill := ui.Transparent
	if row.Hovered() {
		fill = horizonPillIdle(t)
	} else if isActive && group.Kind == model.GroupKindMain {
		fill = t.Accent.Alpha(0.08)
	}
	row.Background(row.AnimateColor("fill", fill, ui.Smooth))
	row.OnClick(func() {
		d.activeGroup = groupName
		d.nodeRegion = nodeFilterDefault
		d.nodePickerQuery = ""
		d.nodePickerOpen = true
	})
	row.Children(func() {
		groupIcon(c, t, group, sidebarIconSize)
		ui.Column(c.Key("info")).Grow(1).Basis(0).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, groupTitle(group)).FontSize(fontBody).FontWeight(weightMedium).MaxLines(1)
			captionText(c, t, groupDestination(group)).MaxLines(1)
		})
		if group.Exit.Name != "" && group.Kind != model.GroupKindRouting {
			delayLabel(c, t, group.Exit.Delay, fontCaption)
		}
		ui.Icon(c, iconChevronRight).Width(iconInline).Height(iconInline).TextColor(t.TextMuted)
	})
}

// shelfSplitRulesBar shows inline 1-click split-rule pills at the bottom of
// VPS shelves when routing groups exist in the profile.
func (d *Dashboard) shelfSplitRulesBar(c *ui.Context, t *ui.Theme) {
	rules := d.splitRuleGroups()
	if len(rules) == 0 {
		return
	}
	if len(rules) > 4 {
		rules = rules[:4]
	}
	ui.Row(c.Key("splitRulesBar")).Margin(gapTight, 0, 0, 0).Gap(gapTight).GapY(gapTight).Wrap().AlignItems(ui.Center).Children(func() {
		captionText(c, t, locales.HomeSplitRules()).NoWrap()
		for _, g := range rules {
			d.splitRulePill(c, t, g)
		}
	})
}

// splitRulePill is a small rounded menu pill for a routing group (e.g. Apple).
func (d *Dashboard) splitRulePill(c *ui.Context, t *ui.Theme, group model.GroupView) {
	groupName := group.Name
	pick := shownName(group.Pick)
	if pick == "" {
		pick = shownName(group.Selected)
	}
	text := groupTitle(group) + " · " + pick
	b := ui.ButtonBase(c.Key("split-"+groupName)).Label(text).Height(homeInlinePillHeight).
		Padding(0, 9).Gap(4).Radius(homeInlinePillHeight / 2).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Disabled(d.controlsLocked())
	fill := horizonPillIdle(t)
	if b.Hovered() {
		fill = horizonPillHover(t)
	}
	b.Background(b.AnimateColor("fill", fill, ui.Smooth))
	b.Menu(func(m *ui.Menu) {
		for _, opt := range group.Others {
			optName := opt.Name
			if m.Item(shownName(opt.Title)).Checked(optName == group.Selected).Chosen() {
				if optName != group.Selected {
					d.selectProxy(groupName, optName)
				}
			}
		}
		for _, region := range group.Regions {
			for _, node := range region.Nodes {
				nodeName := node.Name
				if m.Item(nodeDisplay(node)).Checked(nodeName == group.Selected).Chosen() {
					if nodeName != group.Selected {
						d.selectProxy(groupName, nodeName)
					}
				}
			}
		}
	})
	b.Children(func() {
		ui.Text(c, text).FontSize(fontCaption).FontWeight(weightMedium).TextColor(t.Text).NoWrap().Translate(0, -opticalLift)
		ui.Icon(c, iconChevron).Width(10).Height(10).TextColor(t.TextMuted)
	})
}

// shelfSectionHeader is a clean borderless section heading with an optional
// trailing link.
func shelfSectionHeader(c *ui.Context, t *ui.Theme, key, title, linkLabel string, onLink func()) {
	ui.Row(c.Key(key)).Height(22).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).FontSize(fontSecondary).FontWeight(weightSemibold).TextColor(t.TextMuted).NoWrap()
		ui.Box(c).Grow(1)
		if linkLabel != "" && onLink != nil {
			shelfLink(c, t, key+"-link", linkLabel, onLink)
		}
	})
}

// shelfLink is a quiet accent text link with a trailing chevron.
func shelfLink(c *ui.Context, t *ui.Theme, key, label string, onClick func()) {
	b := ui.ButtonBase(c.Key(key)).Label(label).Height(22).Padding(0).Gap(2).
		Background(ui.Transparent).Cursor(ui.CursorPointer)
	ink := accentInk(t)
	if b.Hovered() {
		ink = t.Accent
	}
	b.OnClick(onClick)
	b.Children(func() {
		ui.Text(c, label).FontSize(fontSecondary).FontWeight(weightMedium).TextColor(ink).NoWrap().Translate(0, -opticalLift)
		ui.Icon(c, iconChevronRight).Width(iconInline).Height(iconInline).TextColor(ink)
	})
}

// shelfRight renders the right column of the frosted shelf: borderless
// micro-tabs (`Services` | `Live`), the active view body, and a quiet bottom
// summary footer.
func (d *Dashboard) shelfRight(c *ui.Context, t *ui.Theme) {
	ui.Row(c.Key("shelfRightHead")).Height(24).Gap(gapLarge).AlignItems(ui.Center).Children(func() {
		shelfMicroTab(c, t, "tabServices", locales.HomeTabServices(), d.homeRightTab == homeTabServices, func() {
			d.homeRightTab = homeTabServices
		})
		shelfMicroTab(c, t, "tabLive", locales.HomeTabLive(), d.homeRightTab == homeTabLive, func() {
			d.homeRightTab = homeTabLive
		})
		ui.Box(c).Grow(1)
		if d.homeRightTab == homeTabServices {
			shelfLink(c, t, "allServices", locales.HomeAllServicesCount(len(model.UnlockServices)), func() {
				d.unlock.services.open = true
			})
		} else {
			shelfLink(c, t, "allConns", locales.ConnectionsCount(len(d.connections.data.Active)), func() {
				d.page = pageActivity
				d.activity = activityConnections
			})
		}
	})
	if d.homeRightTab == homeTabServices {
		d.shelfServicesGrid(c, t)
	} else {
		d.shelfLiveConnections(c, t)
	}
	d.shelfRightFooter(c, t)
}

// shelfMicroTab is a borderless tab button with a subtle accent underline when
// active.
func shelfMicroTab(c *ui.Context, t *ui.Theme, key, label string, active bool, onClick func()) {
	b := ui.ButtonBase(c.Key(key)).Label(label).Height(24).Padding(0).Background(ui.Transparent).Cursor(ui.CursorPointer)
	ink, weight := t.TextMuted, weightMedium
	if active {
		ink, weight = t.Text, weightSemibold
	} else if b.Hovered() {
		ink = t.Text
	}
	b.OnClick(onClick)
	b.Children(func() {
		ui.Column(c).Gap(3).AlignItems(ui.Stretch).Children(func() {
			ui.Text(c, label).FontSize(fontSecondary).FontWeight(weight).TextColor(ink).NoWrap()
			line := ui.Transparent
			if active {
				line = t.Accent
			}
			ui.Box(c).Height(2).Radius(1).Background(line)
		})
	})
}

// shelfServicesGrid renders a clean 2-column borderless grid of services:
// all 12 services on multi-node airport profiles (balancing the taller strategy
// group list on the left), and the top 6 on compact VPS profiles. Clicking any
// row opens the full services dialog.
func (d *Dashboard) shelfServicesGrid(c *ui.Context, t *ui.Theme) {
	s := &d.unlock.services
	services := model.UnlockServices
	if d.classifyHomeTier() != tierMultiAirport && len(services) > homeServicesPreview {
		services = services[:homeServicesPreview]
	}
	ui.Grid(c.Key("shelfServicesGrid")).Columns(2).Gap(gapMedium).GapY(2).Children(func() {
		for _, svc := range services {
			r, checked := s.results[svc]
			d.shelfServiceItem(c, t, svc, r, checked)
		}
	})
}

func (d *Dashboard) shelfServiceItem(c *ui.Context, t *ui.Theme, svc model.UnlockService, r model.UnlockResult, checked bool) {
	dot := t.TextMuted.Alpha(0.45)
	meta := "—"
	if checked {
		dot = homeServiceDotColor(t, r)
		if works(r) {
			var parts []string
			if r.Region != "" {
				parts = append(parts, r.Region)
			}
			if r.Latency > 0 {
				parts = append(parts, delayText(model.Delay{Millis: int(r.Latency / time.Millisecond), Measured: true}))
			}
			if len(parts) > 0 {
				meta = strings.Join(parts, " · ")
			} else {
				meta = serviceState(r)
			}
		} else {
			meta = serviceState(r)
		}
	}
	row := ui.Row(c.Key("svc-"+string(svc))).Padding(homeShelfRowPadY, 4).Gap(gapSmall).
		Radius(radiusControl).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	if checked {
		row.Tooltip(serviceTileTip(svc, r))
	} else {
		row.Tooltip(serviceNames[svc])
	}
	fill := ui.Transparent
	if row.Hovered() {
		fill = horizonPillIdle(t)
	}
	row.Background(row.AnimateColor("fill", fill, ui.Smooth))
	row.OnClick(func() { d.unlock.services.open = true })
	row.Children(func() {
		ui.Box(c.Key("dot")).Width(homeShelfDotSize).Height(homeShelfDotSize).Radius(homeShelfDotSize / 2).Background(dot)
		ui.Text(c, shortServiceName(svc)).FontSize(fontSecondary).FontWeight(weightMedium).MaxLines(1).Grow(1).Basis(0)
		captionText(c, t, meta).FontFeatures("tnum").NoWrap()
	})
}

// homeServiceDotColor uses emerald green for available services so health
// reads at a glance, matching the dock's services indicator.
func homeServiceDotColor(t *ui.Theme, r model.UnlockResult) ui.Color {
	switch r.State {
	case model.UnlockAvailable:
		return t.Success
	case model.UnlockPartial:
		return textOn(t, t.Warning)
	case model.UnlockBlocked:
		return textOn(t, t.Danger)
	default:
		return t.TextMuted
	}
}

// shortServiceName keeps service labels crisp inside the compact 2-column grid.
func shortServiceName(svc model.UnlockService) string {
	if svc == model.UnlockYouTube {
		return "YouTube"
	}
	return serviceNames[svc]
}

// shelfLiveConnections renders the top active connections inside the right
// shelf column; clicking any row jumps to the Activity page.
func (d *Dashboard) shelfLiveConnections(c *ui.Context, t *ui.Theme) {
	conns := d.connections.data.Active
	if len(conns) == 0 {
		ui.Box(c.Key("shelfNoConns")).Padding(gapLarge, 0).Children(func() {
			mutedText(c, t, locales.ConnectionsEmpty())
		})
		return
	}
	limit := homeLiveConnsPreview
	if d.classifyHomeTier() == tierMultiAirport {
		limit = 8
	}
	limit = min(len(conns), limit)
	ui.Column(c.Key("shelfLiveList")).Gap(2).Children(func() {
		for _, conn := range conns[:limit] {
			row := ui.Row(c.Key("live-"+conn.ID)).Padding(6, 4).Gap(gapSmall).
				Radius(radiusControl).AlignItems(ui.Center).Cursor(ui.CursorPointer)
			fill := ui.Transparent
			if row.Hovered() {
				fill = horizonPillIdle(t)
			}
			row.Background(row.AnimateColor("fill", fill, ui.Smooth))
			row.OnClick(func() {
				d.page = pageActivity
				d.activity = activityConnections
			})
			row.Children(func() {
				title := conn.Host
				if conn.Process != "" {
					title = conn.Process + " · " + conn.Host
				}
				ui.Text(c, title).FontSize(fontSecondary).FontWeight(weightMedium).MaxLines(1).Grow(1).Basis(0)
				total := formatBytes(conn.Download + conn.Upload)
				captionText(c, t, total.value+" "+total.unit).FontFeatures("tnum").NoWrap()
				captionText(c, t, "·").NoWrap()
				captionText(c, t, routeLabel(conn.Chain)).MaxLines(1).MaxWidth(96)
			})
		}
	})
}

// shelfRightFooter sits quietly beneath the services/live list with today's
// traffic total and the 10-minute auto-check cadence note.
func (d *Dashboard) shelfRightFooter(c *ui.Context, t *ui.Theme) {
	today := d.stats.Today
	total := today.ProxyDown + today.ProxyUp + today.DirectDown + today.DirectUp
	size := formatBytes(total)
	ui.Column(c.Key("shelfFooter")).Margin(gapTight, 0, 0, 0).Gap(gapSmall).Children(func() {
		horizontalHairline(c, t)
		ui.Row(c).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
			captionText(c, t, locales.HomeTodaySummary(size.value+" "+size.unit, len(d.connections.data.Active))).
				FontFeatures("tnum").MaxLines(1).Grow(1).Basis(0)
			captionText(c, t, locales.HomeAutoCheckNote()).NoWrap()
		})
	})
}
