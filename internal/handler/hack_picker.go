package handler

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/locales"
)

const (
	// hackPickerWidth and hackPickerListHeight size the node list: the width
	// holds a name, its labels and a signal bar on one row, the height scrolls
	// a long list.
	hackPickerWidth      float32 = 600
	hackPickerListHeight float32 = 380
)

// hackPickerDialog is the Hack theme's node list: the options of the group
// clicked in the routes tree, as terminal rows, with the region filters, the
// order and the speed test over them.
func (d *Dashboard) hackPickerDialog(c *ui.Context, t *ui.Theme) {
	if !hackTheme(t) {
		return
	}
	animatedDialog(c, "hackPicker", &d.hackPickerOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(dialogScrim)
		panel.Width(hackPickerWidth).Padding(hackBoxPad).Gap(gapMedium).
			Background(overlaySurface(t)).Border(hairlineWidth, t.Accent).Shadow(0, 0, hackLightBlur, 0, t.Accent.Alpha(hackLight/2))
		group, ok := d.activeGroupView()
		panel.Children(func() {
			if !ok {
				return
			}
			d.hackPickerHead(c, t, group)
			hackDashedLine(c, t)
			d.hackPickerFilters(c, t, group)
			ui.Scroll(c.Key("hackPickerScroll")).MaxHeight(hackPickerListHeight).Disabled(d.controlsLocked()).Children(func() {
				ui.Column(c).Gap(gapSmall).Padding(0, gapLarge, 0, 0).Children(func() {
					filter := activeFilter(group, d.nodeRegion)
					for _, section := range visibleSections(group, filter, d.nodeOrder == nodeOrderDelay, d.orderDelay) {
						d.hackPickerSection(c, t, group, section)
					}
				})
			})
		})
	})
}

// hackPickerHead names the group and what it uses now, with the controls: the
// order as two words, the chosen one inverted, the speed test and closing.
func (d *Dashboard) hackPickerHead(c *ui.Context, t *ui.Theme, group model.GroupView) {
	ui.Row(c.Key("head")).Gap(gapSmall).GapY(gapTight).Wrap().AlignItems(ui.Center).Children(func() {
		ui.Text(c, "> "+shownName(group.Label)).FontSize(fontEmphasis).FontWeight(weightBold).TextColor(t.Accent).MaxLines(1).Shrink(1)
		if group.Selected != "" {
			captionText(c, t, shownName(group.Selected)).MaxLines(1).Shrink(1)
		}
		ui.Box(c).Grow(1)
		for i, label := range nodeOrderLabels() {
			hackChip(c, t, label, d.nodeOrder == i, func() { d.nodeOrder = i })
		}
		label := locales.NodesTestDelay()
		if d.testing != "" {
			label = locales.NodesTesting()
		}
		hackTextButton(c, t, label, d.testing != "", func() { d.testDelay(group.Name) })
		hackTextButton(c, t, locales.Close(), false, func() { d.hackPickerOpen = false })
	})
}

// hackPickerFilters is the region filters, wrapping onto more rows as they
// need.
func (d *Dashboard) hackPickerFilters(c *ui.Context, t *ui.Theme, group model.GroupView) {
	chips := regionChips(group)
	if len(chips) <= 2 {
		return
	}
	current := activeFilter(group, d.nodeRegion)
	ui.Row(c.Key("filters")).Gap(gapSmall).GapY(gapTight).Wrap().AlignItems(ui.Center).Children(func() {
		for _, chip := range chips {
			key := chip.key
			hackChip(c, t, chip.label+" "+strconv.Itoa(chip.count), key == current, func() { d.nodeRegion = key })
		}
	})
}

// hackChip is a choice shown as a word: inverted while chosen.
func hackChip(c *ui.Context, t *ui.Theme, label string, chosen bool, onClick func()) {
	b := ui.ButtonBase(c.Key("chip-"+label)).Label(label).Padding(0, gapSmall).Cursor(ui.CursorPointer)
	ink := t.TextMuted
	switch {
	case chosen:
		b.Background(t.Accent)
		ink = t.AccentText
	case b.Hovered():
		b.Background(t.Accent.Alpha(neutralFillAlpha * 2))
		ink = t.Accent
	}
	b.OnClick(onClick)
	b.Children(func() {
		ui.Text(c, label).FontSize(fontSecondary).FontWeight(weightSemibold).TextColor(ink).NoWrap()
	})
}

// hackPickerSection is one titled run of rows.
func (d *Dashboard) hackPickerSection(c *ui.Context, t *ui.Theme, group model.GroupView, section nodeSection) {
	ui.Column(c.Key("section:" + section.title)).Gap(gapLine).Children(func() {
		if section.title != "" {
			ui.Row(c.Key("title")).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "── "+section.title+" "+strconv.Itoa(len(section.nodes))).FontSize(fontSecondary).TextColor(t.TextMuted).NoWrap()
			})
		}
		for _, node := range section.nodes {
			d.hackPickerRow(c, t, group, node)
		}
	})
}

// hackPickerRow is one option: a radio mark, its name and labels, and its
// signal and latency; the group's choice carries the mark filled.
func (d *Dashboard) hackPickerRow(c *ui.Context, t *ui.Theme, group model.GroupView, node model.NodeView) {
	chosen := node.Name == group.Selected
	row := ui.Row(c.Key(node.Name)).Gap(gapSmall).AlignItems(ui.Center).Padding(0, gapTight)
	ink := t.Text
	switch {
	case chosen:
		row.Background(t.Accent.Alpha(neutralFillAlpha * 2))
	case group.Selectable:
		row.Cursor(ui.CursorPointer)
		if row.Hovered() {
			row.Background(t.Accent.Alpha(neutralFillAlpha * 2))
			ink = t.Accent
		}
	}
	if group.Selectable && !chosen {
		groupName, name := group.Name, node.Name
		row.OnClick(func() { d.selectProxy(groupName, name) })
	}
	row.Children(func() {
		mark, markInk := "( )", t.TextMuted
		if chosen {
			mark, markInk = "(•)", t.Accent
		}
		ui.Text(c, mark).FontSize(fontBody).TextColor(markInk).NoWrap()
		weight := weightMedium
		if chosen {
			weight = weightBold
		}
		ui.Text(c, tileTitle(node)).FontSize(fontBody).FontWeight(weight).TextColor(ink).MaxLines(1).Shrink(1)
		if node.Via != "" {
			captionText(c, t, node.Via).MaxLines(1).Shrink(1)
		} else if labels := strings.Join(nodeLabels(node), " "); labels != "" {
			captionText(c, t, labels).MaxLines(1).Shrink(1)
		}
		ui.Box(c).Grow(1)
		if node.Protocol == "" {
			return
		}
		color := delayColor(t, node.Delay)
		text := delayText(node.Delay)
		if d.testing != "" {
			text = locales.NodesTesting()
		}
		hackBar(c, t, hackSignal(node.Delay, hackBranchCells), hackBranchCells, color)
		ui.Text(c, text).FontSize(fontBody).FontWeight(weightBold).TextColor(color).FontFeatures("tnum").NoWrap().
			Width(hackDelayWidth).TextAlign(ui.End)
	})
}

// hackDelayWidth holds a latency, or the word for testing, so the bars line up.
const hackDelayWidth float32 = 64
