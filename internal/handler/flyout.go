package handler

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
)

const (
	flyoutWidth   = 340
	flyoutHeight  = 576
	flyoutPadding = gapLarge
	fastestCount  = 5
	quickRowH     = 32
	barWidth      = 64
	// barCeiling is the delay at which a latency bar is at its shortest.
	barCeiling = 600
	// compactSpeedWidth holds "999 KB/s" so the line does not shift as
	// numbers change.
	compactSpeedWidth = 88
)

// flyoutView is the menu bar panel: where traffic goes and how fast, the
// mode, and the fastest nodes to switch to, without opening the window.
// Its sections rise into place one after another as it opens.
func (d *Dashboard) flyoutView(c *ui.Context, openWindow func(), quit func()) {
	c.SetTheme(d.theme(c))
	t := c.Theme()
	c.Root().Background(ui.Transparent)
	panel := ui.Column(c).Fill().Padding(flyoutPadding).Gap(14)
	if !c.Vibrancy() {
		panel.Background(t.Background).Radius(radiusPanel).Border(1, t.Border)
	}
	panel.Children(func() {
		entrance(ui.Row(c).Key("top").Gap(gapSmall).AlignItems(ui.Center), 0).Children(func() {
			brandLockup(c, t, 20)
			ui.Box(c).Grow(1)
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
			ui.Row(c).Key("pills").Gap(gapTight).AlignItems(ui.Center).Children(func() {
				for i := range modeLabels {
					d.modePill(c, t, i)
				}
				ui.Box(c).Grow(1)
				d.systemProxyPill(c, t)
			})
			// The TUN pill sits on its own line: the panel is narrow, and
			// the two traffic switches read together on the right.
			ui.Row(c).Key("tun").Gap(gapTight).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Grow(1)
				if note := d.tunHint(); note != "" {
					mutedText(c, t, note).MaxLines(1)
				}
				d.tunPill(c, t)
			})
			if note := d.systemProxyHint(); note != "" {
				mutedText(c, t, note).MaxLines(1).AlignSelf(ui.End)
			}
		})
		entrance(ui.Column(c).Key("fastest"), 4).Gap(2).Grow(1).Clip().
			Disabled(d.controlsLocked()).Children(func() {
			d.fastestNodes(c, t)
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
	routeFlow(c, t, route, d.last)
	exitNode(c, t, route[len(route)-1])
}

// flyoutSpeeds shows both speeds on one line, rolling as they change,
// beside the last two minutes of traffic.
func (d *Dashboard) flyoutSpeeds(c *ui.Context, t *ui.Theme) {
	history := d.history
	down, up, base := t.Accent, t.Warning, t.Border
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

// fastestNodes lists the quickest nodes of the main group with a bar for
// each latency; a click switches to one, and testing again regrows the
// bars.
func (d *Dashboard) fastestNodes(c *ui.Context, t *ui.Theme) {
	group, ok := d.quickGroup()
	if !ok {
		return
	}
	nodes := service.FastestNodes(group, fastestCount)
	ui.Row(c).Gap(gapSmall).AlignItems(ui.Center).Height(26).Children(func() {
		sectionTitle(c, "最快节点")
		mutedText(c, t, group.Label)
		ui.Box(c).Grow(1)
		d.testButton(c, t, group.Name)
	})
	if len(nodes) == 0 {
		mutedText(c, t, "还没有测速结果。点「测速」看看哪个节点最快。")
		return
	}
	for i, node := range nodes {
		d.quickRow(c, t, group, node, i)
	}
}

func (d *Dashboard) quickRow(c *ui.Context, t *ui.Theme, group model.GroupView, node model.NodeView, rank int) {
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
		label := ui.Text(c, strings.TrimSpace(shownFlag(node.Flag)+" "+node.Region+" "+node.Title)).FontSize(fontBody).MaxLines(1).Grow(1)
		if isActive {
			label.FontWeight(weightSemibold)
		}
		latencyBar(c, t, node.Delay, rank)
		ui.Row(c).Width(56).Justify(ui.End).Children(func() {
			delayLabel(c, t, node.Delay, fontSecondary)
		})
	})
}

// latencyBar grows to show how fast a node is: full for an instant answer,
// short near barCeiling. Bars grow one after another when results arrive.
func latencyBar(c *ui.Context, t *ui.Theme, delay model.Delay, rank int) {
	score := float32(1) - float32(min(delay.Millis, barCeiling))/barCeiling
	target := max(0.08, score) * barWidth
	track := ui.Box(c).Width(barWidth).Height(4).Radius(2).Background(t.Border)
	width := track.AnimateTo("bar", target, ui.Animation{Spring: ui.Snappy.Spring, Delay: time.Duration(rank) * entranceStep})
	track.Children(func() {
		ui.Box(c).Width(width).Height(4).Radius(2).Background(delayColor(t, delay))
	})
}
