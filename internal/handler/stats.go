package handler

import (
	"time"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
	"hop/locales"
)

// statsDirectAlpha quietens direct traffic under the accent of proxy traffic.
const statsDirectAlpha float32 = 0.55

// statsPage shows how much traffic Hop carried today and this month, the
// last thirty days as a chart, and this month per subscription.
func (d *Dashboard) statsPage(c *ui.Context, t *ui.Theme) {
	view := d.stats
	if trafficViewEmpty(view) {
		ui.Row(c).Grow(1).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
			mutedText(c, t, locales.StatsEmpty())
		})
		return
	}
	// Like the connections and log pages, the statistics fill the window and
	// start at the page gutter, so the heading stays where the other pages put it.
	ui.Scroll(c).Grow(1).Basis(0).MinHeight(0).Children(func() {
		ui.Column(c).Padding(28, pageGutter(c), 40, pageGutter(c)).Gap(20).Children(func() {
			pageHeadingRow(c, t, locales.StatsTitle(), locales.StatsSubtitle(), func() {
				captionText(c, t, locales.StatsAsOfToday())
			})
			statsTotals(c, t, view)
			if d.memoryKnown {
				statsMemoryCard(c, t, d.memory)
			}
			horizontalHairline(c, t)
			statsChartCard(c, t, view.Days)
			if len(view.Sources) > 0 {
				statsSources(c, t, view.Sources, d.subscription.view.ActiveID)
			}
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

// statsTotals is today and this month, each with its proxy and direct split.
func statsTotals(c *ui.Context, t *ui.Theme, view model.TrafficView) {
	ui.Row(c).Key("totals").Gap(24).Children(func() {
		statsTotalsColumn(c, t, locales.StatsToday(), view.Today)
		statsTotalsColumn(c, t, locales.StatsThisMonth(), view.Month)
	})
}

func statsTotalsColumn(c *ui.Context, t *ui.Theme, label string, split model.TrafficSplit) {
	proxy, direct, total := trafficSplitTotals(split)
	ui.Column(c).Key(label).Grow(1).Basis(0).Gap(gapSmall).Children(func() {
		mutedText(c, t, label)
		statsAmount(c, t, total, fontStat)
		ui.Row(c).Gap(gapLarge).Children(func() {
			statsSplit(c, t, locales.RouteProxy(), proxy, t.Accent)
			statsSplit(c, t, locales.RouteDirect(), direct, t.TextMuted)
		})
	})
}

// statsSplit is one line of a split: a dot, its name and its amount.
func statsSplit(c *ui.Context, t *ui.Theme, label string, bytes int64, color ui.Color) {
	r := formatBytes(bytes)
	ui.Row(c).Key(label).Gap(gapTight).AlignItems(ui.Center).Children(func() {
		statsDot(c, color)
		mutedText(c, t, label)
		ui.Text(c, r.value).FontSize(fontSecondary).FontFeatures("tnum").ContentTransition(ui.ContentTransition{Numeric: true})
		captionText(c, t, r.unit)
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
		captionText(c, t, r.unit)
	})
}

// statsChartCard is the last thirty days as one stacked bar per day, proxy
// over direct, with the hovered day's numbers.
func statsChartCard(c *ui.Context, t *ui.Theme, days []model.TrafficDayView) {
	panelCard(c, t).Padding(22, 24).Gap(16).Children(func() {
		ui.Row(c).Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.StatsLast30()).FontSize(fontEmphasis).FontWeight(weightMedium)
			statsLegend(c, t)
		})
		statsBars(c, t, days)
		ui.Row(c).Justify(ui.SpaceBetween).Children(func() {
			mutedText(c, t, locales.StatsThirtyAgo())
			mutedText(c, t, locales.StatsToday())
		})
	})
}

func statsLegend(c *ui.Context, t *ui.Theme) {
	ui.Row(c).Gap(gapMedium).AlignItems(ui.Center).Children(func() {
		statsLegendItem(c, t, locales.RouteProxy(), t.Accent)
		statsLegendItem(c, t, locales.RouteDirect(), t.TextMuted.Alpha(statsDirectAlpha))
	})
}

func statsLegendItem(c *ui.Context, t *ui.Theme, label string, color ui.Color) {
	ui.Row(c).Key(label).Gap(gapTight).AlignItems(ui.Center).Children(func() {
		statsDot(c, color)
		captionText(c, t, label)
	})
}

// statsBars draws the chart and tracks which day the pointer is over, so
// that day's numbers show.
func statsBars(c *ui.Context, t *ui.Theme, days []model.TrafficDayView) {
	bars := ui.Box(c).Height(statChartHeight).Label(locales.StatsChartLabel()).
		Description(locales.StatsChartDescription())
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
		fill := t.SurfaceHover
		if onGlass(t) {
			fill = glassFill(t, glassChartHover)
		}
		p.Fill(ui.Rect{X: r.X + band*float32(hover), Y: r.Y, W: band, H: r.H}, fill, radiusChip)
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
		title = locales.StatsToday()
	}
	lines := []struct {
		text string
		ink  ui.Color
	}{
		{title, t.TextMuted},
		{locales.StatsProxyValue(amountText(proxy)), t.Text},
		{locales.StatsDirectValue(amountText(direct)), t.TextMuted},
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
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.StatsBySubscription()).FontSize(fontSecondary).FontWeight(weightSemibold).TextColor(t.TextMuted)
			ui.Box(c).Grow(1)
			captionText(c, t, locales.RouteProxy())
		})
		ui.Column(c).Children(func() {
			for i, source := range sources {
				if i > 0 {
					horizontalHairline(c, t)
				}
				statsSourceRow(c, t, source, source.ID == activeID)
			}
		})
	})
}

func statsSourceRow(c *ui.Context, t *ui.Theme, source model.TrafficSourceView, active bool) {
	proxy, _, _ := trafficSplitTotals(source.Split)
	ui.Row(c).Key(source.ID).Gap(gapMedium).AlignItems(ui.Center).MinHeight(switchHeight).
		Padding(gapMedium, 0).Children(func() {
		ui.Text(c, statsSourceName(source)).FontSize(fontBody).MaxLines(1)
		if active {
			badge(c, t, locales.BadgeInUse())
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
		return locales.StatsBuiltInConfig()
	}
	return locales.StatsDeletedSubscription()
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

// statsMemoryCard is the physical memory Hop and its core hold now: the sum,
// and a bar split between the two with each one's amount.
func statsMemoryCard(c *ui.Context, t *ui.Theme, m model.Memory) {
	total := m.App + m.Core
	panelCard(c, t).Key("memory").Padding(18, 24).Gap(12).Children(func() {
		ui.Row(c).Justify(ui.SpaceBetween).AlignItems(ui.End).Children(func() {
			ui.Column(c).Gap(gapTight).Children(func() {
				ui.Text(c, locales.StatsMemory()).FontSize(fontEmphasis).FontWeight(weightMedium)
				statsAmount(c, t, int64(total), fontHeadline)
			})
			ui.Row(c).Gap(gapLarge).Children(func() {
				statsSplit(c, t, "Hop", int64(m.App), t.Accent)
				statsSplit(c, t, locales.StatsMemoryCore(), int64(m.Core), t.TextMuted.Alpha(statsDirectAlpha))
			})
		})
		share := float32(0)
		if total > 0 {
			share = float32(m.App) / float32(total)
		}
		ui.Box(c).Key("memoryBar").Height(statMemoryBar).Label(locales.StatsMemory()).Draw(func(p *ui.Painter, r ui.Rect) {
			drawMemoryBar(p, r, share, t)
		})
	})
}

// drawMemoryBar fills the bar with Hop's share in the accent and the core's
// after it, with a small gap between the two.
func drawMemoryBar(p *ui.Painter, r ui.Rect, share float32, t *ui.Theme) {
	radius := r.H / 2
	split := r.W * share
	core := t.TextMuted.Alpha(statsDirectAlpha)
	switch {
	case split <= statMemoryGap:
		p.Fill(r, core, radius)
	case split >= r.W-statMemoryGap:
		p.Fill(r, t.Accent, radius)
	default:
		p.Fill(ui.Rect{X: r.X, Y: r.Y, W: split - statMemoryGap/2, H: r.H}, t.Accent, radius)
		p.Fill(ui.Rect{X: r.X + split + statMemoryGap/2, Y: r.Y, W: r.W - split - statMemoryGap/2, H: r.H}, core, radius)
	}
}
