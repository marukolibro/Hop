package handler

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/locales"
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
	// nodeControlsWidth is the room the toolbar keeps for the controls at its
	// right: the order switch, the unlock check and the speed test.
	nodeControlsWidth float32 = 400
	// nodeCheckIcon is the check on the chosen tile.
	nodeCheckIcon float32 = 12
	// tileChipFill washes a tile label in its own colour.
	tileChipFill float32 = 0.10
	// tileInkShade moves a tier or accent ink toward the text ink, so a
	// small label keeps 4.5:1 on the tile over any wallpaper.
	tileInkShade float32 = 0.45
	// chosenChipFill washes a label on the chosen tile, which is filled with
	// the accent.
	chosenChipFill float32 = 0.2
	// onAccentMuted is the secondary ink on the accent.
	onAccentMuted float32 = 0.88
	// regionChipSlot is the width one region filter asks for, which sets how
	// many fit in the row before the rest go under 更多.
	regionChipSlot float32 = 150
	// regionChipHeight is a region filter's height.
	regionChipHeight float32 = 26
	// paneTopPadding is the content column's space above the hero card.
	paneTopPadding float32 = 16
)

// The region filters over the node grid: every option grouped, only the groups
// and DIRECT, or one region by "region:" and its name. An empty filter is the
// one the user has not chosen yet, which means the groups and DIRECT when the
// group has any and every option otherwise.
const (
	nodeFilterDefault = ""
	nodeFilterAll     = "all"
	nodeFilterOthers  = "others"
	nodeFilterRegion  = "region:"
)

func (d *Dashboard) activeGroupView() (model.GroupView, bool) {
	for _, g := range d.view.Groups {
		if g.Name == d.activeGroup {
			return g, true
		}
	}
	return model.GroupView{}, false
}

// nodesPage is the groups in a sidebar that runs the window's height, topped
// by the subscription in use, beside the content: the route's hero card, the
// row of home cards, then the options of the group chosen. The subscription management page is a
// subpage of it, reached from the sidebar's subscription.
func (d *Dashboard) nodesPage(c *ui.Context, t *ui.Theme) {
	if d.subscription.managing {
		d.managePage(c, t)
		return
	}
	// The body is one surface: a fixed sidebar, a hairline, and the content.
	// The content may shrink below its height, so it scrolls inside the
	// window rather than pushing past it.
	ui.Row(c).Grow(1).Basis(0).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		d.groupSidebar(c, t)
		verticalHairline(c, t)
		pane := ui.Column(c).Grow(1).Basis(0).MinHeight(0).Background(nodeAreaFill(t))
		pane.Children(func() {
			// The whole column scrolls as one: the hero, the cards, then the
			// options, so a group with many nodes runs past the window's foot.
			ui.Scroll(c).Grow(1).Basis(0).MinHeight(0).Children(func() {
				ui.Column(c).Gap(gapLarge).Padding(paneTopPadding, pagePadding, 30, pagePadding).Children(func() {
					d.routeBar(c, t)
					d.homeCards(c, t)
					ui.Column(c).Key("options").Gap(gapMedium).Disabled(d.controlsLocked()).Children(func() {
						d.nodesToolbar(c, t)
						d.nodeGrid(c, t)
					})
				})
			})
		})
	})
}

// nodesToolbar is the one row over the grid: the region filters on the left,
// and on the right how the options are ordered and the speed test. The group itself is not named again:
// the sidebar already shows which one is chosen, and the hero where traffic
// leaves. The controls wrap below the filters where a longer translation
// leaves the row too little room.
func (d *Dashboard) nodesToolbar(c *ui.Context, t *ui.Theme) {
	group, ok := d.activeGroupView()
	if !ok {
		return
	}
	ui.Row(c).Key("nodesToolbar").Gap(gapMedium).GapY(gapSmall).Wrap().AlignItems(ui.Center).Children(func() {
		d.regionFilter(c, t, group)
		ui.Row(c).Key("nodeControls").Gap(gapMedium).AlignItems(ui.Center).Children(func() {
			slidingSwitch(c, locales.NodesOrder(), &d.nodeOrder, nodeOrderLabels(), pageSwitchStyle(t, nodeOrderSegmentWidth), func(int) {})
			d.testButton(c, t, group.Name)
		})
	})
}

// regionChip is one filter over the grid: its state key, its label and how
// many options it holds.
type regionChip struct {
	key   string
	label string
	count int
}

// regionChips lists the filters a group offers: the groups and DIRECT first
// when the group has any, then every option, then one per region.
func regionChips(group model.GroupView) []regionChip {
	var chips []regionChip
	if len(group.Others) > 0 {
		chips = append(chips, regionChip{key: nodeFilterOthers, label: locales.ColumnRoute(), count: len(group.Others)})
	}
	total := len(group.Others)
	var regions []regionChip
	for _, region := range group.Regions {
		total += len(region.Nodes)
		regions = append(regions, regionChip{
			key:   nodeFilterRegion + region.Name,
			label: strings.TrimSpace(shownFlag(region.Flag) + " " + region.Name),
			count: len(region.Nodes),
		})
	}
	chips = append(chips, regionChip{key: nodeFilterAll, label: locales.All(), count: total})
	return append(chips, regions...)
}

// activeFilter is filter when group offers it. A filter not chosen yet, or
// left over from another group, is the group's default.
func activeFilter(group model.GroupView, filter string) string {
	chips := regionChips(group)
	if slices.ContainsFunc(chips, func(chip regionChip) bool { return chip.key == filter }) {
		return filter
	}
	return chips[0].key
}

// nodeSection is a titled run of options in the grid; an empty title shows
// none.
type nodeSection struct {
	title string
	nodes []model.NodeView
}

// visibleSections are the options the grid shows for filter. Every option
// comes grouped: the groups and DIRECT first, as a group often picks one of
// them, then one section per region in the subscription's order or, when
// byDelay, one list from the fastest node. A single filter is one section
// without a title.
func visibleSections(group model.GroupView, filter string, byDelay bool, delayOf func(model.NodeView) model.Delay) []nodeSection {
	switch {
	case filter == nodeFilterOthers:
		return []nodeSection{{nodes: group.Others}}
	case strings.HasPrefix(filter, nodeFilterRegion):
		for _, region := range group.Regions {
			if nodeFilterRegion+region.Name == filter {
				return []nodeSection{{nodes: regionNodes([]model.RegionView{region}, byDelay, delayOf)}}
			}
		}
		return nil
	}
	var sections []nodeSection
	if len(group.Others) > 0 {
		sections = append(sections, nodeSection{title: locales.ColumnRoute(), nodes: group.Others})
	}
	if byDelay {
		return append(sections, nodeSection{title: locales.NodesByLatency(), nodes: regionNodes(group.Regions, true, delayOf)})
	}
	for _, region := range group.Regions {
		sections = append(sections, nodeSection{title: strings.TrimSpace(shownFlag(region.Flag) + " " + region.Name), nodes: region.Nodes})
	}
	return sections
}

// regionNodes are the nodes of regions in the subscription's order, or from
// the fastest when byDelay.
func regionNodes(regions []model.RegionView, byDelay bool, delayOf func(model.NodeView) model.Delay) []model.NodeView {
	if byDelay {
		return nodesByDelay(regions, delayOf)
	}
	var nodes []model.NodeView
	for _, region := range regions {
		nodes = append(nodes, region.Nodes...)
	}
	return nodes
}

// splitRegionChips keeps the filters that fit one row: the groups and every
// option always, then as many regions as slots allow, the largest first and
// the chosen one for certain. The rest are more, in the subscription's order.
func splitRegionChips(chips []regionChip, current string, slots int) (shown []regionChip, more []regionChip) {
	var fixed []regionChip
	var regions []regionChip
	for _, chip := range chips {
		if strings.HasPrefix(chip.key, nodeFilterRegion) {
			regions = append(regions, chip)
		} else {
			fixed = append(fixed, chip)
		}
	}
	if len(regions) <= slots {
		return chips, nil
	}
	order := make([]int, len(regions))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a int, b int) int { return cmp.Compare(regions[b].count, regions[a].count) })
	picked := order[:slots]
	if chosen := slices.IndexFunc(regions, func(chip regionChip) bool { return chip.key == current }); chosen >= 0 && !slices.Contains(picked, chosen) {
		picked[slots-1] = chosen
	}
	shown = fixed
	for i, chip := range regions {
		if slices.Contains(picked, i) {
			shown = append(shown, chip)
		} else {
			more = append(more, chip)
		}
	}
	return shown, more
}

// regionFilter chooses which region's options the grid shows, in one row that
// takes what the toolbar's controls leave; the regions that do not fit are in
// a menu. A group with no regions to choose between leaves the room empty.
func (d *Dashboard) regionFilter(c *ui.Context, t *ui.Theme, group model.GroupView) {
	chips := regionChips(group)
	row := ui.Row(c).Key("regionFilter").Gap(gapTight).Grow(1).Basis(0).MinWidth(0)
	if len(chips) <= 2 {
		row.Children(func() {})
		return
	}
	slots := max(1, int((nodesPaneWidth(c)-nodeControlsWidth)/regionChipSlot))
	current := activeFilter(group, d.nodeRegion)
	shown, more := splitRegionChips(chips, current, slots)
	row.Children(func() {
		for _, chip := range shown {
			key := chip.key
			d.regionChipButton(c, t, chip, key == current).OnClick(func() { d.nodeRegion = key })
		}
		if len(more) > 0 {
			d.regionChipButton(c, t, regionChip{key: "more", label: locales.NodesMore(), count: len(more)}, false).Menu(func(m *ui.Menu) {
				for _, chip := range more {
					if m.Item(fmt.Sprintf("%s  %d", chip.label, chip.count)).Chosen() {
						d.nodeRegion = chip.key
					}
				}
			})
		}
	})
}

// regionChipButton is a filter capsule: the text ink filled when chosen.
func (d *Dashboard) regionChipButton(c *ui.Context, t *ui.Theme, chip regionChip, chosen bool) ui.Element {
	b := ui.ButtonBase(c).Key(chip.key).Label(chip.label).Height(regionChipHeight).Padding(0, gapMedium).Gap(gapTight).
		Radius(regionChipHeight / 2).Cursor(ui.CursorPointer)
	ink, sub := t.Text, t.TextMuted
	fill := glassPanel(t)
	if b.Hovered() {
		fill = glassTile(t, true, false)
	}
	if chosen {
		fill, ink, sub = t.Text, chosenFilterInk(t), chosenFilterInk(t).Alpha(onAccentMuted)
	}
	b.Background(b.AnimateColor("fill", fill, ui.Smooth)).Border(hairlineWidth, glassMaterialLine(t))
	b.Children(func() {
		ui.Text(c, chip.label).FontSize(fontSecondary).FontWeight(weightMedium).TextColor(ink).NoWrap().Translate(0, -opticalLift)
		ui.Text(c, strconv.Itoa(chip.count)).FontSize(fontSecondary).TextColor(sub).FontFeatures("tnum").NoWrap().Translate(0, -opticalLift)
	})
	return b
}

// chosenFilterInk is the label on a chosen filter, whose fill is the text
// ink turned over.
func chosenFilterInk(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.Hex("#151D2F")
	}
	return ui.RGB(255, 255, 255)
}

// nodeGrid lays the options the filter keeps out as tiles that fill the
// width beside the sidebar, under a title for each section.
func (d *Dashboard) nodeGrid(c *ui.Context, t *ui.Theme) {
	group, ok := d.activeGroupView()
	if !ok {
		return
	}
	width, _ := c.Size()
	paneWidth := width - sidebarWidthFor(width) - hairlineWidth - 2*pagePadding
	columns := tileColumns(paneWidth)
	sections := visibleSections(group, activeFilter(group, d.nodeRegion), d.nodeOrder == nodeOrderDelay, d.orderDelay)
	ui.Column(c).Key("nodeGrid").Gap(gapSection).Children(func() {
		for _, section := range sections {
			d.nodeSection(c, t, group, section, columns)
		}
	})
}

// nodeSection is one titled run of tiles.
func (d *Dashboard) nodeSection(c *ui.Context, t *ui.Theme, group model.GroupView, section nodeSection, columns int) {
	ui.Column(c).Key("section:" + section.title).Gap(10).Children(func() {
		if section.title != "" {
			ui.Row(c).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
				ui.Text(c, section.title).FontSize(fontSecondary).FontWeight(weightMedium).MaxLines(1)
				captionText(c, t, strconv.Itoa(len(section.nodes))).FontFeatures("tnum")
			})
		}
		ui.Grid(c).Columns(columns).Gap(tileGap).Children(func() {
			for _, node := range section.nodes {
				d.nodeTile(c, t, group, node)
			}
		})
	})
}

// tileColumns fits as many tiles of at least minTileWidth as width holds.
func tileColumns(width float32) int {
	return max(1, int((width+tileGap)/(minTileWidth+tileGap)))
}

// nodeTile is one option: its flag, title and latency, its line and rate
// when the name gives them. The option in use is filled with the accent and
// carries a check; in a group that picks by itself, tiles show the choice but
// do not take clicks.
func (d *Dashboard) nodeTile(c *ui.Context, t *ui.Theme, group model.GroupView, node model.NodeView) {
	isActive := node.Name == group.Selected
	groupName := group.Name
	// The chosen tile is read on the accent, every other on the window.
	ink := t
	if isActive {
		ink = onAccentTheme(t)
	}
	tile := ui.Column(c).Key(node.Name).Gap(7).Padding(10, 11).Radius(radiusTile)
	hovered := group.Selectable && tile.Hovered()
	if isActive {
		tile.Background(t.Accent).Border(hairlineWidth, t.Accent)
	} else {
		tile.Background(glassTile(t, hovered, false)).Border(hairlineWidth, glassMaterialLine(t))
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
			if flag := shownFlag(node.Flag); flag != "" && node.Via == "" {
				ui.Text(c, flag).FontSize(tileTitleSize)
			}
			title := ui.Text(c, tileTitle(node)).FontSize(tileTitleSize).TextColor(ink.Text).MaxLines(1).Grow(1).Basis(0)
			if isActive {
				title.FontWeight(weightSemibold)
			} else {
				title.FontWeight(weightMedium)
			}
			if isActive {
				ui.Icon(c, iconCheck).Key("check").Width(nodeCheckIcon).Height(nodeCheckIcon).TextColor(ink.Text)
			}
		})
		ui.Row(c).Key("meta").Gap(7).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Gap(gapTight).Height(chipHeight).Clip().Grow(1).Basis(0).Children(func() {
				if node.Via != "" {
					captionText(c, ink, node.Via).MaxLines(1)
					return
				}
				chip := func(label string) { nodeChip(c, ink, label) }
				if isActive {
					chip = func(label string) { labelChip(c, label, ink.Text.Alpha(chosenChipFill), ink.TextMuted) }
				}
				for _, label := range nodeLabels(node) {
					chip(label)
				}
				if node.Multiplier != "" {
					chip(node.Multiplier)
				}
			})
			latency := delayText(node.Delay)
			latencyInk := delayColor(t, node.Delay)
			if isActive {
				latencyInk = ink.Text
			}
			if d.testing != "" {
				latency = locales.NodesTesting()
				if !isActive {
					latencyInk = textOn(t, accentInk(t))
				}
			}
			ui.Text(c, latency).FontSize(fontSecondary).FontWeight(weightMedium).TextColor(latencyInk).
				FontFeatures("tnum").NoWrap().ContentTransition(ui.ContentTransition{Numeric: true})
		})
	})
}

// onAccentTheme is t as seen on a block filled with the accent, such as the
// chosen node tile: the accent's text colour is the ink.
func onAccentTheme(t *ui.Theme) *ui.Theme {
	h := *t
	h.Text = t.AccentText
	h.TextMuted = t.AccentText.Alpha(onAccentMuted)
	h.Accent = t.AccentText
	return &h
}

// nodeChip is a node's small label, on a tile or under the exit: a neutral
// wash with the secondary ink, so protocol, line and rate stay quiet and hue
// means state.
func nodeChip(c *ui.Context, t *ui.Theme, text string) {
	labelChip(c, text, t.Text.Alpha(neutralFillAlpha), t.TextMuted)
}

// labelChip is a small label with its own wash and ink. CJK glyphs sit low in
// a box this small, so a label with any is lifted.
func labelChip(c *ui.Context, text string, fill ui.Color, ink ui.Color) {
	pill := ui.Box(c).Padding(1, 6).Radius(radiusChip).Background(fill)
	pill.Children(func() {
		label := ui.Text(c, text).FontSize(fontCaption).FontWeight(weightMedium).TextColor(ink).NoWrap()
		if strings.ContainsFunc(text, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
			label.Translate(0, -opticalLift)
		}
	})
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
		return locales.NodesTimeout()
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
	label := locales.NodesTestDelay()
	if d.testing != "" {
		label = locales.NodesTesting()
	}
	button(c, buttonSpec{label: label, icon: iconBolt, style: mainSoftButton(t), busy: d.testing != ""}, func() { d.testDelay(group) })
}
