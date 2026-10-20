package handler

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
)

const (
	sidebarWidth    float32 = 236
	sidebarItemH    float32 = 46
	sidebarHeaderH  float32 = 30
	sidebarIconSize float32 = 26
	// searchThreshold is how many groups a profile has before the sidebar
	// offers to search them.
	searchThreshold = 8
)

// sidebarSections are the sidebar's sections, by what groups are for.
var sidebarSections = []struct {
	kind  string
	title string
}{
	{model.GroupKindMain, "主线路"},
	{model.GroupKindAuto, "自动选择"},
	{model.GroupKindRouting, "分流"},
	{model.GroupKindRegion, "地区"},
	{model.GroupKindOther, "其他"},
}

// sidebarRow is one row of the sidebar: a section title or a group.
type sidebarRow struct {
	title string
	group model.GroupView
	isTop bool
}

// groupSidebar lists every group by what it is for, with where each sends
// traffic now. The highlight springs to the group chosen.
func (d *Dashboard) groupSidebar(c *ui.Context, t *ui.Theme) {
	rows := sidebarRows(d.view.Groups, d.groupFilter)
	ui.Column(c).Width(sidebarWidth).MinHeight(0).Gap(gapSmall).Children(func() {
		if len(d.view.Groups) > searchThreshold {
			ui.Row(c).Gap(gapTight).AlignItems(ui.Center).Height(switchHeight).Padding(0, 10).Radius(switchHeight / 2).
				Background(t.Text.Alpha(softFillAlpha)).Children(func() {
				ui.Icon(c, iconSearch).Width(13).Height(13).TextColor(t.TextMuted)
				ui.TextInput(c, &d.groupFilter).Placeholder("搜索策略组").Grow(1).Background(ui.Transparent).Border(0, ui.Transparent).Padding(0)
			})
		}
		ui.Scroll(c).Grow(1).Basis(0).MinHeight(0).Children(func() {
			list := ui.Box(c).Padding(0, 0, gapLarge, 0)
			activeY, found := float32(0), false
			y := float32(0)
			for _, row := range rows {
				if row.title != "" {
					y += sidebarHeaderH
					continue
				}
				if row.group.Name == d.activeGroup {
					activeY, found = y, true
				}
				y += sidebarItemH
			}
			highlightY := list.AnimateTo("highlight", activeY, ui.Snappy)
			list.Children(func() {
				if found {
					ui.Box(c).Absolute().Left(0).Top(highlightY).Width(sidebarWidth).Height(sidebarItemH).
						Radius(radiusControl).Background(t.Accent.Alpha(0.1))
				}
				ui.Column(c).Children(func() {
					for _, row := range rows {
						if row.title != "" {
							sidebarHeader(c, t, row.title, row.isTop)
							continue
						}
						d.sidebarItem(c, t, row.group)
					}
				})
			})
		})
	})
}

// sidebarRows lays the groups out under their section titles, keeping the
// profile's order within a section and only groups whose name contains
// filter, ignoring case.
func sidebarRows(groups []model.GroupView, filter string) []sidebarRow {
	needle := strings.ToLower(strings.TrimSpace(filter))
	var rows []sidebarRow
	for _, section := range sidebarSections {
		var members []sidebarRow
		for _, g := range groups {
			if g.Kind == section.kind && strings.Contains(strings.ToLower(g.Label), needle) {
				members = append(members, sidebarRow{group: g})
			}
		}
		if len(members) == 0 {
			continue
		}
		rows = append(rows, sidebarRow{title: section.title, isTop: len(rows) == 0})
		rows = append(rows, members...)
	}
	return rows
}

func sidebarHeader(c *ui.Context, t *ui.Theme, title string, isTop bool) {
	top := float32(12)
	if isTop {
		top = 4
	}
	ui.Box(c).Key("section:"+title).Height(sidebarHeaderH).Padding(top, 10, 0, 10).Children(func() {
		ui.Text(c, title).FontSize(fontCaption).FontWeight(weightSemibold).TextColor(t.TextMuted)
	})
}

// sidebarItem is one group: its icon, its name, and where it sends traffic.
func (d *Dashboard) sidebarItem(c *ui.Context, t *ui.Theme, group model.GroupView) {
	isActive := group.Name == d.activeGroup
	name := group.Name
	item := ui.Row(c).Key(name).Height(sidebarItemH).Gap(10).AlignItems(ui.Center).Padding(0, 10).Radius(radiusControl).Cursor(ui.CursorPointer)
	fill := ui.Transparent
	if item.Hovered() && !isActive {
		fill = t.Text.Alpha(0.04)
	}
	item.Background(item.AnimateColor("fill", fill, ui.Smooth))
	item.OnClick(func() { d.activeGroup = name })
	item.Children(func() {
		groupIcon(c, t, group, sidebarIconSize)
		ui.Column(c).Grow(1).Basis(0).Gap(1).Children(func() {
			title := ui.Text(c, groupTitle(group)).FontSize(fontBody).MaxLines(1)
			if isActive {
				title.FontWeight(weightSemibold).TextColor(t.Accent)
			} else {
				title.FontWeight(weightMedium)
			}
			ui.Text(c, groupDestination(group)).FontSize(fontCaption).TextColor(t.TextMuted).MaxLines(1)
		})
		if group.Exit.Name != "" && group.Kind != model.GroupKindRouting {
			delayLabel(c, t, group.Exit.Delay, fontCaption)
		}
	})
}

// serviceIconScale draws a service's icon a little larger than the plain
// glyphs, which are lighter.
const serviceIconScale float32 = 1.12

// serviceIcons are the icons of the services a group can route.
var serviceIcons = map[string]*ui.SVG{
	model.ServiceVideo:     iconVideo,
	model.ServiceMusic:     iconMusic,
	model.ServiceAI:        iconSparkle,
	model.ServiceSearch:    iconSearch,
	model.ServiceApple:     iconPhone,
	model.ServiceMicrosoft: iconGrid,
	model.ServiceGame:      iconGamepad,
	model.ServiceChat:      iconChat,
	model.ServiceTelegram:  iconPlane,
	model.ServiceSocial:    iconUsers,
	model.ServiceCloud:     iconCloud,
	model.ServiceDev:       iconCode,
	model.ServiceShop:      iconBag,
	model.ServiceDownload:  iconDownload,
	model.ServiceAdBlock:   iconShield,
	model.ServiceDirect:    iconHome,
	model.ServiceFinal:     iconTarget,
}

// groupIcon marks a group: its flag for a region, an icon for the service a
// routing group serves or the first letter of its name when it serves none
// Hop knows, and otherwise an icon for what it is for, on a soft tile.
func groupIcon(c *ui.Context, t *ui.Theme, group model.GroupView, size float32) {
	flag, isRegional := service.LeadingFlag(group.Label)
	ui.Box(c).Width(size).Height(size).Radius(size * 0.3).Center().Background(t.Text.Alpha(softFillAlpha)).Children(func() {
		glyph := size * 0.55
		switch {
		case isRegional:
			ui.Text(c, flag).FontSize(glyph)
		case group.Kind == model.GroupKindMain:
			hopMark(c, glyph, solidInk(t.Accent))
		case group.Kind == model.GroupKindAuto:
			ui.Icon(c, iconBolt).Width(glyph).Height(glyph).TextColor(t.Warning)
		case group.Kind == model.GroupKindRouting:
			serviceMark(c, t, group, size, glyph)
		default:
			ui.Icon(c, iconList).Width(glyph).Height(glyph).TextColor(t.TextMuted)
		}
	})
}

// serviceMark is the mark of a routing group: its service's icon, or the
// first letter of its name.
func serviceMark(c *ui.Context, t *ui.Theme, group model.GroupView, size float32, glyph float32) {
	if icon, known := serviceIcons[group.Service]; known {
		mark := glyph * serviceIconScale
		ui.Icon(c, icon).Width(mark).Height(mark).TextColor(t.Accent)
		return
	}
	for _, r := range group.Label {
		ui.Text(c, strings.ToUpper(string(r))).FontSize(size*0.42).FontWeight(weightSemibold).TextColor(t.Accent).Translate(0, -opticalLift)
		return
	}
}

// groupTitle is a group's name without the flag its icon already shows.
func groupTitle(group model.GroupView) string {
	flag, isRegional := service.LeadingFlag(group.Label)
	if !isRegional {
		return group.Label
	}
	return strings.TrimSpace(strings.TrimPrefix(group.Label, flag))
}

// groupDestination says where a group sends traffic: a service's group the
// route it takes, any other group the node it leaves by.
func groupDestination(group model.GroupView) string {
	if group.Kind == model.GroupKindRouting || group.Exit.Name == "" {
		return "→ " + group.Pick
	}
	if group.Exit.PlanNote {
		return "→ 机场信息节点"
	}
	return "→ " + nodeDisplay(group.Exit)
}
