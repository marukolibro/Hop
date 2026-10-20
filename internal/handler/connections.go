package handler

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
)

// connectionColumns are the columns of the connection table, by index.
const (
	colHost = iota
	colRule
	colRoute
	colDownload
	colUpload
	colAge
	colClose
)

var connectionColumns = []ui.TableColumn{
	colHost:     {Title: "主机", MinWidth: 160},
	colRule:     {Title: "规则", Width: 170, MinWidth: 90},
	colRoute:    {Title: "线路", Width: 150, MinWidth: 90},
	colDownload: {Title: "下载", Width: 76, Align: ui.End},
	colUpload:   {Title: "上传", Width: 76, Align: ui.End},
	colAge:      {Title: "时长", Width: 60, Align: ui.End},
	colClose:    {Title: "", ID: "close", Width: 40, Fixed: true, Align: ui.Center},
}

// connectionsPage lists the connections the core carries, newest first,
// with a filter and a way to close them.
func (d *Dashboard) connectionsPage(c *ui.Context, t *ui.Theme) {
	conns := d.connections.data
	visible := d.visibleConnections()
	toolbar(c, func() {
		ui.TextInput(c, &d.connections.filter).Placeholder("筛选主机、规则或节点").Width(260)
		mutedText(c, t, fmt.Sprintf("%d 条连接", len(conns.Active))).FontFeatures("tnum")
		total(c, t, "↓", conns.DownloadTotal, t.Accent)
		total(c, t, "↑", conns.UploadTotal, t.Warning)
		ui.Box(c).Grow(1)
		button(c, buttonSpec{label: "全部断开", icon: iconClose, style: dangerButton(t), disabled: len(conns.Active) == 0}, d.closeAll)
	})
	switch {
	case len(conns.Active) == 0:
		emptyState(c, t, "还没有连接经过 Hop",
			fmt.Sprintf("Hop 没有设为系统代理。把浏览器或应用的代理设为 %s(HTTP 或 SOCKS5),它们的连接就会出现在这里。", d.proxyAddress))
	case len(visible) == 0:
		emptyState(c, t, "没有匹配的连接", fmt.Sprintf("没有连接的主机、规则或节点包含「%s」。", strings.TrimSpace(d.connections.filter)))
	default:
		now := time.Now()
		ui.Box(c).Grow(1).Padding(0, pagePadding, gapLarge, pagePadding).Children(func() {
			ui.Table(c, &d.connections.list, connectionColumns, len(visible), func(row int, col int) {
				d.connectionCell(c, t, visible[row], col, now)
			}).Fill()
		})
	}
}

func total(c *ui.Context, t *ui.Theme, arrow string, bytes int64, color ui.Color) {
	r := formatBytes(bytes)
	ui.Row(c).Key(arrow).Gap(4).AlignItems(ui.End).Children(func() {
		ui.Text(c, arrow).FontSize(fontSecondary).FontWeight(weightBold).TextColor(color)
		ui.Text(c, r.value+" "+r.unit).FontSize(fontSecondary).FontFeatures("tnum")
	})
}

func (d *Dashboard) connectionCell(c *ui.Context, t *ui.Theme, conn model.Connection, col int, now time.Time) {
	switch col {
	case colHost:
		ui.Column(c).Gap(1).Children(func() {
			ui.Text(c, conn.Host+":"+conn.Port).FontSize(fontBody).MaxLines(1)
			if conn.Process != "" {
				ui.Text(c, conn.Process).FontSize(fontCaption).TextColor(t.TextMuted).MaxLines(1)
			}
		})
	case colRule:
		ui.Text(c, conn.Rule).FontSize(fontSecondary).TextColor(t.TextMuted).MaxLines(1)
	case colRoute:
		ui.Text(c, routeLabel(conn.Chain)).FontSize(fontSecondary).MaxLines(1)
	case colDownload:
		amount(c, conn.Download)
	case colUpload:
		amount(c, conn.Upload)
	case colAge:
		ui.Text(c, formatAge(now.Sub(conn.Start))).FontSize(fontSecondary).TextColor(t.TextMuted).FontFeatures("tnum")
	case colClose:
		id := conn.ID
		iconButton(c, "断开连接", iconClose, dangerButton(t), func() { d.closeConnection(id) })
	}
}

func amount(c *ui.Context, bytes int64) {
	r := formatBytes(bytes)
	ui.Text(c, r.value+" "+r.unit).FontSize(fontSecondary).FontFeatures("tnum").NoWrap()
}

// routeLabel names the node a connection leaves by, the way the node grid
// does, or "直连" when it skips the proxies.
func routeLabel(chain []string) string {
	if len(chain) == 0 {
		return ""
	}
	exit := chain[len(chain)-1]
	if exit == "DIRECT" {
		return "直连"
	}
	node := service.ParseNode(exit, nil)
	if node.Flag == "" {
		return exit
	}
	return strings.TrimSpace(shownFlag(node.Flag) + " " + node.Region + " " + node.Title)
}

// formatAge renders how long a connection has been open, in its largest
// unit.
func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
}

// visibleConnections are the connections the filter lets through.
func (d *Dashboard) visibleConnections() []model.Connection {
	return filterConnections(d.connections.data.Active, d.connections.filter)
}

// filterConnections keeps the connections whose host, rule or route
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
