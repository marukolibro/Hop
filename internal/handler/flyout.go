package handler

import (
	"math"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
	"hop/locales"
)

const (
	flyoutWidth = 340
	// flyoutHeight is the first panel's height, before it fits its content.
	flyoutHeight  = 640
	flyoutPadding = gapLarge
	fastestCount  = 5
	quickRowH     = 34
	quickRowGap   = 3
	// quickRowsMax is how many routes show before the list scrolls: the
	// panel grows with the routes up to it.
	quickRowsMax = 10
	// quickTestEvery keeps opening the panel again and again from testing
	// every time.
	quickTestEvery = 2 * time.Minute
	// compactSpeedWidth holds "999 KB/s" so the line does not shift as
	// numbers change.
	compactSpeedWidth = 88
	// flyoutGaugeSize is the exit gauge, smaller than the window's.
	flyoutGaugeSize float32 = 52
	// flyoutIdleOpacity softens the exit card gently while nothing sends
	// traffic to Hop, staying legible instead of muddy.
	flyoutIdleOpacity   float32 = 0.68
	flyoutLockedOpacity float32 = 0.5
	flyoutModeHeight    float32 = 34
)

// flyoutView is the menu bar panel: where traffic goes and how fast, the
// mode, and the routes to switch to, without opening the window.
// Its sections rise into place one after another as it opens. It is as tall
// as its content: fit hears the height the last frame laid out, and the
// route list scrolls past quickRowsMax routes.
func (d *Dashboard) flyoutView(c *ui.Context, openWindow func(), quit func(), fit func(height int)) {
	c.SetTheme(d.theme(c))
	t := c.Theme()
	d.failures.flyoutSeen = d.showFailures(c, d.failures.flyoutSeen)
	d.toasts(c, t, flyoutPadding)
	if c.Vibrancy() {
		// Over the panel's material the palette's border is a frame; every
		// outline in the panel takes the translucent line instead.
		over := *t
		over.Border = flyoutLine(t)
		c.SetTheme(&over)
		t = c.Theme()
	}
	c.Root().Background(ui.Transparent)
	panel := ui.Column(c).Fill().Padding(flyoutPadding)
	var border float32
	if !c.Vibrancy() {
		border = 1
		panel.Background(t.Background).Radius(radiusPanel).Border(border, t.Border)
	}
	panel.Children(func() {
		content := ui.Column(c.Key("content")).Gap(14).Shrink(0)
		if h := content.LayoutBounds().H; h > 0 {
			fit(int(math.Ceil(float64(h + 2*(flyoutPadding+border)))))
		}
		d.flyoutContent(c, t, content, openWindow, quit)
	})
}

// flyoutContent is the panel's sections, top to bottom, inside content.
func (d *Dashboard) flyoutContent(c *ui.Context, t *ui.Theme, content ui.Element, openWindow func(), quit func()) {
	content.Children(func() {
		entrance(ui.Row(c.Key("top")).Gap(gapSmall).AlignItems(ui.Center), 0).Children(func() {
			brandLockup(c, t, 20, d.markRun)
			ui.Box(c).Grow(1)
			if d.appUpdate.update.Available && d.appUpdate.enabled {
				// Labelled: a bare arrow here would read as the upload rate.
				button(c, buttonSpec{label: locales.FlyoutNewVersion(), icon: iconUpgrade, style: softButton(t)}, func() {
					d.page = pageSettings
					openWindow()
				})
			}
			ui.Row(c.Key("flyoutStatus")).Height(24).Padding(0, 9).Gap(5).Radius(12).
				AlignItems(ui.Center).Background(horizonPillIdle(t)).Children(func() {
				statusDot(c, d.statusColor(t), d.proxying())
				mutedText(c, t, d.statusText()).NoWrap()
			})
			// Compact quick actions in the flyout header: copy terminal proxy
			// command, open the main window, and quit.
			iconButton(c, locales.SettingsCopyCommand(), iconTerminal, softButton(t), func() {
				c.WriteClipboard(service.TerminalProxyCommand(runtime.GOOS, d.proxyAddress()))
				c.Toast(locales.SettingsCommandCopied())
			}).Tooltip(locales.SettingsCopyCommand())
			iconButton(c, locales.OpenMainWindow(), iconWindow, softButton(t), openWindow).Tooltip(locales.OpenMainWindow())
			iconButton(c, locales.QuitHop(), iconPower, softButton(t), quit).Tooltip(locales.QuitHop())
		})
		d.notice(c, t)
		// The exit and the traffic sit on a soft tinted frosted face aligned
		// with the main window's Horizon aesthetic.
		d.flyoutCard(c, t, func() {
			card := entrance(ui.Column(c.Key("route")), 1).Gap(gapSmall).Padding(13, 14).Radius(radiusPanel + 2)
			if c.Vibrancy() {
				card.Background(overlaySurface(t).Alpha(0.56)).Border(hairlineWidth, t.Border)
			} else {
				card.LinearGradient(heroFace(t, overlaySurface(t))).Border(hairlineWidth, t.Border)
			}
			card.Children(func() {
				d.flyoutRoute(c, t)
			})
		})
		entrance(ui.Column(c.Key("mode")), 3).Gap(gapSmall).Children(func() {
			// In the order it happens, matching the main window's floating dock:
			// emerald capture switches on top, solid-blue sliding mode pills below.
			ui.Row(c.Key("switches")).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
				d.systemProxyPill(c, t).Grow(1).Basis(0)
				d.tunPill(c, t).Grow(1).Basis(0)
			})
			d.flyoutModes(c, t)
			for _, note := range []string{d.systemProxyHint(), d.tunHint()} {
				if note != "" {
					mutedText(c, t, note).MaxLines(1)
				}
			}
		})
		horizontalHairline(c, t)
		entrance(ui.Column(c.Key("routes")), 4).Gap(gapSmall).
			Disabled(d.controlsLocked()).Children(func() {
			d.quickRoutes(c, t, openWindow)
		})
	})
}

// flyoutCard holds the exit card, which fades while the core is up but nothing
// sends traffic to it: the panel then says plainly that Hop is doing nothing.
func (d *Dashboard) flyoutCard(c *ui.Context, t *ui.Theme, card func()) {
	wrap := ui.Column(c.Key("routeDim"))
	target := float32(1)
	if d.status == statusConnected && !d.proxying() {
		target = flyoutIdleOpacity
	}
	wrap.Opacity(wrap.AnimateTo("opacity", target, ui.Smooth)).Children(card)
}

// flyoutModes renders the three outbound modes with the exact same filled blue
// spring-sliding thumb as the main window's floating dock.
func (d *Dashboard) flyoutModes(c *ui.Context, t *ui.Theme) {
	const inset float32 = 3
	width, _ := c.Size()
	border := float32(0)
	if !c.Vibrancy() {
		border = 1 // the panel draws its own frame
	}
	labels := modeLabels()
	hints := modeHints()
	segW := (width - 2*(flyoutPadding+border) - 2*inset) / float32(len(labels))
	inner := flyoutModeHeight - 2*inset
	locked := d.modeLocked()
	track := ui.Box(c.Key("flyout-mode")).Label(locales.OutboundMode()).
		Width(segW*float32(len(labels)) + 2*inset).Height(flyoutModeHeight).
		Radius(flyoutModeHeight / 2).Background(horizonPillIdle(t)).Disabled(locked)
	if locked {
		track.Opacity(flyoutLockedOpacity)
	}
	x := track.AnimateTo("thumb", float32(d.modeIndex)*segW, ui.Snappy)
	track.Children(func() {
		ui.Box(c.Key("flyoutModeThumb")).Absolute().Left(inset+x).Top(inset).Width(segW).Height(inner).
			Radius(inner/2).Background(t.Accent).Shadow(0, 1, 4, 0, t.Accent.Alpha(0.28))
		ui.Row(c.Key("flyoutModeLabels")).Absolute().Left(inset).Top(inset).Children(func() {
			for i, label := range labels {
				idx := i
				chosen := d.modeIndex == idx
				seg := ui.ButtonBase(c.Key("flyoutMode-" + label)).Label(label).Tooltip(hints[idx]).
					Width(segW).Height(inner).Radius(inner / 2).Center().
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
					ui.Text(c, label).FontSize(fontSecondary).FontWeight(weight).TextColor(color).NoWrap().Translate(0, -opticalLift)
				})
			}
		})
	})
}

// flyoutRoute is the exit: the gauge with its flag, the exit's name (without
// repeating the flag already centered inside the gauge), the groups it passes,
// then the live speeds with the latency pill at the right end.
func (d *Dashboard) flyoutRoute(c *ui.Context, t *ui.Theme) {
	route := d.view.Route
	if len(route) == 0 {
		mutedText(c, t, locales.FlyoutNoSubscription())
		return
	}
	hop := route[len(route)-1]
	ui.Row(c.Key("exitRow")).Gap(gapMedium).AlignItems(ui.Center).Children(func() {
		routeGauge(c, t, hop, flyoutGaugeSize, d.last)
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(gapLine).Children(func() {
			ui.Text(c, horizonExitTitle(hop)).FontSize(fontHeadline).FontWeight(weightSemibold).LetterSpacing(-0.3).MaxLines(1)
			if via := heroVia(route, hop); via != "" {
				mutedText(c, t, via).MaxLines(1)
			}
		})
	})
	ui.Row(c.Key("speeds")).Gap(gapMedium).AlignItems(ui.Center).Children(func() {
		compactSpeed(c, t, "↓", formatRate(d.last.Down), accentInk(t))
		compactSpeed(c, t, "↑", formatRate(d.last.Up), uploadColor(t))
		ui.Box(c).Grow(1)
		heroDelay(c, t, hop.Node.Delay)
	})
}

// compactSpeed is one speed: the arrow in its line's colour, the number
// rolling as it changes, and its unit.
func compactSpeed(c *ui.Context, t *ui.Theme, arrow string, r readout, color ui.Color) {
	ui.Row(c.Key(arrow)).Width(compactSpeedWidth).Gap(4).AlignItems(ui.End).Children(func() {
		ui.Text(c, arrow).FontSize(fontBody).FontWeight(weightBold).TextColor(color)
		ui.Text(c, r.value).FontSize(17).FontWeight(weightSemibold).LetterSpacing(-0.3).FontFeatures("tnum").
			ContentTransition(ui.ContentTransition{Numeric: true})
		ui.Text(c, r.unit).FontSize(fontCaption).TextColor(t.TextMuted).Margin(0, 0, 2, 0)
	})
}

// flyoutSubChars is how much of a subscription's name the route row shows; the
// menu shows the whole of each name.
const flyoutSubChars = 8

// flyoutSubName shortens a subscription name for the route row: at most
// flyoutSubChars characters, an ellipsis when it was cut.
func flyoutSubName(name string) string {
	runes := []rune(name)
	if len(runes) <= flyoutSubChars {
		return name
	}
	return string(runes[:flyoutSubChars]) + "…"
}

// flyoutSubscription is the subscription in use, at the left of the panel's
// route row: it shows the profile's name shortened by flyoutSubName and opens a
// menu of the saved ones, the one in use checked, each item naming the whole
// subscription. The menu's last item opens the management page in the window.
// With no subscription in use the row draws nothing.
func (d *Dashboard) flyoutSubscription(c *ui.Context, t *ui.Theme, openWindow func()) {
	s := &d.subscription
	active, ok := s.active()
	if !ok {
		return
	}
	locked := d.controlsLocked()
	row := ui.ButtonBase(c.Key("flyoutSubscription")).Label(locales.SubSelect()).
		Value(pickerLabel(s.view.Items, active.ID)).
		Height(switchHeight).Padding(0, 10).Radius(switchHeight / 2).Gap(gapTight).
		Justify(ui.Start).Disabled(locked)
	fill := horizonPillIdle(t)
	if !locked && row.Hovered() {
		fill = horizonPillHover(t)
	}
	row.Background(row.AnimateColor("fill", fill, ui.Smooth))
	if !locked {
		row.Cursor(ui.CursorPointer)
	}
	row.Menu(func(m *ui.Menu) {
		for _, item := range s.view.Items {
			if m.Item(pickerLabel(s.view.Items, item.ID)).Checked(item.ID == s.view.ActiveID).Chosen() {
				d.useSubscription(item.ID)
			}
		}
		m.Separator()
		if m.Item(locales.SubManage()).Chosen() {
			d.page = pageNodes
			s.managing = true
			openWindow()
		}
	})
	meta := d.subscriptionShortMeta(active)
	row.Children(func() {
		ui.Text(c, flyoutSubName(active.Name)).FontSize(fontSecondary).
			FontWeight(weightSemibold).MaxLines(1).TextColor(t.Text).Translate(0, -opticalLift)
		if meta != "" {
			captionText(c, t, meta).FontFeatures("tnum").NoWrap().Translate(0, -opticalLift)
		}
		ui.Icon(c, iconChevron).Width(iconInline).Height(iconInline).TextColor(t.TextMuted)
	})
}

// quickGroup is the group the panel switches nodes in: the main group first,
// then the first manual selector, then any selectable group.
func (d *Dashboard) quickGroup() (model.GroupView, bool) {
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
	return model.GroupView{}, false
}

// hasSubscription says a subscription is in use: the panel's routes, their
// header and their test all follow it.
func (d *Dashboard) hasSubscription() bool {
	_, ok := d.subscription.active()
	return ok
}

// testQuickRoutes measures the panel's routes as it opens, so their delays
// are fresh without a click; a test that ran in the last quickTestEvery is
// recent enough.
func (d *Dashboard) testQuickRoutes() {
	group, ok := d.quickGroup()
	if !ok || !d.hasSubscription() || d.testDelay == nil || d.testing != "" || time.Since(d.quickTestedAt) < quickTestEvery {
		return
	}
	d.quickTestedAt = time.Now()
	d.testDelay(group.Name)
}

// quickRoutes heads the panel's routes with the subscription in use and lists
// the quick group's routes with their delays; a click switches to one. The row
// names no group, as the window's toolbar does. With no subscription in use the
// section offers adding one, which opens the window's add dialog, instead of the
// built-in profile's only choices, DIRECT and REJECT.
func (d *Dashboard) quickRoutes(c *ui.Context, t *ui.Theme, openWindow func()) {
	if !d.hasSubscription() {
		button(c, buttonSpec{label: locales.SubAdd(), icon: iconPlus, style: softButton(t),
			disabled: d.controlsLocked()}, func() {
			d.page = pageNodes
			if !d.subscription.editorOpen {
				d.openEditor(editorAdd, "")
			}
			openWindow()
		})
		return
	}
	group, ok := d.quickGroup()
	ui.Row(c.Key("routeHead")).Gap(gapSmall).AlignItems(ui.Center).Height(26).Children(func() {
		d.flyoutSubscription(c, t, openWindow)
		ui.Box(c).Grow(1)
		if ok {
			d.testButton(c, t, group.Name)
		}
	})
	if !ok {
		return
	}
	routes := service.QuickRoutes(group, fastestCount)
	if len(routes) == 0 {
		mutedText(c, t, locales.FlyoutNoDelays())
		return
	}
	ui.Scroll(c).MaxHeight(quickRowsMax*(quickRowH+quickRowGap) - quickRowGap).Children(func() {
		ui.Column(c).Gap(quickRowGap).Children(func() {
			for _, route := range routes {
				d.quickRow(c, t, group, route)
			}
		})
	})
}

// quickRow is one route: a group with the node it picks now, or a node.
func (d *Dashboard) quickRow(c *ui.Context, t *ui.Theme, group model.GroupView, node model.NodeView) {
	isActive := node.Name == group.Selected
	groupName, nodeName := group.Name, node.Name
	row := ui.Row(c.Key(node.Name)).Height(quickRowH).Gap(gapSmall).AlignItems(ui.Center).
		Padding(0, 10).Radius(homeShelfRowRadius).Cursor(ui.CursorPointer)
	fill := ui.Transparent
	switch {
	case isActive:
		fill = t.Accent.Alpha(0.11)
	case row.Hovered():
		fill = horizonPillIdle(t)
	}
	row.Background(row.AnimateColor("fill", fill, ui.Smooth))
	row.OnClick(func() {
		if !isActive {
			d.selectProxy(groupName, nodeName)
		}
	})
	row.Children(func() {
		name := strings.TrimSpace(shownFlag(node.Flag) + " " + node.Region + " " + node.Title)
		if node.Via != "" {
			name = shownName(node.Name)
		}
		titleInk, weight := t.Text, weightMedium
		if isActive {
			titleInk, weight = accentInk(t), weightSemibold
		}
		ui.Text(c, name).FontSize(fontBody).FontWeight(weight).TextColor(titleInk).MaxLines(1).Shrink(0)
		ui.Row(c).Grow(1).Justify(ui.End).MinWidth(0).Children(func() {
			if node.Via != "" {
				captionText(c, t, shownName(node.Via)).MaxLines(1)
			}
		})
		ui.Row(c).Width(52).Justify(ui.End).Children(func() {
			delayLabel(c, t, node.Delay, fontSecondary)
		})
		ui.Box(c.Key("mark")).Width(iconInline).Height(iconInline).Center().Children(func() {
			if isActive {
				ui.Icon(c, iconCheck).Width(iconInline).Height(iconInline).TextColor(accentInk(t))
			}
		})
	})
}
