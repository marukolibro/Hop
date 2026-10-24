package handler

import (
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
)

const (
	flyoutWidth = 340
	// flyoutHeight is the first panel's height, before it fits its content.
	flyoutHeight  = 640
	flyoutPadding = gapLarge
	fastestCount  = 5
	quickRowH     = 32
	quickRowGap   = 2
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
	flyoutGaugeSize float32 = 44
)

// flyoutView is the menu bar panel: where traffic goes and how fast, the
// mode, and the routes to switch to, without opening the window.
// Its sections rise into place one after another as it opens. It is as tall
// as its content: fit hears the height the last frame laid out, and the
// route list scrolls past quickRowsMax routes.
func (d *Dashboard) flyoutView(c *ui.Context, openWindow func(), quit func(), fit func(height int)) {
	c.SetTheme(d.theme(c))
	t := c.Theme()
	c.Root().Background(ui.Transparent)
	panel := ui.Column(c).Fill().Padding(flyoutPadding)
	var border float32
	if !c.Vibrancy() {
		border = 1
		panel.Background(t.Background).Radius(radiusPanel).Border(border, t.Border)
	}
	panel.Children(func() {
		content := ui.Column(c).Key("content").Gap(14).Shrink(0)
		if h := content.LayoutBounds().H; h > 0 {
			fit(int(math.Ceil(float64(h + 2*(flyoutPadding+border)))))
		}
		d.flyoutContent(c, t, content, openWindow, quit)
	})
}

// flyoutContent is the panel's sections, top to bottom, inside content.
func (d *Dashboard) flyoutContent(c *ui.Context, t *ui.Theme, content ui.Element, openWindow func(), quit func()) {
	content.Children(func() {
		entrance(ui.Row(c).Key("top").Gap(gapSmall).AlignItems(ui.Center), 0).Children(func() {
			brandLockup(c, t, 20, d.markRun)
			ui.Box(c).Grow(1)
			if d.appUpdate.update.Available && d.appUpdate.enabled {
				// Labelled: a bare arrow here would read as the upload rate.
				button(c, buttonSpec{label: "新版本", icon: iconUpgrade, style: softButton(t)}, func() {
					d.page = pageSettings
					openWindow()
				})
			}
			statusDot(c, d.statusColor(t), d.proxying())
			mutedText(c, t, d.statusText())
		})
		entrance(ui.Column(c).Key("route"), 1).Gap(10).Padding(12, 14).Radius(radiusPanel).Background(t.Surface).Border(1, t.Border).Children(func() {
			d.flyoutRoute(c, t)
		})
		entrance(ui.Column(c).Key("speed"), 2).Children(func() {
			d.flyoutSpeeds(c, t)
		})
		entrance(ui.Column(c).Key("mode"), 3).Gap(gapTight).Children(func() {
			// The panel is too narrow for all five pills on one line, so the
			// three modes, one of which is always on, fill the first, and the
			// two traffic switches split the second.
			ui.Row(c).Key("pills").Gap(gapTight).AlignItems(ui.Center).Children(func() {
				for i := range modeLabels {
					d.modePill(c, t, i).Grow(1).Basis(0)
				}
			})
			ui.Row(c).Key("switches").Gap(gapTight).AlignItems(ui.Center).Children(func() {
				d.systemProxyPill(c, t).Grow(1).Basis(0)
				d.tunPill(c, t).Grow(1).Basis(0)
			})
			for _, note := range []string{d.systemProxyHint(), d.tunHint()} {
				if note != "" {
					mutedText(c, t, note).MaxLines(1)
				}
			}
		})
		entrance(ui.Column(c).Key("routes"), 4).Gap(gapSmall).
			Disabled(d.controlsLocked()).Children(func() {
			d.quickRoutes(c, t)
		})
		entrance(ui.Row(c).Key("footer"), 5).Gap(gapSmall).Children(func() {
			button(c, buttonSpec{label: "打开主窗口", icon: iconWindow, style: softButton(t)}, openWindow).Grow(1)
			button(c, buttonSpec{label: "退出 Hop", icon: iconPower, style: softButton(t)}, quit)
		})
	})
}

func (d *Dashboard) flyoutRoute(c *ui.Context, t *ui.Theme) {
	route := d.view.Route
	if len(route) == 0 {
		mutedText(c, t, "还没有使用中的订阅。打开主窗口添加一个。")
		return
	}
	ui.Row(c).Key("exitRow").Gap(gapMedium).AlignItems(ui.Center).Children(func() {
		routeGauge(c, t, route[len(route)-1], flyoutGaugeSize, d.last)
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(gapTight).Children(func() {
			exitNode(c, t, route)
		})
	})
}

// flyoutSpeeds shows both speeds on one line, rolling as they change,
// beside the last two minutes of traffic.
func (d *Dashboard) flyoutSpeeds(c *ui.Context, t *ui.Theme) {
	history := d.history
	down, up, base := t.Accent, uploadColor(t), t.Border
	ui.Row(c).Gap(gapLarge).AlignItems(ui.Center).Children(func() {
		compactSpeed(c, t, "↓", formatRate(d.last.Down), down)
		compactSpeed(c, t, "↑", formatRate(d.last.Up), up)
		ui.Box(c).Grow(1).Height(28).Label("Traffic").Description("Download and upload speed over the last two minutes").Draw(func(p *ui.Painter, r ui.Rect) {
			p.Line(r.X, r.Y+r.H-0.5, r.X+r.W, r.Y+r.H-0.5, 1, base)
			sparkArea(p, r, history, down, up)
		})
	})
}

// compactSpeed is one speed: the arrow in its line's colour, the number
// rolling as it changes, and its unit.
func compactSpeed(c *ui.Context, t *ui.Theme, arrow string, r readout, color ui.Color) {
	ui.Row(c).Key(arrow).Width(compactSpeedWidth).Gap(4).AlignItems(ui.End).Children(func() {
		ui.Text(c, arrow).FontSize(fontBody).FontWeight(weightBold).TextColor(color)
		ui.Text(c, r.value).FontSize(17).FontWeight(weightSemibold).LetterSpacing(-0.3).FontFeatures("tnum").
			ContentTransition(ui.ContentTransition{Numeric: true})
		ui.Text(c, r.unit).FontSize(fontCaption).TextColor(t.TextMuted).Margin(0, 0, 2, 0)
	})
}

// quickGroup is the group the panel switches nodes in: the first that
// takes a choice.
func (d *Dashboard) quickGroup() (model.GroupView, bool) {
	for _, g := range d.view.Groups {
		if g.Selectable {
			return g, true
		}
	}
	return model.GroupView{}, false
}

// testQuickRoutes measures the panel's routes as it opens, so their delays
// are fresh without a click; a test that ran in the last quickTestEvery is
// recent enough.
func (d *Dashboard) testQuickRoutes() {
	group, ok := d.quickGroup()
	if !ok || d.testDelay == nil || d.testing != "" || time.Since(d.quickTestedAt) < quickTestEvery {
		return
	}
	d.quickTestedAt = time.Now()
	d.testDelay(group.Name)
}

// quickRoutes lists the main group's routes with their delays; a click
// switches to one.
func (d *Dashboard) quickRoutes(c *ui.Context, t *ui.Theme) {
	group, ok := d.quickGroup()
	if !ok {
		return
	}
	routes := service.QuickRoutes(group, fastestCount)
	ui.Row(c).Gap(gapSmall).AlignItems(ui.Center).Height(26).Children(func() {
		sectionTitle(c, "线路")
		mutedText(c, t, shownName(group.Label))
		ui.Box(c).Grow(1)
		d.testButton(c, t, group.Name)
	})
	if len(routes) == 0 {
		mutedText(c, t, "还没有测速结果。点「测速」看看哪个节点最快。")
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
	row := ui.Row(c).Key(node.Name).Height(quickRowH).Gap(10).AlignItems(ui.Center).Padding(0, 10).Radius(radiusControl).Cursor(ui.CursorPointer)
	fill := ui.Transparent
	switch {
	case isActive:
		fill = t.Accent.Alpha(0.12)
	case row.Hovered():
		fill = t.SurfaceHover
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
		label := ui.Text(c, name).FontSize(fontBody).MaxLines(1).Shrink(0)
		if isActive {
			label.FontWeight(weightSemibold)
		}
		ui.Row(c).Grow(1).Justify(ui.End).MinWidth(0).Children(func() {
			if node.Via != "" {
				mutedText(c, t, shownName(node.Via)).MaxLines(1)
			}
		})
		ui.Row(c).Width(56).Justify(ui.End).Children(func() {
			delayLabel(c, t, node.Delay, fontSecondary)
		})
	})
}
