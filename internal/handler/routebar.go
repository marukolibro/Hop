package handler

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
)

// routeFlowWidth keeps the stops close enough to read as one line.
const routeFlowWidth = 520

// routeBar leads the nodes page: the path traffic takes right now, drawn as
// a short transit line through the groups to the node it leaves by, with
// the live speed beside it.
func (d *Dashboard) routeBar(c *ui.Context, t *ui.Theme) {
	route := d.view.Route
	ui.Row(c).Gap(gapSection).AlignItems(ui.Center).Padding(14, 18).Margin(0, pagePadding, gapLarge, pagePadding).
		Radius(radiusPanel).Background(t.Surface).Border(1, t.Border).Children(func() {
		ui.Column(c).Grow(1).Gap(gapSmall).Children(func() {
			if len(route) == 0 {
				mutedText(c, t, "导入订阅后,这里会显示流量经过的线路")
				return
			}
			ui.Box(c).MaxWidth(routeFlowWidth).Children(func() {
				routeFlow(c, t, route, d.last)
			})
			exitNode(c, t, route[len(route)-1])
		})
		d.speeds(c, t)
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
	headline(c, t, strings.TrimSpace(node.Flag+" "+node.Region+" "+node.Title), node.Delay)
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
