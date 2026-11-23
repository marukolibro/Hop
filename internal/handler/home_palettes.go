package handler

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
	"hop/locales"
)

// nodePickerDialog is the centered macOS floating glass palette for picking
// nodes and switching strategy groups. It stays compact (1-col) for 1-4 node
// profiles and widens (2-col with Spotlight search, sort and region filters)
// for multi-node airports.
func (d *Dashboard) nodePickerDialog(c *ui.Context, t *ui.Theme) {
	if hackTheme(t) {
		return
	}
	animatedDialog(c, "nodePicker", &d.nodePickerOpen, func(backdrop, panel ui.Element) {
		group, ok := d.activeGroupView()
		if !ok && len(d.view.Groups) > 0 {
			group = d.view.Groups[0]
			ok = true
		}
		wide := d.nodePickerIsWide(group)
		width := homePaletteCompactW
		if wide {
			width = homePaletteWideW
		}
		backdrop.Background(dialogScrim)
		panel.Width(width).Padding(homePalettePad).Gap(gapMedium).Radius(homePaletteRadius).
			Background(overlaySurface(t)).Border(hairlineWidth, glassMaterialLine(t)).
			Shadow(0, 18, 44, 0, dialogShadow)
		panel.Children(func() {
			if !ok {
				return
			}
			d.nodePickerHeader(c, t, group, wide)
			if wide {
				d.nodePickerRegions(c, t, group, width-2*homePalettePad)
			}
			horizontalHairline(c, t)
			d.nodePickerList(c, t, group, wide)
			d.nodePickerFooter(c, t, group)
		})
	})
}

// nodePickerIsWide switches the Node Picker from a compact single-column
// palette to the full Spotlight search + 2-column airport layout.
func (d *Dashboard) nodePickerIsWide(group model.GroupView) bool {
	if len(d.view.Groups) > 2 {
		return true
	}
	total := len(group.Others) + len(groupRealNodes(group))
	return total > homeFewNodesLimit
}

func (d *Dashboard) nodePickerHeader(c *ui.Context, t *ui.Theme, group model.GroupView, wide bool) {
	groupName := group.Name
	total := len(group.Others) + len(groupRealNodes(group))
	ui.Row(c.Key("npHead")).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
		if wide && len(d.view.Groups) > 1 {
			d.nodePickerGroupMenu(c, t, group)
			if dest := groupDestination(group); dest != "" {
				mutedText(c, t, dest).Grow(1).Basis(0).MinWidth(0).MaxLines(1)
			} else {
				ui.Box(c).Grow(1)
			}
		} else {
			ui.Text(c, groupTitle(group)).FontSize(fontEmphasis).FontWeight(weightSemibold).MaxLines(1)
			badge(c, t, locales.HomeNodeCount(total))
			ui.Box(c).Grow(1)
		}
		if fastest, ok := fastestNodeInGroup(group); ok && group.Selectable {
			button(c, buttonSpec{
				label:    locales.HomePickFastest(),
				icon:     iconSparkle,
				style:    softButton(t),
				disabled: d.controlsLocked() || fastest == group.Selected,
			}, func() {
				d.selectProxy(groupName, fastest)
			})
		}
		testLabel := locales.NodesTestDelay()
		if d.testing != "" {
			testLabel = locales.NodesTesting()
		}
		button(c, buttonSpec{
			label:    testLabel,
			icon:     iconBolt,
			style:    softButton(t),
			disabled: d.controlsLocked(),
			busy:     d.testing != "",
		}, func() { d.testDelay(groupName) })
		iconButton(c, locales.Close(), iconClose, softButton(t), func() { d.nodePickerOpen = false })
	})
}

// nodePickerGroupMenu is a single compact group selector pill in the palette
// header so multi-group airports never wrap into multiple rows of group chips.
func (d *Dashboard) nodePickerGroupMenu(c *ui.Context, t *ui.Theme, active model.GroupView) {
	title := groupTitle(active)
	b := ui.ButtonBase(c.Key("npGroupMenu")).Label(title).Height(switchHeight).
		Padding(0, 10).Gap(5).Radius(switchHeight / 2).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	fill := t.Accent.Alpha(0.13)
	if b.Hovered() {
		fill = t.Accent.Alpha(0.20)
	}
	b.Background(b.AnimateColor("fill", fill, ui.Smooth))
	b.Menu(func(m *ui.Menu) {
		for _, g := range d.view.Groups {
			name := g.Name
			itemLabel := groupTitle(g)
			if dest := groupDestination(g); dest != "" {
				itemLabel += "  " + dest
			}
			if m.Item(itemLabel).Checked(name == d.activeGroup).Chosen() {
				d.activeGroup = name
				d.nodeRegion = nodeFilterDefault
			}
		}
	})
	b.Children(func() {
		ui.Text(c, title).FontSize(fontSecondary).FontWeight(weightSemibold).TextColor(accentInk(t)).
			MaxLines(1).MaxWidth(110).Translate(0, -opticalLift)
		ui.Icon(c, iconChevron).Width(iconInline).Height(iconInline).TextColor(accentInk(t))
	})
}

func (d *Dashboard) paletteToggleChip(c *ui.Context, t *ui.Theme, key, label string, active bool, onClick func()) {
	b := ui.ButtonBase(c.Key(key)).Label(label).Height(homeInlinePillHeight).Padding(0, 9).
		Radius(homeInlinePillHeight / 2).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	fill, ink := horizonPillIdle(t), t.TextMuted
	switch {
	case active:
		fill, ink = t.Accent.Alpha(0.14), accentInk(t)
	case b.Hovered():
		fill, ink = horizonPillHover(t), t.Text
	}
	b.Background(b.AnimateColor("fill", fill, ui.Smooth))
	b.OnClick(onClick)
	b.Children(func() {
		weight := weightMedium
		if active {
			weight = weightSemibold
		}
		ui.Text(c, label).FontSize(fontCaption).FontWeight(weight).TextColor(ink).NoWrap().Translate(0, -opticalLift)
	})
}

// nodePickerRegions renders the unified filter-and-sort bar on a single clean
// row: region filter capsules on the left (overflow tucked into "More") and
// view-filter toggles (sort by latency, hide timed out) aligned on the right.
func (d *Dashboard) nodePickerRegions(c *ui.Context, t *ui.Theme, group model.GroupView, availW float32) {
	orderLabel := locales.NodesByLatency()
	hideLabel := locales.HomeHideTimeout()
	ow, _ := c.MeasureText(0, ui.Span{Text: orderLabel, Size: fontCaption, Weight: weightSemibold})
	hw, _ := c.MeasureText(0, ui.Span{Text: hideLabel, Size: fontCaption, Weight: weightSemibold})
	// Reserve room on the right for the two toggle pills plus gap.
	rightW := (ow + 18) + gapTight + (hw + 18) + gapMedium
	chips := regionChips(group)
	total := len(group.Others) + len(groupRealNodes(group))

	ui.Row(c.Key("npRegions")).Gap(gapTight).AlignItems(ui.Center).Children(func() {
		if len(chips) > 2 {
			current := activeFilter(group, d.nodeRegion)
			shown, more := fitPaletteRegionChips(c, chips, current, max(0, availW-rightW))
			for _, ch := range shown {
				key := ch.key
				d.paletteRegionPill(c, t, "npReg-"+key, ch.label, ch.count, key == current, false).OnClick(func() {
					d.nodeRegion = key
				})
			}
			if len(more) > 0 {
				d.paletteRegionPill(c, t, "npReg-more", locales.NodesMore(), len(more), false, true).Menu(func(m *ui.Menu) {
					for _, ch := range more {
						key := ch.key
						if m.Item(fmt.Sprintf("%s  %d", ch.label, ch.count)).Checked(key == current).Chosen() {
							d.nodeRegion = key
						}
					}
				})
			}
		} else {
			captionText(c, t, locales.HomeNodeCount(total)).FontFeatures("tnum").NoWrap()
		}
		ui.Box(c).Grow(1)
		byDelay := d.nodeOrder == nodeOrderDelay
		d.paletteToggleChip(c, t, "npOrder", orderLabel, byDelay, func() {
			if d.nodeOrder == nodeOrderDelay {
				d.nodeOrder = nodeOrderSubscription
			} else {
				d.nodeOrder = nodeOrderDelay
			}
		})
		d.paletteToggleChip(c, t, "npHideTimeout", hideLabel, d.nodeHideTimeout, func() {
			d.nodeHideTimeout = !d.nodeHideTimeout
		})
	})
}

func (d *Dashboard) paletteRegionPill(c *ui.Context, t *ui.Theme, key, label string, count int, chosen, withChevron bool) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Label(label).Height(homeInlinePillHeight).
		Padding(0, 9).Gap(4).Radius(homeInlinePillHeight / 2).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	fill, ink, countInk := horizonPillIdle(t), t.TextMuted, t.TextMuted
	switch {
	case chosen:
		fill, ink, countInk = t.Accent.Alpha(0.14), accentInk(t), accentInk(t)
	case b.Hovered():
		fill, ink = horizonPillHover(t), t.Text
	}
	b.Background(b.AnimateColor("fill", fill, ui.Smooth))
	b.Children(func() {
		weight := weightMedium
		if chosen {
			weight = weightSemibold
		}
		ui.Text(c, label).FontSize(fontCaption).FontWeight(weight).TextColor(ink).NoWrap().Translate(0, -opticalLift)
		ui.Text(c, strconv.Itoa(count)).FontSize(fontCaption).TextColor(countInk).FontFeatures("tnum").NoWrap().Translate(0, -opticalLift)
		if withChevron {
			ui.Icon(c, iconChevron).Width(10).Height(10).TextColor(t.TextMuted)
		}
	})
	return b
}

// fitPaletteRegionChips picks as many region filters (largest first, always
// keeping the active filter) as fit cleanly on one horizontal row within
// availW, placing the rest into the overflow menu.
func fitPaletteRegionChips(c *ui.Context, chips []regionChip, current string, availW float32) ([]regionChip, []regionChip) {
	chipW := func(label string, count int, withChevron bool) float32 {
		lw, _ := c.MeasureText(0, ui.Span{Text: label, Size: fontCaption, Weight: weightSemibold})
		cw, _ := c.MeasureText(0, ui.Span{Text: strconv.Itoa(count), Size: fontCaption})
		extra := float32(18 + 4) // horizontal padding + gap
		if withChevron {
			extra += 14
		}
		return lw + cw + extra
	}
	totalW := func(list []regionChip, moreCount int) float32 {
		w := float32(0)
		for i, ch := range list {
			if i > 0 {
				w += gapTight
			}
			w += chipW(ch.label, ch.count, false)
		}
		if moreCount > 0 {
			if len(list) > 0 {
				w += gapTight
			}
			w += chipW(locales.NodesMore(), moreCount, true)
		}
		return w
	}
	regionCount := 0
	for _, ch := range chips {
		if strings.HasPrefix(ch.key, nodeFilterRegion) {
			regionCount++
		}
	}
	for slots := regionCount; slots >= 0; slots-- {
		shown, more := splitRegionChips(chips, current, slots)
		if totalW(shown, len(more)) <= availW {
			return shown, more
		}
	}
	return splitRegionChips(chips, current, 0)
}

// nodePickerList renders the scrollable filtered node options (1 column when
// compact, 2 columns when wide).
func (d *Dashboard) nodePickerList(c *ui.Context, t *ui.Theme, group model.GroupView, wide bool) {
	filter := activeFilter(group, d.nodeRegion)
	sections := visibleSections(group, filter, d.nodeOrder == nodeOrderDelay, d.orderDelay)
	filtered := filterNodeSections(sections, d.nodeHideTimeout, group.Selected)

	cols := 1
	if wide {
		cols = 2
	}
	ui.Scroll(c.Key("npScroll")).MaxHeight(homePaletteScrollH).Disabled(d.controlsLocked()).Children(func() {
		ui.Column(c.Key("npList")).Gap(gapMedium).Padding(0, gapSmall, 0, 0).Children(func() {
			if len(filtered) == 0 {
				ui.Box(c.Key("npEmpty")).Padding(gapSection, 0).Center().Children(func() {
					mutedText(c, t, locales.HomeNoMatchNodes())
				})
				return
			}
			for _, sec := range filtered {
				ui.Column(c.Key("npSec:" + sec.title)).Gap(gapTight).Children(func() {
					if sec.title != "" && wide {
						ui.Row(c).Gap(gapTight).AlignItems(ui.Center).Children(func() {
							captionText(c, t, sec.title).FontWeight(weightSemibold)
							captionText(c, t, strconv.Itoa(len(sec.nodes))).FontFeatures("tnum")
						})
					}
					if cols == 1 {
						ui.Column(c).Gap(4).Children(func() {
							for _, node := range sec.nodes {
								d.nodePaletteRow(c, t, group, node)
							}
						})
					} else {
						ui.Grid(c).Columns(2).Gap(gapSmall).GapY(4).Children(func() {
							for _, node := range sec.nodes {
								d.nodePaletteRow(c, t, group, node)
							}
						})
					}
				})
			}
		})
	})
}

// filterNodeSections optionally omits timed-out nodes while always keeping the
// currently selected node visible.
func filterNodeSections(sections []nodeSection, hideTimeout bool, selected string) []nodeSection {
	if !hideTimeout {
		return sections
	}
	var out []nodeSection
	for _, sec := range sections {
		var matched []model.NodeView
		for _, node := range sec.nodes {
			if node.Name != selected && node.Via == "" && node.Delay.Measured && node.Delay.Millis == 0 {
				continue
			}
			matched = append(matched, node)
		}
		if len(matched) > 0 {
			out = append(out, nodeSection{title: sec.title, nodes: matched})
		}
	}
	return out
}

// fastestNodeInGroup returns the proxy node in group with the lowest measured
// positive latency, or ("", false) when none have a positive delay yet.
func fastestNodeInGroup(group model.GroupView) (string, bool) {
	bestName := ""
	bestDelay := 0
	for _, region := range group.Regions {
		for _, node := range region.Nodes {
			if node.Delay.Measured && node.Delay.Millis > 0 && (bestDelay == 0 || node.Delay.Millis < bestDelay) {
				bestName = node.Name
				bestDelay = node.Delay.Millis
			}
		}
	}
	return bestName, bestName != ""
}

// nodePaletteRow is one borderless rounded option inside the Node Picker.
func (d *Dashboard) nodePaletteRow(c *ui.Context, t *ui.Theme, group model.GroupView, node model.NodeView) {
	isActive := node.Name == group.Selected
	groupName, nodeName := group.Name, node.Name
	row := ui.Row(c.Key("npRow:"+nodeName)).Padding(homeShelfRowPadY, homeShelfRowPadX).Gap(gapSmall).
		Radius(homePaletteRowRadius).AlignItems(ui.Center)
	fill := ui.Transparent
	switch {
	case isActive:
		fill = t.Accent.Alpha(0.12)
	case group.Selectable && row.Hovered():
		fill = horizonPillIdle(t)
	}
	row.Background(row.AnimateColor("fill", fill, ui.Smooth))
	if group.Selectable {
		isAutoGroup := group.Type == model.GroupURLTest || group.Type == model.GroupFallback
		row.Cursor(ui.CursorPointer).OnClick(func() {
			if nodeName != group.Selected || (isAutoGroup && group.Fixed == "") {
				d.selectProxy(groupName, nodeName)
			}
		})
	}
	row.Children(func() {
		if flag := shownFlag(node.Flag); flag != "" && node.Via == "" {
			ui.Text(c, flag).FontSize(fontEmphasis)
		}
		ui.Column(c.Key("meta")).Grow(1).Basis(0).MinWidth(0).Gap(2).Children(func() {
			titleInk, weight := t.Text, weightMedium
			if isActive {
				titleInk, weight = accentInk(t), weightSemibold
			}
			ui.Text(c, tileTitle(node)).FontSize(fontBody).FontWeight(weight).TextColor(titleInk).MaxLines(1)
			sub := node.Via
			if sub == "" {
				labels := nodeLabels(node)
				if node.Multiplier != "" {
					labels = append(labels, node.Multiplier)
				}
				sub = strings.Join(labels, " · ")
			}
			if sub != "" {
				captionText(c, t, sub).MaxLines(1)
			}
		})
		if node.Via == "" {
			testingThis := d.testingNode == nodeName
			latency := delayText(node.Delay)
			latencyInk := delayColor(t, node.Delay)
			if d.testing != "" || testingThis {
				latency = locales.NodesTesting()
				latencyInk = accentInk(t)
			}
			latBtn := ui.ButtonBase(c.Key("lat")).Label(locales.HomeTestNodeTip()).
				Padding(3, 6).Radius(radiusChip).Gap(5).AlignItems(ui.Center).
				Tooltip(locales.HomeTestNodeTip()).Disabled(d.controlsLocked() || d.testing != "" || testingThis)
			latFill := ui.Transparent
			if latBtn.Hovered() && !d.controlsLocked() && d.testing == "" && !testingThis {
				latFill = t.Text.Alpha(0.06)
			}
			latBtn.Background(latBtn.AnimateColor("fill", latFill, ui.Smooth)).Cursor(ui.CursorPointer).OnClick(func() {
				if d.testNode != nil {
					d.testNode(nodeName)
				}
			})
			latBtn.Children(func() {
				if testingThis {
					spinner(c, 10, accentInk(t))
				} else {
					ui.Box(c.Key("dot")).Width(homeShelfDotSize).Height(homeShelfDotSize).
						Radius(homeShelfDotSize / 2).Background(latencyInk)
				}
				ui.Text(c, latency).FontSize(fontCaption).FontWeight(weightMedium).TextColor(latencyInk).
					FontFeatures("tnum").NoWrap()
			})
		}
		if isActive {
			ui.Icon(c.Key("check"), iconCheck).Width(iconInline).Height(iconInline).TextColor(accentInk(t))
		}
	})
}

func (d *Dashboard) nodePickerFooter(c *ui.Context, t *ui.Theme, group model.GroupView) {
	exitName := "—"
	if len(d.view.Route) > 0 {
		exitName = horizonExitTitle(d.view.Route[len(d.view.Route)-1])
	}
	isAutoGroup := group.Type == model.GroupURLTest || group.Type == model.GroupFallback
	groupName := group.Name
	ui.Column(c.Key("npFoot")).Gap(gapSmall).Children(func() {
		horizontalHairline(c, t)
		ui.Row(c).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
			captionText(c, t, locales.HomeCurrentExitLabel(exitName)).MaxLines(1).Grow(1).Basis(0)
			switch {
			case isAutoGroup && group.Fixed != "":
				button(c, buttonSpec{
					label:    locales.HomePaletteResetAuto(),
					style:    softButton(t),
					disabled: d.controlsLocked(),
				}, func() {
					if d.resetProxy != nil {
						d.resetProxy(groupName)
					}
				})
			case isAutoGroup:
				captionText(c, t, locales.HomePaletteAutoPinHint()).NoWrap()
			case group.Selectable:
				captionText(c, t, locales.HomePaletteApplyHint()).NoWrap()
			}
		})
	})
}

// subPickerDialog is the centered macOS floating glass palette for switching
// subscriptions, visually unified with nodePickerDialog.
func (d *Dashboard) subPickerDialog(c *ui.Context, t *ui.Theme) {
	if hackTheme(t) {
		return
	}
	animatedDialog(c, "subPicker", &d.subPickerOpen, func(backdrop, panel ui.Element) {
		s := &d.subscription
		backdrop.Background(dialogScrim)
		panel.Width(homePaletteCompactW).Padding(homePalettePad).Gap(gapMedium).Radius(homePaletteRadius).
			Background(overlaySurface(t)).Border(hairlineWidth, glassMaterialLine(t)).
			Shadow(0, 18, 44, 0, dialogShadow)
		panel.Children(func() {
			d.subPickerHeader(c, t)
			d.subPickerList(c, t, s)
			d.subPickerFooter(c, t)
		})
	})
}

func (d *Dashboard) subPickerHeader(c *ui.Context, t *ui.Theme) {
	s := &d.subscription
	active, hasActive := s.active()
	ui.Row(c.Key("spHead")).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.HomeSwitchSubscription()).FontSize(fontEmphasis).FontWeight(weightSemibold)
		badge(c, t, locales.SubCount(len(s.view.Items)))
		ui.Box(c).Grow(1)
		if hasActive && active.Source == model.SourceRemote {
			busy := s.busy(active.ID)
			label := locales.HomeRefreshCurrent()
			if busy {
				label = locales.Refreshing()
			}
			button(c, buttonSpec{
				label:    label,
				icon:     iconRefresh,
				style:    softButton(t),
				disabled: d.controlsLocked(),
				busy:     busy,
			}, func() { d.refreshSubscription(active.ID) })
		}
		iconButton(c, locales.Close(), iconClose, softButton(t), func() { d.subPickerOpen = false })
	})
}

func (d *Dashboard) subPickerList(c *ui.Context, t *ui.Theme, s *subscriptionsState) {
	now := time.Now()
	barWidth := homePaletteCompactW - 2*homePalettePad - 26 - gapSmall
	ui.Scroll(c.Key("spScroll")).MaxHeight(homeSubPaletteScrollH).Disabled(d.controlsLocked()).Children(func() {
		ui.Column(c.Key("spList")).Gap(4).Padding(0, gapSmall, 0, 0).Children(func() {
			if len(s.view.Items) == 0 {
				ui.Box(c.Key("spEmpty")).Padding(gapSection, 0).Center().Children(func() {
					mutedText(c, t, locales.SubNoneInUse())
				})
				return
			}
			for _, item := range s.view.Items {
				d.subPaletteRow(c, t, item, item.ID == s.view.ActiveID, now, barWidth)
			}
		})
	})
}

func (d *Dashboard) subPaletteRow(c *ui.Context, t *ui.Theme, item model.SubscriptionSummary, isActive bool, now time.Time, barWidth float32) {
	id := item.ID
	row := ui.Column(c.Key("spRow:"+id)).Padding(11, 13).Gap(5).Radius(homePaletteRowRadius).Cursor(ui.CursorPointer)
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
			d.useSubscription(id)
		}
		d.subPickerOpen = false
	})
	row.Children(func() {
		ui.Row(c.Key("top")).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
			nameInk := t.Text
			if isActive {
				nameInk = accentInk(t)
			}
			ui.Text(c, item.Name).FontSize(fontEmphasis).FontWeight(weightSemibold).TextColor(nameInk).MaxLines(1).Grow(1).Basis(0)
			if item.Rules == model.RulesHop {
				chip(c, t, locales.SubRuleSmart(), accentInk(t))
			} else if item.Source == model.SourceLocal {
				chip(c, t, service.LocalProfileName(), t.TextMuted)
			}
			if isActive {
				ui.Icon(c.Key("check"), iconCheck).Width(iconInline).Height(iconInline).TextColor(accentInk(t))
			}
		})
		usage, hasTotal := item.Usage, item.HasUsage && item.Usage.Total > 0
		if !item.HasUsage {
			usage = service.MonthlyProxyForSource(d.stats, item.ID)
		}
		used := formatBytes(usage.Upload + usage.Download)
		leftText := used.value + " " + used.unit
		if hasTotal {
			total := formatBytes(usage.Total)
			leftText = fmt.Sprintf("%s %s / %s %s", used.value, used.unit, total.value, total.unit)
		}
		rightText := locales.HomeNoAutoRefresh()
		if item.Source == model.SourceRemote {
			if item.HasUsage && !item.Usage.Expire.IsZero() {
				rightText = remainingText(item.Usage.Expire.Sub(now))
			} else {
				rightText = item.Host
			}
		}
		ui.Row(c.Key("meta")).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
			captionText(c, t, leftText).FontFeatures("tnum").NoWrap()
			ui.Box(c).Grow(1)
			captionText(c, t, rightText).MaxLines(1)
		})
		if hasTotal {
			ratio := min(1, float32(usage.Upload+usage.Download)/float32(usage.Total))
			stripUsageBar(c, t, ratio, barWidth)
		} else {
			stripLocalUsageBar(c, t, usage, barWidth)
		}
		if item.LastError != "" {
			warningLine(c, t, item.LastError)
		}
	})
}

func (d *Dashboard) subPickerFooter(c *ui.Context, t *ui.Theme) {
	ui.Column(c.Key("spFoot")).Gap(gapSmall).Children(func() {
		horizontalHairline(c, t)
		ui.Row(c).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
			button(c, buttonSpec{
				label:    locales.SubAdd(),
				icon:     iconPlus,
				style:    softButton(t),
				disabled: d.controlsLocked(),
			}, func() {
				d.subPickerOpen = false
				d.openEditor(editorAdd, "")
			})
			ui.Box(c).Grow(1)
			shelfLink(c, t, "manageAllSubs", locales.HomeManageAllSubs(), func() {
				d.subPickerOpen = false
				d.page = pageNodes
				d.subscription.managing = true
			})
		})
	})
}
