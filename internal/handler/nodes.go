package handler

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
)

const (
	// minTileWidth is the narrowest a tile may be; the grid fits as many
	// whole tiles as the pane holds, so the options stay compact.
	minTileWidth  float32 = 168
	tileGap               = gapSmall
	tileTitleSize float32 = fontEmphasis
	radiusTile    float32 = 10
	fastDelay             = 150
	moderateDelay         = 350
	// nodeOrderSegmentWidth fits a two-character order name.
	nodeOrderSegmentWidth float32 = 52
	// Every tile has a hollow choice ring; the selected one fills it.
	nodeCheckSize float32 = 14
	nodeCheckIcon float32 = 10
	// tileChipFill washes a tile label in its own colour.
	tileChipFill float32 = 0.10
	// tileInkShade moves a tier or accent ink toward the text ink, so a
	// small label keeps 4.5:1 on the tile over any wallpaper.
	tileInkShade float32 = 0.45
)

// groupModes says in plain words how each group type picks its node.
var groupModes = map[string]string{
	model.GroupSelector:    "手动选择",
	model.GroupURLTest:     "自动选择延迟最低的节点",
	model.GroupFallback:    "当前节点不可用时自动切换",
	model.GroupLoadBalance: "在节点间分配连接",
	model.GroupRelay:       "依次经过所有节点",
}

func (d *Dashboard) activeGroupView() (model.GroupView, bool) {
	for _, g := range d.view.Groups {
		if g.Name == d.activeGroup {
			return g, true
		}
	}
	return model.GroupView{}, false
}

// nodesPage shows the route on top, then the groups in a sidebar beside the
// options of the one chosen. The subscription management page is a subpage of
// it, reached from the subscription bar.
func (d *Dashboard) nodesPage(c *ui.Context, t *ui.Theme) {
	if d.subscription.managing {
		d.managePage(c, t)
		return
	}
	d.routeBar(c, t)
	d.subscriptionBar(c, t)
	// The body is one surface: a fixed sidebar, a hairline, and the options
	// that scroll beside it. Each pane may shrink below its content, so it
	// scrolls inside the window rather than pushing past it.
	ui.Row(c).Grow(1).Basis(0).MinHeight(0).AlignItems(ui.Stretch).
		Disabled(d.controlsLocked()).Children(func() {
		d.groupSidebar(c, t)
		verticalHairline(c, t)
		pane := ui.Column(c).Grow(1).Basis(0).MinHeight(0).Padding(22, pagePadding, 0, pagePadding).
			Background(nodeAreaFill(t))
		pane.Children(func() {
			d.groupHeader(c, t)
			d.nodeGrid(c, t)
		})
	})
}

// groupHeader names the group shown, how it picks its node and where its
// traffic leaves now, with a way to test its nodes.
func (d *Dashboard) groupHeader(c *ui.Context, t *ui.Theme) {
	group, ok := d.activeGroupView()
	if !ok {
		return
	}
	ui.Row(c).Gap(gapMedium).AlignItems(ui.Center).Padding(0, 0, gapMedium, 0).Children(func() {
		groupIcon(c, t, group, 32)
		ui.Column(c).Grow(1).Gap(3).Children(func() {
			ui.Text(c, groupTitle(group)).FontSize(fontHeadline).FontWeight(weightSemibold).MaxLines(1)
			ui.Row(c).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
				mutedText(c, t, groupModes[group.Type])
				if group.Exit.Name != "" {
					mutedText(c, t, "出口")
					ui.Text(c, nodeDisplay(group.Exit)).FontSize(fontSecondary).FontWeight(weightMedium).MaxLines(1)
					delayLabel(c, t, group.Exit.Delay, fontSecondary)
				}
			})
		})
		slidingSwitch(c, "节点排序", &d.nodeOrder, nodeOrderLabels, pageSwitchStyle(t, nodeOrderSegmentWidth), func(int) {})
		d.testButton(c, t, group.Name)
	})
}

// nodeGrid lays the active group's options out by region, as tiles that
// fill the width beside the sidebar.
func (d *Dashboard) nodeGrid(c *ui.Context, t *ui.Theme) {
	group, ok := d.activeGroupView()
	if !ok {
		return
	}
	width, _ := c.Size()
	paneWidth := width - sidebarWidthFor(width) - hairlineWidth - 2*pagePadding
	columns := tileColumns(paneWidth)
	inner := paneWidth
	ui.Scroll(c).Grow(1).Basis(0).MinHeight(0).Children(func() {
		ui.Column(c).Gap(gapSection).Width(inner).Padding(0, 0, 30, 0).Children(func() {
			// Groups and DIRECT come first: a group often picks one of them.
			if len(group.Others) > 0 {
				d.nodeSection(c, t, group, "线路", group.Others, columns)
			}
			if d.nodeOrder == nodeOrderDelay {
				if nodes := nodesByDelay(group.Regions, d.orderDelay); len(nodes) > 0 {
					d.nodeSection(c, t, group, "按延迟", nodes, columns)
				}
				return
			}
			for _, region := range group.Regions {
				title := strings.TrimSpace(shownFlag(region.Flag) + " " + region.Name)
				d.nodeSection(c, t, group, title, region.Nodes, columns)
			}
		})
	})
}

// tileColumns fits as many tiles of at least minTileWidth as width holds.
func tileColumns(width float32) int {
	return max(1, int((width+tileGap)/(minTileWidth+tileGap)))
}

func (d *Dashboard) nodeSection(c *ui.Context, t *ui.Theme, group model.GroupView, title string, nodes []model.NodeView, columns int) {
	ui.Column(c).Key(title).Gap(10).Children(func() {
		ui.Row(c).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
			ui.Text(c, title).FontSize(fontSecondary).FontWeight(weightMedium).MaxLines(1)
			captionText(c, t, fmt.Sprint(len(nodes))).FontFeatures("tnum")
		})
		ui.Grid(c).Columns(columns).Gap(tileGap).Children(func() {
			for _, node := range nodes {
				d.nodeTile(c, t, group, node)
			}
		})
	})
}

// nodeTile is one option: its title and latency, its line and rate when
// the name gives them. The option
// in use is outlined in the accent colour; in a group that picks by itself,
// tiles show the choice but do not take clicks.
func (d *Dashboard) nodeTile(c *ui.Context, t *ui.Theme, group model.GroupView, node model.NodeView) {
	isActive := node.Name == group.Selected
	groupName := group.Name
	tile := ui.Column(c).Key(node.Name).Gap(7).Padding(10, 11).Radius(radiusTile)
	hovered := group.Selectable && tile.Hovered()
	if onGlass(t) {
		tile.Background(glassTile(t, hovered, isActive))
	} else {
		tile.Background(glassTile(t, hovered, isActive))
	}
	if isActive {
		tile.Border(hairlineWidth, glassSelectedBorder(t))
	} else {
		tile.Border(hairlineWidth, glassMaterialLine(t))
	}
	tile.Shadow(0, 3, 12, 0, glassCardShadow(t))
	if group.Selectable {
		tile.Cursor(ui.CursorPointer).OnClick(func() {
			if node.Name != group.Selected {
				d.selectProxy(groupName, node.Name)
			}
		})
	}
	tile.Children(func() {
		ui.Row(c).Key("top").Gap(gapSmall).AlignItems(ui.Center).Children(func() {
			title := ui.Text(c, tileTitle(node)).FontSize(tileTitleSize).MaxLines(1).Grow(1).Basis(0)
			if isActive {
				title.FontWeight(weightSemibold)
			} else {
				title.FontWeight(weightMedium)
			}
			nodeCheck(c, t, isActive)
		})
		ui.Row(c).Key("meta").Gap(7).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Gap(gapTight).Height(chipHeight).Clip().Grow(1).Basis(0).Children(func() {
				if node.Via != "" {
					captionText(c, t, node.Via).MaxLines(1)
					return
				}
				for _, label := range nodeLabels(node) {
					nodeChip(c, t, label)
				}
				if node.Multiplier != "" {
					nodeChip(c, t, node.Multiplier)
				}
			})
			latency := delayText(node.Delay)
			latencyInk := delayColor(t, node.Delay)
			if d.testing != "" {
				latency, latencyInk = "测速中", textOn(t, accentInk(t))
			}
			ui.Text(c, latency).FontSize(fontSecondary).FontWeight(weightMedium).TextColor(latencyInk).
				FontFeatures("tnum").NoWrap().ContentTransition(ui.ContentTransition{Numeric: true})
		})
	})
}

// nodeChip is a node's small label, on a tile or under the exit: a neutral
// wash with the secondary ink, so protocol, line and rate stay quiet and hue
// means state.
func nodeChip(c *ui.Context, t *ui.Theme, text string) {
	pill := ui.Box(c).Padding(1, 6).Radius(radiusChip).Background(t.Text.Alpha(neutralFillAlpha))
	pill.Children(func() {
		label := ui.Text(c, text).FontSize(fontCaption).FontWeight(weightMedium).TextColor(t.TextMuted).NoWrap()
		if strings.ContainsFunc(text, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
			label.Translate(0, -opticalLift)
		}
	})
}

// nodeCheck is the hollow choice ring on every tile, filled when chosen.
func nodeCheck(c *ui.Context, t *ui.Theme, selected bool) {
	ring := ui.Box(c).Key("check").Width(nodeCheckSize).Height(nodeCheckSize).Radius(nodeCheckSize/2).Center().
		Border(1, glassStrongLine(t))
	if selected {
		ring.Background(t.Accent).Border(1, t.Accent).Children(func() {
			ui.Icon(c, iconCheck).Width(nodeCheckIcon).Height(nodeCheckIcon).TextColor(t.AccentText)
		})
	}
}

// protocolNames shortens the core's protocol names the way people write
// them; names not listed show as the core gives them.
var protocolNames = map[string]string{
	"Shadowsocks":  "SS",
	"ShadowsocksR": "SSR",
	"Vmess":        "VMess",
	"Vless":        "VLESS",
	"Trojan":       "Trojan",
	"Hysteria":     "Hysteria",
	"Hysteria2":    "Hy2",
	"Tuic":         "TUIC",
	"WireGuard":    "WG",
	"Socks5":       "SOCKS5",
	"Http":         "HTTP",
	"Ssh":          "SSH",
}

// nodeLabels are the small labels under a node: its protocol, UDP when it
// carries it, then the lines its name gives.
func nodeLabels(node model.NodeView) []string {
	var labels []string
	if node.Protocol != "" {
		name, known := protocolNames[node.Protocol]
		if !known {
			name = node.Protocol
		}
		labels = append(labels, name)
	}
	if node.UDP {
		labels = append(labels, "UDP")
	}
	return append(labels, node.Tags...)
}

// tileChip is one small label on a tile.

// tileTitle names a node on its tile, as "Hong Kong 01": the section above
// already shows the flag. Groups and built-in outbounds keep their title.
func tileTitle(node model.NodeView) string {
	if node.Via != "" {
		return node.Title
	}
	return strings.TrimSpace(node.Region + " " + node.Title)
}

// nodeDisplay names a node as the route does: flag, region, title.
func nodeDisplay(node model.NodeView) string {
	return strings.TrimSpace(shownFlag(node.Flag) + " " + node.Region + " " + node.Title)
}

// delayLabel shows a latency in a colour for how usable it is: green fast,
// amber slow, red very slow or failed; a dash before any test.
func delayLabel(c *ui.Context, t *ui.Theme, delay model.Delay, size float32) {
	ui.Text(c, delayText(delay)).FontSize(size).FontWeight(weightMedium).TextColor(delayColor(t, delay)).FontFeatures("tnum").NoWrap().
		ContentTransition(ui.ContentTransition{Numeric: true})
}

// delayText words a latency: milliseconds, "超时" for a failed test, a dash before
// any test.
func delayText(delay model.Delay) string {
	switch {
	case !delay.Measured:
		return "—"
	case delay.Millis == 0:
		return "超时"
	default:
		return fmt.Sprintf("%d ms", delay.Millis)
	}
}

func delayColor(t *ui.Theme, delay model.Delay) ui.Color {
	switch {
	case !delay.Measured:
		return t.TextMuted
	case delay.Millis == 0, delay.Millis > moderateDelay:
		return textOn(t, t.Danger)
	case delay.Millis <= fastDelay:
		return textOn(t, accentInk(t))
	default:
		return textOn(t, t.Warning)
	}
}

// testButton measures every node of group; while any test runs it spins
// and takes no clicks.
func (d *Dashboard) testButton(c *ui.Context, t *ui.Theme, group string) {
	label := "测速"
	if d.testing != "" {
		label = "测速中"
	}
	button(c, buttonSpec{label: label, icon: iconBolt, style: mainSoftButton(t), busy: d.testing != ""}, func() { d.testDelay(group) })
}
