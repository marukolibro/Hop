package handler

import (
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
	"hop/locales"
)

// connectionColumns are the columns of the connection table, by index.
const (
	colHost = iota
	colRule
	colRoute
	colDownload
	colUpload
	colAddRule
	colClose
)

const (
	connRouteAll = iota
	connRouteProxy
	connRouteDirect
)

const (
	connSortNewest = iota
	connSortTraffic
	connSortHost
)

func connSortLabels() []string {
	return []string{locales.ConnectionsSortNewest(), locales.ConnectionsSortTraffic(), locales.ConnectionsSortHost()}
}

func connectionColumns() []ui.TableColumn {
	return []ui.TableColumn{
		colHost:     {Title: locales.ColumnHost(), MinWidth: 165},
		colRule:     {Title: locales.ColumnRule(), Width: 155, MinWidth: 90},
		colRoute:    {Title: locales.ColumnRoute(), Width: 135, MinWidth: 90},
		colDownload: {Title: locales.ColumnDownload(), Width: 84, Align: ui.End},
		colUpload:   {Title: locales.ColumnUpload(), Width: 84, Align: ui.End},
		colAddRule:  {Title: "", ID: "add-rule", Width: 36, Fixed: true, Align: ui.Center},
		colClose:    {Title: "", ID: "close", Width: 36, Fixed: true, Align: ui.Center},
	}
}

// activeConnectionsData returns the frozen snapshot while paused, or the live
// connection table otherwise.
func (d *Dashboard) activeConnectionsData() model.Connections {
	if d.connections.paused {
		return d.connections.pausedSnapshot
	}
	return d.connections.data
}

// hasConnectionFilter reports whether a text search or route filter narrows
// the connection table.
func (d *Dashboard) hasConnectionFilter() bool {
	return strings.TrimSpace(d.connections.filter) != "" || d.connections.routeFilter != connRouteAll
}

// toggleConnectionsPause freezes or unfreezes the visible connection table
// without stopping background traffic recording.
func (d *Dashboard) toggleConnectionsPause() {
	if d.connections.paused {
		d.connections.paused = false
		d.connections.pausedSnapshot = model.Connections{}
		return
	}
	snap := d.connections.data
	snap.Active = slices.Clone(snap.Active)
	d.connections.pausedSnapshot = snap
	d.connections.paused = true
}

// dropPausedConnections removes closed connection IDs from the frozen table so
// close actions reflect immediately while paused.
func (d *Dashboard) dropPausedConnections(ids ...string) {
	if !d.connections.paused || len(ids) == 0 {
		return
	}
	dropped := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		dropped[id] = struct{}{}
	}
	kept := make([]model.Connection, 0, len(d.connections.pausedSnapshot.Active))
	for _, conn := range d.connections.pausedSnapshot.Active {
		if _, ok := dropped[conn.ID]; !ok {
			kept = append(kept, conn)
		}
	}
	d.connections.pausedSnapshot.Active = kept
}

// connectionsView lists the connections the core carries, with search, route
// filter pills, sort selector, live-stream pause, and filtered or full batch
// close.
func (d *Dashboard) connectionsView(c *ui.Context, t *ui.Theme, contentW float32) {
	conns := d.activeConnectionsData()
	columns, order := connectionLayout(contentW < 700)
	ui.Column(c.Key("connections")).Grow(1).Basis(0).MinHeight(0).Gap(gapMedium).Children(func() {
		ui.Row(c.Key("connToolbar")).Wrap().Gap(gapSmall).GapY(gapSmall).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c.Key("connSearch"), &d.connections.filter).
				Placeholder(locales.ConnectionsFilter()).Width(205)
			d.connRouteFilterPills(c, t)
			d.connSortMenu(c, t)
			d.connPauseButton(c, t)
			ui.Box(c).Grow(1)
			// The text input takes its edit as its constructor returns, inside
			// this build, so visible rows are computed after it.
			visibleNow := d.visibleConnections()
			if d.hasConnectionFilter() {
				ids := make([]string, len(visibleNow))
				for i, conn := range visibleNow {
					ids[i] = conn.ID
				}
				button(c, buttonSpec{
					label:    locales.ConnectionsCloseFiltered(len(visibleNow)),
					icon:     iconClose,
					style:    mainDangerButton(t),
					disabled: len(visibleNow) == 0,
				}, func() {
					d.dropPausedConnections(ids...)
					if d.closeConnections != nil {
						d.closeConnections(ids)
					}
				})
			} else {
				button(c, buttonSpec{
					label:    locales.ConnectionsCloseAll(),
					icon:     iconClose,
					style:    mainDangerButton(t),
					disabled: len(conns.Active) == 0,
				}, func() {
					if d.connections.paused {
						d.connections.pausedSnapshot.Active = nil
					}
					if d.closeAll != nil {
						d.closeAll()
					}
				})
			}
		})
		visible := d.visibleConnections()
		d.connections.list.Key = func(row int) any { return visible[row].ID }
		switch {
		case len(conns.Active) == 0:
			emptyState(c, t, locales.ConnectionsEmpty(), locales.ConnectionsEmptyHint(d.proxyAddress()))
		case len(visible) == 0:
			query := strings.TrimSpace(d.connections.filter)
			if query == "" {
				query = connRouteFilterLabel(d.connections.routeFilter)
			}
			emptyState(c, t, locales.ConnectionsNoMatch(), locales.ConnectionsNoMatchHint(query))
		default:
			panelCard(c, t).Key("connTableCard").Grow(1).Basis(0).MinHeight(0).
				Radius(radiusPanel+2).Shadow(0, 3, 12, 0, glassCardShadow(t)).Clip().Children(func() {
				ui.Table(c, &d.connections.list, columns, len(visible), func(row int, col int) {
					d.connectionCell(c, t, visible[row], order[col])
				}).Fill()
			})
		}
	})
}

func (d *Dashboard) connSortMenu(c *ui.Context, t *ui.Theme) {
	labels := connSortLabels()
	cur := max(0, min(d.connections.sort, len(labels)-1))
	b := ui.ButtonBase(c.Key("connSort")).Label(labels[cur]).Height(switchHeight).
		Padding(0, 10).Gap(gapTight).Radius(switchHeight / 2).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	fill := horizonPillIdle(t)
	if b.Hovered() {
		fill = horizonPillHover(t)
	}
	b.Background(b.AnimateColor("fill", fill, ui.Smooth))
	b.Menu(func(m *ui.Menu) {
		for i, label := range labels {
			idx := i
			if m.Item(label).Checked(idx == cur).Chosen() {
				d.connections.sort = idx
			}
		}
	})
	b.Children(func() {
		ui.Text(c, labels[cur]).FontSize(fontSecondary).FontWeight(weightMedium).TextColor(t.Text).
			NoWrap().Translate(0, -opticalLift)
		ui.Icon(c, iconChevron).Width(iconInline).Height(iconInline).TextColor(t.TextMuted)
	})
}

func (d *Dashboard) connPauseButton(c *ui.Context, t *ui.Theme) {
	paused := d.connections.paused
	label := locales.ConnectionsPause()
	icon := iconPause
	if paused {
		label = locales.ConnectionsResume()
		icon = iconPlay
	}
	b := ui.ButtonBase(c.Key("connPause")).Label(label).Height(switchHeight).
		Padding(0, 10).Gap(gapTight).Radius(switchHeight / 2).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	fill, ink := horizonPillIdle(t), t.TextMuted
	switch {
	case paused:
		fill, ink = t.Warning.Alpha(0.16), textOn(t, t.Warning)
	case b.Hovered():
		fill, ink = horizonPillHover(t), t.Text
	}
	b.Background(b.AnimateColor("fill", fill, ui.Smooth))
	b.OnClick(d.toggleConnectionsPause)
	b.Children(func() {
		ui.Icon(c, icon).Width(iconInline).Height(iconInline).TextColor(ink)
		text := label
		if paused {
			text = locales.ConnectionsPausedBadge() + " · " + label
		}
		ui.Text(c, text).FontSize(fontSecondary).FontWeight(weightMedium).TextColor(ink).
			NoWrap().Translate(0, -opticalLift)
	})
}

func connRouteFilterLabel(idx int) string {
	switch idx {
	case connRouteProxy:
		return locales.RouteProxy()
	case connRouteDirect:
		return locales.RouteDirect()
	default:
		return locales.All()
	}
}

func (d *Dashboard) connRouteFilterPills(c *ui.Context, t *ui.Theme) {
	labels := []string{locales.All(), locales.RouteProxy(), locales.RouteDirect()}
	ui.Row(c.Key("connRoutePills")).Gap(gapTight).AlignItems(ui.Center).Children(func() {
		for i, label := range labels {
			idx := i
			chosen := d.connections.routeFilter == idx
			b := ui.ButtonBase(c.Key("connRoute-"+strconv.Itoa(idx))).Label(label).
				Height(switchHeight).Padding(0, 10).Radius(switchHeight / 2).
				AlignItems(ui.Center).Cursor(ui.CursorPointer)
			fill, ink, weight := horizonPillIdle(t), t.TextMuted, weightMedium
			switch {
			case chosen:
				fill, ink, weight = t.Accent.Alpha(0.14), accentInk(t), weightSemibold
			case b.Hovered():
				fill, ink = horizonPillHover(t), t.Text
			}
			b.Background(b.AnimateColor("fill", fill, ui.Smooth))
			b.OnClick(func() { d.connections.routeFilter = idx })
			b.Children(func() {
				ui.Text(c, label).FontSize(fontSecondary).FontWeight(weight).TextColor(ink).NoWrap().Translate(0, -opticalLift)
			})
		}
	})
}

// connectionLayout drops the rule column when the table would be crowded;
// MyGo's table has no horizontal scroll.
func connectionLayout(narrow bool) ([]ui.TableColumn, []int) {
	order := []int{colHost, colRule, colRoute, colDownload, colUpload, colAddRule, colClose}
	if narrow {
		order = []int{colHost, colRoute, colDownload, colUpload, colAddRule, colClose}
	}
	columns := make([]ui.TableColumn, len(order))
	for i, col := range order {
		columns[i] = connectionColumns()[col]
	}
	return columns, order
}

func (d *Dashboard) connectionCell(c *ui.Context, t *ui.Theme, conn model.Connection, col int) {
	switch col {
	case colHost:
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, conn.Host+":"+conn.Port).FontSize(fontBody).FontWeight(weightMedium).MaxLines(1)
			meta := conn.Process
			if net := strings.ToUpper(conn.Network); net != "" {
				if meta != "" {
					meta += " · " + net
				} else {
					meta = net
				}
			}
			if meta != "" {
				captionText(c, t, meta).MaxLines(1)
			}
		})
	case colRule:
		mutedText(c, t, conn.Rule).MaxLines(1)
	case colRoute:
		direct := isDirectChain(conn.Chain)
		dot := t.Accent
		if direct {
			dot = t.TextMuted.Alpha(statsDirectAlpha)
		}
		ui.Row(c).Gap(gapTight).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Width(homeShelfDotSize).Height(homeShelfDotSize).Radius(homeShelfDotSize / 2).Background(dot)
			ui.Text(c, routeLabel(conn.Chain)).FontSize(fontSecondary).MaxLines(1).Shrink(1)
		})
	case colDownload:
		amount(c, conn.Download, t.Text)
	case colUpload:
		amount(c, conn.Upload, t.TextMuted)
	case colAddRule:
		iconButton(c, locales.ConnectionAddRule(), iconPlus, softButton(t), func() { d.openRuleEditorFor(conn) })
	case colClose:
		id := conn.ID
		iconButton(c, locales.ConnectionClose(), iconClose, dangerButton(t), func() {
			d.dropPausedConnections(id)
			if d.closeConnection != nil {
				d.closeConnection(id)
			}
		})
	}
}

func amount(c *ui.Context, bytes int64, ink ui.Color) {
	r := formatBytes(bytes)
	ui.Text(c, r.value+" "+r.unit).FontSize(fontSecondary).TextColor(ink).FontFeatures("tnum").NoWrap()
}

// routeLabel names the node a connection leaves by, the way the node grid does,
// or "Direct" when it skips the proxies.
func routeLabel(chain []string) string {
	if len(chain) == 0 {
		return ""
	}
	exit := chain[len(chain)-1]
	if exit == "DIRECT" {
		return locales.RouteDirect()
	}
	node := service.ParseNode(exit, nil)
	if node.Flag == "" {
		return exit
	}
	return strings.TrimSpace(shownFlag(node.Flag) + " " + node.Region + " " + node.Title)
}

// visibleConnections are the connections the filter, route pills, and sort
// order keep.
func (d *Dashboard) visibleConnections() []model.Connection {
	filtered := filterConnections(d.activeConnectionsData().Active, d.connections.filter)
	var out []model.Connection
	if d.connections.routeFilter == connRouteAll {
		out = slices.Clone(filtered)
	} else {
		wantDirect := d.connections.routeFilter == connRouteDirect
		for _, conn := range filtered {
			if isDirectChain(conn.Chain) == wantDirect {
				out = append(out, conn)
			}
		}
	}
	sortConnections(out, d.connections.sort)
	return out
}

// sortConnections orders connections in place according to order, breaking ties
// deterministically by Start descending then ID.
func sortConnections(conns []model.Connection, order int) {
	if len(conns) < 2 {
		return
	}
	slices.SortStableFunc(conns, func(a, b model.Connection) int {
		switch order {
		case connSortTraffic:
			at, bt := a.Download+a.Upload, b.Download+b.Upload
			if at != bt {
				if at > bt {
					return -1
				}
				return 1
			}
		case connSortHost:
			ah, bh := strings.ToLower(a.Host), strings.ToLower(b.Host)
			if ah != bh {
				return strings.Compare(ah, bh)
			}
		}
		if !a.Start.Equal(b.Start) {
			if a.Start.After(b.Start) {
				return -1
			}
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
}

// filterConnections keeps the connections whose host, rule, route, or process
// contains filter, ignoring case. The input is not modified.
func filterConnections(conns []model.Connection, filter string) []model.Connection {
	needle := strings.ToLower(strings.TrimSpace(filter))
	if needle == "" {
		return conns
	}
	var out []model.Connection
	for _, conn := range conns {
		haystack := strings.ToLower(conn.Host + " " + conn.Rule + " " + strings.Join(conn.Chain, " ") + " " + conn.Process)
		if strings.Contains(haystack, needle) {
			out = append(out, conn)
		}
	}
	return out
}
