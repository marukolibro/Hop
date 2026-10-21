package handler

import (
	"time"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
)

// statsDirectAlpha quietens direct traffic under the accent of proxy traffic.
const statsDirectAlpha float32 = 0.55

// statsPage shows how much traffic Hop carried today and this month, the
// last thirty days as a chart, and this month per subscription.
func (d *Dashboard) statsPage(c *ui.Context, t *ui.Theme) {
	view := d.stats
	if trafficViewEmpty(view) {
		ui.Row(c).Grow(1).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "还没有流量经过 Hop。").FontSize(fontSecondary).TextColor(t.TextMuted)
		})
		return
	}
	ui.Scroll(c).Grow(1).Basis(0).MinHeight(0).Children(func() {
		ui.Row(c).Justify(ui.Center).Padding(scrollTrim, pagePadding, scrollEnd, pagePadding).Children(func() {
			ui.Column(c).Grow(1).MaxWidth(settingsWidth).Gap(gapSection).Children(func() {
				statsTotals(c, t, view)
				statsChartCard(c, t, view.Days)
				if len(view.Sources) > 0 {
					statsSources(c, t, view.Sources, d.subscription.view.ActiveID)
				}
			})
		})
	})
}

// recordTraffic accounts one sample to the subscription in use and refreshes
// the statistics page. It runs on the UI thread.
func (d *Dashboard) recordTraffic(conns model.Connections, now time.Time) {
	d.services.Traffic.Record(conns, d.trafficSource(), now)
	d.stats = d.services.Traffic.Summary(now)
}

// trafficSource is the subscription the core runs now, or Hop's bootstrap
// config when none does.
func (d *Dashboard) trafficSource() model.TrafficSource {
	id := d.subscription.view.ActiveID
	if id == "" {
		return model.TrafficSource{ID: service.TrafficBootstrapID}
	}
	for _, item := range d.subscription.view.Items {
		if item.ID == id {
			return model.TrafficSource{ID: id, Name: item.Name}
		}
	}
	return model.TrafficSource{ID: id}
}

// statsCard is one panel of the statistics page.
func statsCard(c *ui.Context, t *ui.Theme, build func()) {
	ui.Column(c).Padding(panelPadY, panelPadX).Radius(radiusPanel).Background(t.Surface).Border(1, t.Border).Gap(gapMedium).Children(build)
}

// statsTotals is today and this month, each with its proxy and direct split.
func statsTotals(c *ui.Context, t *ui.Theme, view model.TrafficView) {
	statsCard(c, t, func() {
		ui.Row(c).Children(func() {
			statsTotalsColumn(c, t, "今天", view.Today)
			ui.Box(c).Width(1).Margin(0, gapLarge).Background(t.Border.Alpha(cardDividerAlpha))
			statsTotalsColumn(c, t, "本月", view.Month)
		})
	})
}

func statsTotalsColumn(c *ui.Context, t *ui.Theme, label string, split model.TrafficSplit) {
	proxy, direct, total := trafficSplitTotals(split)
	ui.Column(c).Key(label).Grow(1).Basis(0).Gap(gapSmall).Children(func() {
		ui.Text(c, label).FontSize(fontSecondary).TextColor(t.TextMuted)
		statsAmount(c, t, total, fontHeadline)
		ui.Row(c).Gap(gapLarge).Children(func() {
			statsSplit(c, t, "代理", proxy, t.Accent)
			statsSplit(c, t, "直连", direct, t.TextMuted)
		})
	})
}

// statsSplit is one line of a split: a dot, its name and its amount.
func statsSplit(c *ui.Context, t *ui.Theme, label string, bytes int64, color ui.Color) {
	r := formatBytes(bytes)
	ui.Row(c).Key(label).Gap(gapTight).AlignItems(ui.Center).Children(func() {
		statsDot(c, color)
		ui.Text(c, label).FontSize(fontSecondary).TextColor(t.TextMuted)
		ui.Text(c, r.value).FontSize(fontSecondary).FontFeatures("tnum").ContentTransition(ui.ContentTransition{Numeric: true})
		ui.Text(c, r.unit).FontSize(fontCaption).TextColor(t.TextMuted)
	})
}

func statsDot(c *ui.Context, color ui.Color) {
	ui.Box(c).Width(statDot).Height(statDot).Radius(statDot / 2).Background(color)
}

// statsAmount shows an amount with its unit, its number rolling as it
// changes.
func statsAmount(c *ui.Context, t *ui.Theme, bytes int64, size float32) {
	r := formatBytes(bytes)
	ui.Row(c).Gap(4).AlignItems(ui.End).Children(func() {
		ui.Text(c, r.value).FontSize(size).FontWeight(weightSemibold).FontFeatures("tnum").ContentTransition(ui.ContentTransition{Numeric: true})
		ui.Text(c, r.unit).FontSize(fontCaption).TextColor(t.TextMuted)
	})
}

// statsChartCard is the last thirty days as one stacked bar per day, proxy
// over direct, with the hovered day's numbers.
func statsChartCard(c *ui.Context, t *ui.Theme, days []model.TrafficDayView) {
	statsCard(c, t, func() {
		ui.Row(c).Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
			sectionTitle(c, "最近 30 天")
			statsLegend(c, t)
		})
		statsBars(c, t, days)
		ui.Row(c).Justify(ui.SpaceBetween).Children(func() {
			mutedText(c, t, "30 天前")
			mutedText(c, t, "今天")
		})
	})
}

func statsLegend(c *ui.Context, t *ui.Theme) {
	ui.Row(c).Gap(gapMedium).AlignItems(ui.Center).Children(func() {
		statsLegendItem(c, t, "代理", t.Accent)
		statsLegendItem(c, t, "直连", t.TextMuted.Alpha(statsDirectAlpha))
	})
}

func statsLegendItem(c *ui.Context, t *ui.Theme, label string, color ui.Color) {
	ui.Row(c).Key(label).Gap(gapTight).AlignItems(ui.Center).Children(func() {
		statsDot(c, color)
		ui.Text(c, label).FontSize(fontCaption).TextColor(t.TextMuted)
	})
}

// statsBars draws the chart and tracks which day the pointer is over, so
// that day's numbers show.
func statsBars(c *ui.Context, t *ui.Theme, days []model.TrafficDayView) {
	bars := ui.Box(c).Height(statChartHeight).Label("最近 30 天的流量").
		Description("每天经过 Hop 的流量，代理与直连合计")
	hover := -1
	if x, _, over := bars.PointerPosition(); over && len(days) > 0 {
		if width := bars.Bounds().W; width > 0 {
			if index := int(x / (width / float32(len(days)))); index >= 0 && index < len(days) {
				hover = index
			}
		}
	}
	bars.Draw(func(p *ui.Painter, r ui.Rect) {
		drawTrafficBars(p, r, days, hover, t)
	})
}

// drawTrafficBars paints the daily chart: direct at the bottom of each bar,
// proxy stacked over it, on a hairline baseline.
func drawTrafficBars(p *ui.Painter, r ui.Rect, days []model.TrafficDayView, hover int, t *ui.Theme) {
	if len(days) == 0 || r.W <= 0 || r.H <= 0 {
		return
	}
	bottom := r.Y + r.H - 1
	plotHeight := r.H - statChartInset
	band := r.W / float32(len(days))
	barWidth := band * statBarWidthPart
	peak := int64(0)
	for _, day := range days {
		peak = max(peak, trafficSplitTotal(day.Split))
	}
	if hover >= 0 && hover < len(days) {
		p.Fill(ui.Rect{X: r.X + band*float32(hover), Y: r.Y, W: band, H: r.H}, t.SurfaceHover, radiusChip)
	}
	p.Line(r.X, bottom+0.5, r.X+r.W, bottom+0.5, 1, t.Border)
	if peak == 0 {
		return
	}
	directColor := t.TextMuted.Alpha(statsDirectAlpha)
	for i, day := range days {
		x := r.X + band*float32(i) + (band-barWidth)/2
		proxy, direct, total := trafficSplitTotals(day.Split)
		if total == 0 {
			p.Fill(ui.Rect{X: x, Y: bottom - 1, W: barWidth, H: 1}, t.Border, 1)
			continue
		}
		directHeight := float32(direct) / float32(peak) * plotHeight
		proxyHeight := float32(proxy) / float32(peak) * plotHeight
		if direct > 0 && directHeight < 1 {
			directHeight = 1
		}
		if proxy > 0 && proxyHeight < 1 {
			proxyHeight = 1
		}
		if directHeight > 0 {
			p.Fill(ui.Rect{X: x, Y: bottom - directHeight, W: barWidth, H: directHeight}, directColor, min(statBarRadius, directHeight/2))
		}
		if proxyHeight > 0 {
			p.Fill(ui.Rect{X: x, Y: bottom - directHeight - proxyHeight, W: barWidth, H: proxyHeight}, t.Accent, min(statBarRadius, proxyHeight/2))
		}
	}
	if hover >= 0 && hover < len(days) {
		drawTrafficTooltip(p, r, days[hover], hover == len(days)-1, hover, band, t)
	}
}

// drawTrafficTooltip shows the hovered day's numbers above its column.
func drawTrafficTooltip(p *ui.Painter, r ui.Rect, day model.TrafficDayView, today bool, index int, band float32, t *ui.Theme) {
	proxy, direct, _ := trafficSplitTotals(day.Split)
	title := day.Date[5:]
	if today {
		title = "今天"
	}
	lines := []struct {
		text string
		ink  ui.Color
	}{
		{title, t.TextMuted},
		{"代理 " + amountText(proxy), t.Text},
		{"直连 " + amountText(direct), t.TextMuted},
	}
	width := float32(0)
	for _, line := range lines {
		w, _ := p.MeasureText(0, ui.Span{Text: line.text, Size: fontCaption})
		width = max(width, w)
	}
	boxWidth := width + 2*statTooltipPadX
	boxHeight := float32(len(lines))*statTooltipLine + 2*statTooltipPadY
	center := r.X + band*(float32(index)+0.5)
	x := clampStatX(center-boxWidth/2, r.X, r.X+r.W-boxWidth)
	y := r.Y + statTooltipPadY
	box := ui.Rect{X: x, Y: y, W: boxWidth, H: boxHeight}
	p.Fill(box, t.Background, radiusControl)
	p.Stroke(box, t.Border, radiusControl, 1)
	for i, line := range lines {
		p.Text(x+statTooltipPadX, y+statTooltipPadY+float32(i)*statTooltipLine, line.text, fontCaption, line.ink)
	}
}

// statsSources lists this month's proxy traffic per subscription; the one in
// use is marked.
func statsSources(c *ui.Context, t *ui.Theme, sources []model.TrafficSourceView, activeID string) {
	ui.Column(c).Key("sources").Gap(gapSmall).Children(func() {
		ui.Row(c).Padding(0, gapLarge).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "本月按订阅").FontSize(fontSecondary).FontWeight(weightSemibold).TextColor(t.TextMuted)
			ui.Box(c).Grow(1)
			ui.Text(c, "代理").FontSize(fontCaption).TextColor(t.TextMuted)
		})
		ui.Column(c).Radius(radiusPanel).Background(t.Surface).Border(1, t.Border).Children(func() {
			for i, source := range sources {
				if i > 0 {
					ui.Box(c).Key(i).Height(1).Margin(0, gapLarge).Background(t.Border.Alpha(cardDividerAlpha))
				}
				statsSourceRow(c, t, source, source.ID == activeID)
			}
		})
	})
}

func statsSourceRow(c *ui.Context, t *ui.Theme, source model.TrafficSourceView, active bool) {
	proxy, _, _ := trafficSplitTotals(source.Split)
	ui.Row(c).Key(source.ID).Gap(gapMedium).AlignItems(ui.Center).MinHeight(switchHeight).
		Padding(gapMedium, gapLarge).Children(func() {
		ui.Text(c, statsSourceName(source)).FontSize(fontBody).MaxLines(1)
		if active {
			chip(c, "使用中", t.Accent)
		}
		ui.Box(c).Grow(1)
		statsDot(c, t.Accent)
		statsAmount(c, t, proxy, fontBody)
	})
}

// statsSourceName names a bucket's source: its stored name, including one
// that was deleted, or what the bucket stands for when it never had one.
func statsSourceName(source model.TrafficSourceView) string {
	if source.Name != "" {
		return source.Name
	}
	if source.ID == service.TrafficBootstrapID {
		return "内置配置"
	}
	return "已删除的订阅"
}

// trafficSplitTotals sums a split into proxy, direct and total bytes.
func trafficSplitTotals(split model.TrafficSplit) (proxy, direct, total int64) {
	proxy = split.ProxyUp + split.ProxyDown
	direct = split.DirectUp + split.DirectDown
	return proxy, direct, proxy + direct
}

func trafficSplitTotal(split model.TrafficSplit) int64 {
	_, _, total := trafficSplitTotals(split)
	return total
}

func trafficViewEmpty(view model.TrafficView) bool {
	if trafficSplitTotal(view.Today) > 0 || trafficSplitTotal(view.Month) > 0 {
		return false
	}
	for _, day := range view.Days {
		if trafficSplitTotal(day.Split) > 0 {
			return false
		}
	}
	return true
}

// amountText renders an amount with its unit, as a tooltip line.
func amountText(bytes int64) string {
	r := formatBytes(bytes)
	return r.value + " " + r.unit
}

func clampStatX(value, low, high float32) float32 {
	return min(max(value, low), high)
}
