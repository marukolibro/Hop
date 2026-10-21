package handler

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
)

const (
	// routeFlowWidth keeps the stops close enough to read as one line.
	routeFlowWidth = 460
	// exitWidth fits "🇸🇬 Singapore 01" and its latency on one line.
	exitWidth = 210
	// sparkMinWidth is the window width from which the sparkline fits
	// beside the route.
	sparkMinWidth = 900
	// modeSegmentWidth fits a two-character mode name.
	modeSegmentWidth = 56
)

// modeHints say what each of modeLabels does, index for index.
var modeHints = []string{
	"按规则决定哪些走代理、哪些直连",
	"全部流量走代理",
	"全部直连,不经过代理",
}

// routeBar leads the nodes page: the node traffic leaves by, the path it
// takes there drawn as a short transit line through the groups, and the
// live speed, on one row; under it, the outbound mode. How traffic gets in,
// the system proxy and TUN, is the header's.
func (d *Dashboard) routeBar(c *ui.Context, t *ui.Theme) {
	route := d.view.Route
	ui.Column(c).Margin(0, pagePadding, gapLarge, pagePadding).
		Radius(radiusPanel).Background(t.Surface).Border(1, t.Border).Children(func() {
		ui.Row(c).Key("route").Gap(gapSection).AlignItems(ui.Center).Padding(panelPadY, panelPadX).Children(func() {
			if len(route) == 0 {
				mutedText(c, t, "导入订阅后,这里会显示流量经过的线路").Grow(1)
				d.speeds(c, t)
				return
			}
			ui.Column(c).Key("exit").Gap(gapTight).Width(exitWidth).Children(func() {
				exitNode(c, t, route[len(route)-1])
			})
			ui.Box(c).Key("flow").Grow(1).Basis(0).MinWidth(0).Children(func() {
				ui.Box(c).MaxWidth(routeFlowWidth).Children(func() {
					routeFlow(c, t, route, d.last)
				})
			})
			d.speeds(c, t)
		})
		ui.Box(c).Height(1).Background(t.Border.Alpha(cardDividerAlpha))
		d.trafficControls(c, t)
	})
}

// trafficControls are the route card's foot: the outbound mode, and what
// it does in a line. The switch waits for the core to report its mode and
// for any subscription mutation to finish, so an apply cannot carry the mode
// the user just left back in.
func (d *Dashboard) trafficControls(c *ui.Context, t *ui.Theme) {
	disabled := d.view.Mode == "" || d.subscription.mutations > 0 || d.core.busy
	ui.Row(c).Key("controls").Gap(gapMedium).AlignItems(ui.Center).Padding(gapSmall+2, panelPadX).Children(func() {
		mode := ui.Box(c).Key("mode").Disabled(disabled)
		if disabled {
			mode.Opacity(disabledOpacity)
		}
		mode.Children(func() {
			slidingSwitch(c, "出站模式", &d.modeIndex, modeLabels, pageSwitchStyle(t, modeSegmentWidth), func(i int) {
				d.setMode(service.Modes[i])
			})
		})
		mutedText(c, t, modeHints[d.modeIndex]).MaxLines(1)
	})
}

// exitNode names the node traffic leaves by, with its line, multiplier and
// latency.
func exitNode(c *ui.Context, t *ui.Theme, hop model.RouteHop) {
	if hop.Group {
		ui.Text(c, hop.Name).FontSize(fontHeadline).FontWeight(weightSemibold)
		return
	}
	node := hop.Node
	switch {
	case node.PlanNote:
		headline(c, t, "机场信息节点", node.Delay)
		mutedText(c, t, node.Name+" · 测速后,自动选择会换到延迟最低的节点").MaxLines(1)
		return
	case node.Via != "":
		headline(c, t, "直连", node.Delay)
		mutedText(c, t, node.Via)
		return
	}
	headline(c, t, strings.TrimSpace(shownFlag(node.Flag)+" "+node.Region+" "+node.Title), node.Delay)
	labels := nodeLabels(node)
	if len(labels) > 0 || node.Multiplier != "" {
		ui.Row(c).Gap(gapTight).Children(func() {
			for _, label := range labels {
				chip(c, label, t.TextMuted)
			}
			if node.Multiplier != "" {
				chip(c, node.Multiplier, t.Warning)
			}
		})
	}
}

func headline(c *ui.Context, t *ui.Theme, text string, delay model.Delay) {
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, text).FontSize(fontHeadline).FontWeight(weightSemibold).LetterSpacing(-0.2).MaxLines(1).ContentTransition(ui.ContentTransition{})
		delayLabel(c, t, delay, fontBody)
	})
}
