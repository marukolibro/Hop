package handler

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
)

const (
	heroGaugeSize float32 = 56
	// heroModeGap separates the exit/gauge/plot block from the mode row, so the
	// switch clears the exit's chips; the row's own bottom padding keeps the
	// space to the hairline below it.
	heroModeGap      float32 = 16
	heroExitWidth    float32 = 235
	heroExitWidthMid float32 = 210
	heroSpeedsWidth  float32 = 145
	heroSpeedsNarrow float32 = 110
)

// routeBar is the design's hero: the ring gauge with the exit's flag, the
// exit with its route, public address and facts, the sparkline that fills
// the room left, the two speeds behind a hairline, and the outbound mode
// below. The gauge's arcs ease to the traffic and then rest.
func (d *Dashboard) routeBar(c *ui.Context, t *ui.Theme) {
	route := d.view.Route
	width, _ := c.Size()
	hero := ui.Column(c).Key("hero").Gap(heroModeGap).Padding(pagePadding, pagePadding, 0, pagePadding).
		Background(heroFill(t))
	hero.Children(func() {
		ui.Row(c).Key("routeTop").Gap(pagePadding).AlignItems(ui.Center).Children(func() {
			if len(route) > 0 {
				routeGauge(c, t, route[len(route)-1], heroGaugeSize, d.last)
			}
			exitWidth := heroExitWidthMid
			if width >= 1050 {
				exitWidth = heroExitWidth
			}
			d.heroExit(c, t, route, exitWidth)
			if width >= sparkMinWidth {
				d.sparkline(c, t)
			} else {
				ui.Box(c).Grow(1)
			}
			speedsWidth := heroSpeedsNarrow
			if width >= 1050 {
				speedsWidth = heroSpeedsWidth
			}
			d.heroSpeeds(c, t, speedsWidth)
		})
		d.routeFooter(c, t)
	})
	horizontalHairline(c, t)
}

// heroExit names the exit, the groups it passes, its public address and its
// facts.
func (d *Dashboard) heroExit(c *ui.Context, t *ui.Theme, route []model.RouteHop, width float32) {
	exit := ui.Column(c).Key("exit").Gap(gapTight).Width(width)
	exit.Children(func() {
		if len(route) == 0 {
			mutedText(c, t, "导入订阅后,这里会显示流量经过的线路")
			return
		}
		hop := route[len(route)-1]
		node := hop.Node
		ui.Row(c).Key("routeTitle").Gap(10).AlignItems(ui.Baseline).Children(func() {
			if flag := shownFlag(node.Flag); flag != "" && !hop.Group && node.Via == "" {
				ui.Text(c, flag).FontSize(21)
			}
			// The row stays as wide as its content, so the delay follows the
			// name; a long name shrinks and truncates instead of pushing it
			// to the column's edge.
			ui.Text(c, heroExitName(hop)).FontSize(22).FontWeight(weightSemibold).LetterSpacing(-0.5).
				MaxLines(1).Shrink(1)
			delayLabel(c, t, node.Delay, fontSecondary)
		})
		if via := heroVia(route, hop); via != "" {
			mutedText(c, t, via).MaxLines(1)
		}
		d.exitIPLine(c, t)
		heroChips(c, t, hop)
	})
}

// heroExitName is the exit's name without the flag, which the title shows
// separately.
func heroExitName(hop model.RouteHop) string {
	node := hop.Node
	switch {
	case hop.Group:
		return hop.Name
	case node.PlanNote:
		return "机场信息节点"
	case node.Via != "":
		return "直连"
	default:
		name := strings.TrimSpace(node.Region + " " + node.Title)
		if name == "" {
			return node.Name
		}
		return name
	}
}

// heroVia is the line under the exit's name: the groups it passes, or that
// it goes out directly; "" when the route says neither.
func heroVia(route []model.RouteHop, hop model.RouteHop) string {
	switch {
	case hop.Group:
		return routeVia(route)
	case hop.Node.PlanNote, hop.Node.Via != "":
		return "直连,不经过代理"
	default:
		return routeVia(route)
	}
}

// heroChips are the exit's facts as small labels: its protocol, UDP, the
// multiplier that costs something, then what its name gives.
func heroChips(c *ui.Context, t *ui.Theme, hop model.RouteHop) {
	node := hop.Node
	if hop.Group || node.PlanNote || node.Via != "" {
		return
	}
	labels := nodeLabels(node)
	if len(labels) == 0 && node.Multiplier == "" {
		return
	}
	ui.Row(c).Key("routeFacts").Gap(gapTight).Wrap().Children(func() {
		for _, label := range labels {
			nodeChip(c, t, label)
		}
		if node.Multiplier != "" {
			nodeChip(c, t, node.Multiplier)
		}
	})
}

// heroSpeeds is the download and upload column, separated from the plot by
// a hairline. The download arrow takes the accent, upload the muted ink.
func (d *Dashboard) heroSpeeds(c *ui.Context, t *ui.Theme, width float32) {
	ui.Row(c).Key("speeds").AlignItems(ui.Center).Children(func() {
		ui.Box(c).Width(hairlineWidth).Height(44).Background(t.Border)
		ui.Column(c).Width(width-1).Padding(0, 0, 0, 22).Gap(6).Children(func() {
			heroSpeedLine(c, t, "↓", formatRate(d.last.Down), accentInk(t))
			heroSpeedLine(c, t, "↑", formatRate(d.last.Up), uploadColor(t))
		})
	})
}

func heroSpeedLine(c *ui.Context, t *ui.Theme, arrow string, r readout, color ui.Color) {
	ui.Row(c).Key(arrow).Gap(6).AlignItems(ui.End).Children(func() {
		ui.Text(c, arrow).FontSize(fontSecondary).FontWeight(weightMedium).TextColor(color)
		ui.Text(c, r.value).FontSize(18).FontWeight(weightMedium).LetterSpacing(-0.4).
			FontFeatures("tnum").ContentTransition(ui.ContentTransition{Numeric: true})
		captionText(c, t, r.unit)
	})
}

// routeFooter is the mode switch with what it does, and the core's state.
func (d *Dashboard) routeFooter(c *ui.Context, t *ui.Theme) {
	disabled := d.view.Mode == "" || d.subscription.mutations > 0 || d.core.busy
	ui.Row(c).Key("controls").Gap(gapMedium).AlignItems(ui.Center).Padding(0, 0, 19, 0).Children(func() {
		mode := ui.Box(c).Key("mode").Disabled(disabled).Tooltip(modeHints[d.modeIndex])
		if disabled {
			mode.Opacity(disabledOpacity)
		}
		mode.Children(func() {
			slidingSwitch(c, "出站模式", &d.modeIndex, modeLabels, pageSwitchStyle(t, modeSegmentWidth), func(i int) {
				d.setMode(service.Modes[i])
			})
		})
		ui.Box(c).Grow(1)
		dotColor, title := t.Success, "内核运行中"
		switch d.status {
		case statusFailed:
			dotColor, title = t.Danger, "内核未连接"
		case statusConnected:
		default:
			dotColor, title = t.TextMuted, "内核连接中"
		}
		ui.Row(c).Key("coreStatus").Gap(gapTight).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Width(5).Height(5).Radius(2.5).Background(dotColor)
			captionText(c, t, title).NoWrap()
		})
	})
}
