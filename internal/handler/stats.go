package handler

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
	"hop/locales"
)

// statsDirectAlpha quietens direct traffic under the accent of proxy traffic.
const statsDirectAlpha float32 = 0.55

const (
	statsRange30 = iota
	statsRange7
)

const (
	statsMetricRoute = iota
	statsMetricDirection
)

const (
	statsTabApps = iota
	statsTabNodes
)

// statsPage shows today and monthly KPIs, daily average and peak, an
// interactive 7/30-day trend chart (proxy/direct or down/up), monthly usage
// per subscription, and in-memory session rankings by app and exit node.
func (d *Dashboard) statsPage(c *ui.Context, t *ui.Theme) {
	view := d.stats
	if trafficViewEmpty(view) {
		ui.Row(c).Grow(1).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
			mutedText(c, t, locales.StatsEmpty())
		})
		return
	}
	width, _ := c.Size()
	contentW := min(max(0, width-2*pagePadding), homeContentMaxWidth)
	days := statsWindowDays(view.Days, d.statsRange)

	ui.Scroll(c.Key("statsScroll")).Grow(1).Basis(0).MinHeight(0).Children(func() {
		ui.Column(c.Key("statsCenter")).AlignItems(ui.Center).
			Padding(24, pagePadding, 40, pagePadding).Children(func() {
			ui.Column(c.Key("statsBody")).Width(contentW).Gap(gapSection).Children(func() {
				pageHeadingRow(c, t, locales.StatsTitle(), locales.StatsSubtitle(), func() {
					ui.Row(c.Key("statsHeadRight")).Gap(gapMedium).AlignItems(ui.Center).Children(func() {
						captionText(c, t, locales.StatsAsOfToday()).NoWrap()
						slidingSwitch(c, "statsRange", &d.statsRange,
							[]string{locales.StatsRange30(), locales.StatsRange7()},
							pageSwitchStyle(t, 52), func(int) {})
					})
				})
				d.statsOverviewStrip(c, t, view, days, width)
				d.statsChartCard(c, t, days)
				d.statsLowerColumns(c, t, view, width)
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

// statsWindowDays slices the trailing 7 days when the 7d range is active.
func statsWindowDays(days []model.TrafficDayView, rangeIdx int) []model.TrafficDayView {
	if rangeIdx == statsRange7 && len(days) > 7 {
		return days[len(days)-7:]
	}
	return days
}

// statsWindowSummary computes aggregate totals, daily average, and peak day
// for the visible window.
type statsWindowSummary struct {
	split    model.TrafficSplit
	avgDaily int64
	peakDay  string
	peakSize int64
}

func summarizeWindow(days []model.TrafficDayView) statsWindowSummary {
	var out statsWindowSummary
	if len(days) == 0 {
		return out
	}
	var sum int64
	for _, day := range days {
		out.split.ProxyUp += day.Split.ProxyUp
		out.split.ProxyDown += day.Split.ProxyDown
		out.split.DirectUp += day.Split.DirectUp
		out.split.DirectDown += day.Split.DirectDown
		total := trafficSplitTotal(day.Split)
		sum += total
		if total >= out.peakSize && total > 0 {
			out.peakSize = total
			out.peakDay = day.Date
		}
	}
	out.avgDaily = sum / int64(len(days))
	return out
}

// statsOverviewStrip renders the 4 borderless top KPIs: Today, This Month,
// Daily Average + Peak Day, and Traffic Mix ratio bar.
func (d *Dashboard) statsOverviewStrip(c *ui.Context, t *ui.Theme, view model.TrafficView, days []model.TrafficDayView, windowWidth float32) {
	win := summarizeWindow(days)
	todayDown := view.Today.ProxyDown + view.Today.DirectDown
	todayUp := view.Today.ProxyUp + view.Today.DirectUp
	monthProxy, monthDirect, monthTotal := trafficSplitTotals(view.Month)
	todayTotal := trafficSplitTotal(view.Today)

	renderToday := func() {
		ui.Column(c.Key("kpiToday")).Grow(1).Basis(0).MinWidth(0).Gap(gapTight).Children(func() {
			mutedText(c, t, locales.StatsToday())
			statsAmount(c, t, todayTotal, fontExit)
			captionText(c, t, "↓ "+amountText(todayDown)+" · ↑ "+amountText(todayUp)).
				FontFeatures("tnum").MaxLines(1)
		})
	}
	renderMonth := func() {
		ui.Column(c.Key("kpiMonth")).Grow(1).Basis(0).MinWidth(0).Gap(gapTight).Children(func() {
			mutedText(c, t, locales.StatsThisMonth())
			statsAmount(c, t, monthTotal, fontExit)
			captionText(c, t, locales.RouteProxy()+" "+amountText(monthProxy)+" · "+locales.RouteDirect()+" "+amountText(monthDirect)).
				FontFeatures("tnum").MaxLines(1)
		})
	}
	renderAvg := func() {
		peakLabel := "—"
		if win.peakSize > 0 && len(win.peakDay) >= 10 {
			peakLabel = locales.StatsPeakDay(win.peakDay[5:], amountText(win.peakSize))
		}
		ui.Column(c.Key("kpiAvg")).Grow(1).Basis(0).MinWidth(0).Gap(gapTight).Children(func() {
			mutedText(c, t, locales.StatsDailyAvg())
			statsAmount(c, t, win.avgDaily, fontExit)
			captionText(c, t, peakLabel).FontFeatures("tnum").MaxLines(1)
		})
	}
	renderMix := func() {
		primary, secondary, total := statsMetricPair(win.split, d.statsMetric)
		pct := float32(0)
		if total > 0 {
			pct = float32(primary) / float32(total)
		}
		ratioLabel := locales.StatsProxyRatio()
		secondColor := t.TextMuted.Alpha(statsDirectAlpha)
		if d.statsMetric == statsMetricDirection {
			ratioLabel = locales.StatsDownloadRatio()
			secondColor = uploadColor(t)
		}
		ui.Column(c.Key("kpiMix")).Grow(1).Basis(0).MinWidth(0).Gap(gapTight).Children(func() {
			mutedText(c, t, locales.StatsShareTitle())
			ui.Row(c).Gap(gapTight).AlignItems(ui.End).Children(func() {
				ui.Text(c, fmt.Sprintf("%.0f%%", pct*100)).FontSize(fontExit).FontWeight(weightSemibold).
					TextColor(accentInk(t)).FontFeatures("tnum").NoWrap().
					ContentTransition(ui.ContentTransition{Numeric: true})
				captionText(c, t, ratioLabel).NoWrap()
			})
			statsRatioBar(c, t, pct, t.Accent, secondColor, secondary > 0 || primary > 0)
		})
	}

	if windowWidth >= homeTwoColMinWidth {
		ui.Row(c.Key("statsKpiRow")).Gap(gapLarge).AlignItems(ui.Stretch).Children(func() {
			renderToday()
			verticalHairline(c, t)
			renderMonth()
			verticalHairline(c, t)
			renderAvg()
			verticalHairline(c, t)
			renderMix()
		})
		return
	}
	ui.Column(c.Key("statsKpiStack")).Gap(gapLarge).Children(func() {
		ui.Row(c.Key("row1")).Gap(gapLarge).AlignItems(ui.Stretch).Children(func() {
			renderToday()
			verticalHairline(c, t)
			renderMonth()
		})
		horizontalHairline(c, t)
		ui.Row(c.Key("row2")).Gap(gapLarge).AlignItems(ui.Stretch).Children(func() {
			renderAvg()
			verticalHairline(c, t)
			renderMix()
		})
	})
}

// statsRatioBar draws a fluid full-width two-tone proportion bar.
func statsRatioBar(c *ui.Context, t *ui.Theme, ratio float32, firstColor, secondColor ui.Color, active bool) {
	box := ui.Box(c.Key("ratioBar")).Height(statRatioBarH).Margin(4, 0, 0, 0)
	anim := box.AnimateTo("ratio", ratio, ui.Snappy)
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		radius := r.H / 2
		track := t.Text.Alpha(neutralFillAlpha)
		if active {
			track = secondColor.Alpha(0.35)
		}
		p.Fill(r, track, radius)
		if active && anim > 0 {
			w := max(r.H, r.W*anim)
			if w > r.W {
				w = r.W
			}
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: w, H: r.H}, firstColor, radius)
		}
	})
}

// statsDot draws a small colored legend indicator dot.
func statsDot(c *ui.Context, color ui.Color) {
	ui.Box(c).Width(statDot).Height(statDot).Radius(statDot / 2).Background(color)
}

// statsAmount shows an amount with its unit, its number rolling as it changes.
func statsAmount(c *ui.Context, t *ui.Theme, bytes int64, size float32) {
	r := formatBytes(bytes)
	ui.Row(c).Gap(4).AlignItems(ui.End).Children(func() {
		ui.Text(c, r.value).FontSize(size).FontWeight(weightSemibold).FontFeatures("tnum").
			NoWrap().ContentTransition(ui.ContentTransition{Numeric: true})
		captionText(c, t, r.unit).NoWrap()
	})
}

// statsChartCard is the 7-day or 30-day stacked trend chart with dimension
// switcher (proxy/direct vs down/up), Y-axis scale lines, daily average guide,
// multi-tick dates, and rich hover tooltip.
func (d *Dashboard) statsChartCard(c *ui.Context, t *ui.Theme, days []model.TrafficDayView) {
	title := locales.StatsLast30()
	if d.statsRange == statsRange7 {
		title = locales.StatsLast7()
	}
	panelCard(c, t).Key("statsChartCard").Padding(20, 22).Gap(gapMedium).Radius(radiusPanel+2).
		Shadow(0, 3, 12, 0, glassCardShadow(t)).Children(func() {
		ui.Row(c.Key("chartHeader")).Wrap().Gap(gapMedium).GapY(gapSmall).AlignItems(ui.Center).Children(func() {
			ui.Text(c, title).FontSize(fontEmphasis).FontWeight(weightSemibold)
			statsLegend(c, t, d.statsMetric)
			ui.Box(c).Grow(1)
			slidingSwitch(c, "statsMetric", &d.statsMetric,
				[]string{locales.StatsModeRoute(), locales.StatsModeDirection()},
				pageSwitchStyle(t, 78), func(int) {})
		})
		d.statsBars(c, t, days)
		statsXAxis(c, t, days, d.statsRange)
	})
}

func statsLegend(c *ui.Context, t *ui.Theme, metric int) {
	ui.Row(c.Key("legend")).Gap(gapMedium).AlignItems(ui.Center).Children(func() {
		if metric == statsMetricDirection {
			statsLegendItem(c, t, locales.ColumnDownload(), t.Accent)
			statsLegendItem(c, t, locales.ColumnUpload(), uploadColor(t))
			return
		}
		statsLegendItem(c, t, locales.RouteProxy(), t.Accent)
		statsLegendItem(c, t, locales.RouteDirect(), t.TextMuted.Alpha(statsDirectAlpha))
	})
}

func statsLegendItem(c *ui.Context, t *ui.Theme, label string, color ui.Color) {
	ui.Row(c.Key(label)).Gap(gapTight).AlignItems(ui.Center).Children(func() {
		statsDot(c, color)
		captionText(c, t, label)
	})
}

// statsXAxis renders evenly spaced date ticks aligned to the plot area.
func statsXAxis(c *ui.Context, t *ui.Theme, days []model.TrafficDayView, rangeIdx int) {
	if len(days) == 0 {
		return
	}
	ui.Row(c.Key("xAxis")).Padding(0, statChartYAxisW, 0, 0).Justify(ui.SpaceBetween).Children(func() {
		if rangeIdx == statsRange7 {
			for i, day := range days {
				label := day.Date
				if len(label) >= 10 {
					label = label[5:]
				}
				if i == len(days)-1 {
					label = locales.StatsToday()
				}
				captionText(c, t, label).FontFeatures("tnum").NoWrap()
			}
			return
		}
		// 30-day view: 5 balanced ticks from 30 days ago to Today.
		indices := []int{0, len(days) / 4, len(days) / 2, len(days) * 3 / 4, len(days) - 1}
		for pos, idx := range indices {
			label := days[idx].Date
			if len(label) >= 10 {
				label = label[5:]
			}
			switch pos {
			case 0:
				label = locales.StatsThirtyAgo()
			case len(indices) - 1:
				label = locales.StatsToday()
			}
			captionText(c, t, label).FontFeatures("tnum").NoWrap()
		}
	})
}

// statsBars draws the chart and tracks which day the pointer is over, so that
// day's numbers show.
func (d *Dashboard) statsBars(c *ui.Context, t *ui.Theme, days []model.TrafficDayView) {
	metric := d.statsMetric
	bars := ui.Box(c.Key("statsBars")).Height(statChartHeight).Label(locales.StatsChartLabel()).
		Description(locales.StatsChartDescription())
	hover := -1
	if x, _, over := bars.PointerPosition(); over && len(days) > 0 {
		plotW := bars.Bounds().W - statChartYAxisW
		if plotW > 0 && x >= 0 && x < plotW {
			if index := int(x / (plotW / float32(len(days)))); index >= 0 && index < len(days) {
				hover = index
			}
		}
	}
	bars.Draw(func(p *ui.Painter, r ui.Rect) {
		drawTrafficBars(p, r, days, hover, metric, t)
	})
}

// statsMetricPair returns the primary (top) and secondary (bottom) byte counts
// for the active dimension: Proxy/Direct or Download/Upload.
func statsMetricPair(split model.TrafficSplit, metric int) (primary, secondary, total int64) {
	if metric == statsMetricDirection {
		down := split.ProxyDown + split.DirectDown
		up := split.ProxyUp + split.DirectUp
		return down, up, down + up
	}
	return trafficSplitTotals(split)
}

// drawTrafficBars paints the daily chart with right-side Y-axis scale labels,
// horizontal guide hairlines, a daily average marker, stacked rounded bars,
// and an elevated hover tooltip.
func drawTrafficBars(p *ui.Painter, r ui.Rect, days []model.TrafficDayView, hover int, metric int, t *ui.Theme) {
	if len(days) == 0 || r.W <= statChartYAxisW || r.H <= 0 {
		return
	}
	plotW := r.W - statChartYAxisW
	bottom := r.Y + r.H - 1
	plotHeight := r.H - statChartInset
	band := plotW / float32(len(days))
	barWidth := min(band*statBarWidthPart, statBarMaxWidth)

	var peak, sum int64
	for _, day := range days {
		total := trafficSplitTotal(day.Split)
		peak = max(peak, total)
		sum += total
	}
	avg := sum / int64(len(days))

	// Subtle horizontal guide lines + right-side Y-axis scale labels.
	gridColor := t.Border.Alpha(0.65)
	p.Line(r.X, bottom+0.5, r.X+plotW, bottom+0.5, 1, t.Border)
	if peak > 0 {
		midY := bottom - plotHeight*0.5
		topY := bottom - plotHeight
		p.Line(r.X, midY, r.X+plotW, midY, 1, gridColor)
		p.Line(r.X, topY, r.X+plotW, topY, 1, gridColor)

		labelX := r.X + plotW + 8
		p.Text(labelX, topY-5, amountText(peak), fontCaption, t.TextMuted)
		p.Text(labelX, midY-5, amountText(peak/2), fontCaption, t.TextMuted)
		p.Text(labelX, bottom-10, "0", fontCaption, t.TextMuted)
	}

	if hover >= 0 && hover < len(days) {
		fill := t.SurfaceHover
		if onGlass(t) {
			fill = glassFill(t, glassChartHover)
		}
		p.Fill(ui.Rect{X: r.X + band*float32(hover), Y: r.Y, W: band, H: r.H}, fill, radiusChip)
	}
	if peak == 0 {
		return
	}

	secondColor := t.TextMuted.Alpha(statsDirectAlpha)
	if metric == statsMetricDirection {
		secondColor = uploadColor(t)
	}

	for i, day := range days {
		x := r.X + band*float32(i) + (band-barWidth)/2
		primary, secondary, total := statsMetricPair(day.Split, metric)
		if total == 0 {
			p.Fill(ui.Rect{X: x, Y: bottom - 1, W: barWidth, H: 1}, t.Border, 1)
			continue
		}
		secH := float32(secondary) / float32(peak) * plotHeight
		priH := float32(primary) / float32(peak) * plotHeight
		if secondary > 0 && secH < 1.5 {
			secH = 1.5
		}
		if primary > 0 && priH < 1.5 {
			priH = 1.5
		}
		if secH > 0 {
			p.Fill(ui.Rect{X: x, Y: bottom - secH, W: barWidth, H: secH}, secondColor, min(statBarRadius, secH/2))
		}
		if priH > 0 {
			p.Fill(ui.Rect{X: x, Y: bottom - secH - priH, W: barWidth, H: priH}, t.Accent, min(statBarRadius, priH/2))
		}
	}

	// Quiet daily average guide line across the bars.
	if avg > 0 {
		avgY := bottom - float32(avg)/float32(peak)*plotHeight
		dashW := float32(4)
		gapW := float32(4)
		for dx := r.X; dx < r.X+plotW; dx += dashW + gapW {
			endX := min(dx+dashW, r.X+plotW)
			p.Line(dx, avgY, endX, avgY, 1, t.Accent.Alpha(0.45))
		}
		tag := locales.StatsAvgTag()
		tw, _ := p.MeasureText(0, ui.Span{Text: tag, Size: fontCaption})
		pill := ui.Rect{X: r.X + 2, Y: avgY - 15, W: tw + 8, H: 14}
		p.Fill(pill, overlaySurface(t).Alpha(0.88), 4)
		p.Text(pill.X+4, pill.Y+1, tag, fontCaption, accentInk(t))
	}

	if hover >= 0 && hover < len(days) {
		drawTrafficTooltip(p, ui.Rect{X: r.X, Y: r.Y, W: plotW, H: r.H}, days[hover], hover == len(days)-1, hover, band, metric, t)
	}
}

// drawTrafficTooltip shows the hovered day's total and dimension breakdown.
func drawTrafficTooltip(p *ui.Painter, r ui.Rect, day model.TrafficDayView, today bool, index int, band float32, metric int, t *ui.Theme) {
	primary, secondary, total := statsMetricPair(day.Split, metric)
	title := day.Date
	if len(title) >= 10 {
		title = title[5:]
	}
	if today {
		title = locales.StatsToday()
	}
	firstLine := locales.StatsProxyValue(amountText(primary))
	secondLine := locales.StatsDirectValue(amountText(secondary))
	if metric == statsMetricDirection {
		firstLine = locales.StatsDownValue(amountText(primary))
		secondLine = locales.StatsUpValue(amountText(secondary))
	}
	lines := []struct {
		text string
		ink  ui.Color
	}{
		{title + " · " + locales.StatsTotalValue(amountText(total)), t.Text},
		{firstLine, accentInk(t)},
		{secondLine, t.TextMuted},
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
	p.Shadow(box, radiusControl, 0, 4, 14, 0, dialogShadow)
	p.Fill(box, overlaySurface(t), radiusControl)
	p.Stroke(box, t.Border, radiusControl, 1)
	for i, line := range lines {
		p.Text(x+statTooltipPadX, y+statTooltipPadY+float32(i)*statTooltipLine, line.text, fontCaption, line.ink)
	}
}

// statsLowerColumns renders the two-column lower breakdown matching the Home
// shelf's borderless proportions: monthly subscriptions on the left, in-memory
// session rankings (apps / nodes) on the right.
func (d *Dashboard) statsLowerColumns(c *ui.Context, t *ui.Theme, view model.TrafficView, windowWidth float32) {
	if windowWidth >= homeTwoColMinWidth {
		ui.Row(c.Key("statsLower")).Gap(homeShelfGap).AlignItems(ui.Start).Children(func() {
			ui.Column(c.Key("statsSourcesCol")).Grow(1).Basis(0).MinWidth(0).Gap(gapMedium).Children(func() {
				statsSources(c, t, view.Sources, d.subscription.view.ActiveID)
			})
			ui.Column(c.Key("statsSessionCol")).Grow(1).Basis(0).MinWidth(0).Gap(gapMedium).Children(func() {
				d.statsSessionRankings(c, t, view)
			})
		})
		return
	}
	ui.Column(c.Key("statsLowerStacked")).Gap(gapSection).Children(func() {
		statsSources(c, t, view.Sources, d.subscription.view.ActiveID)
		horizontalHairline(c, t)
		d.statsSessionRankings(c, t, view)
	})
}

// statsSources lists this month's traffic per subscription with proportional
// share bars and upload/download detail.
func statsSources(c *ui.Context, t *ui.Theme, sources []model.TrafficSourceView, activeID string) {
	ui.Column(c.Key("sources")).Gap(gapSmall).Children(func() {
		ui.Row(c).Height(24).AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.StatsBySubscription()).FontSize(fontSecondary).FontWeight(weightSemibold).TextColor(t.TextMuted)
			ui.Box(c).Grow(1)
			captionText(c, t, locales.RouteProxy())
		})
		if len(sources) == 0 {
			mutedText(c, t, locales.StatsEmpty())
			return
		}
		var sumProxy int64
		for _, s := range sources {
			p, _, _ := trafficSplitTotals(s.Split)
			sumProxy += p
		}
		ui.Column(c).Gap(6).Children(func() {
			for _, source := range sources {
				statsSourceRow(c, t, source, source.ID == activeID, sumProxy)
			}
		})
	})
}

func statsSourceRow(c *ui.Context, t *ui.Theme, source model.TrafficSourceView, active bool, sumProxy int64) {
	proxy, direct, _ := trafficSplitTotals(source.Split)
	ratio := float32(0)
	if sumProxy > 0 {
		ratio = float32(proxy) / float32(sumProxy)
	}
	row := ui.Column(c.Key("src-"+source.ID)).Gap(5).Padding(homeShelfRowPadY, homeShelfRowPadX).Radius(homeShelfRowRadius)
	if active {
		row.Background(t.Accent.Alpha(0.09))
	}
	row.Children(func() {
		ui.Row(c.Key("top")).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
			weight := weightMedium
			if active {
				weight = weightSemibold
			}
			ui.Text(c, statsSourceName(source)).FontSize(fontBody).FontWeight(weight).MaxLines(1)
			if active {
				badge(c, t, locales.BadgeInUse())
			}
			ui.Box(c).Grow(1)
			statsAmount(c, t, proxy, fontSecondary)
			if sumProxy > 0 {
				captionText(c, t, fmt.Sprintf("%.0f%%", ratio*100)).FontFeatures("tnum").NoWrap()
			}
		})
		statsRankBar(c, t, ratio, 1, false)
		meta := "↓ " + amountText(source.Split.ProxyDown) + " · ↑ " + amountText(source.Split.ProxyUp)
		if direct > 0 {
			meta += " · " + locales.RouteDirect() + " " + amountText(direct)
		}
		captionText(c, t, meta).FontFeatures("tnum").MaxLines(1)
	})
}

// statsSessionRankings shows the in-memory top apps or exit nodes for the
// current run, with borderless micro-tabs matching the Home shelf.
func (d *Dashboard) statsSessionRankings(c *ui.Context, t *ui.Theme, view model.TrafficView) {
	ui.Column(c.Key("sessionRank")).Gap(gapSmall).Children(func() {
		ui.Row(c.Key("sessionHead")).Height(24).Gap(gapLarge).AlignItems(ui.Center).Children(func() {
			shelfMicroTab(c, t, "tabApps", locales.StatsTabApps(), d.statsSessionTab == statsTabApps, func() {
				d.statsSessionTab = statsTabApps
			})
			shelfMicroTab(c, t, "tabNodes", locales.StatsTabNodes(), d.statsSessionTab == statsTabNodes, func() {
				d.statsSessionTab = statsTabNodes
			})
			ui.Box(c).Grow(1)
			captionText(c, t, locales.StatsSessionScope()).NoWrap()
		})
		entries := view.SessionApps
		isNodes := false
		if d.statsSessionTab == statsTabNodes {
			entries = view.SessionNodes
			isNodes = true
		}
		if len(entries) == 0 {
			ui.Box(c.Key("sessionEmpty")).Padding(gapMedium, 0).Children(func() {
				mutedText(c, t, locales.StatsSessionEmpty())
			})
			return
		}
		var totalAll int64
		for _, e := range entries {
			totalAll += trafficSplitTotal(e.Split)
		}
		limit := min(len(entries), statSessionPreview)
		ui.Column(c.Key("sessionList")).Gap(6).Children(func() {
			for i, entry := range entries[:limit] {
				statsSessionRow(c, t, i, entry, isNodes, totalAll)
			}
		})
	})
}

func statsSessionRow(c *ui.Context, t *ui.Theme, idx int, entry model.TrafficEntryView, isNodes bool, totalAll int64) {
	proxy, _, total := trafficSplitTotals(entry.Split)
	share := float32(0)
	if totalAll > 0 {
		share = float32(total) / float32(totalAll)
	}
	proxyPart := float32(1)
	if total > 0 {
		proxyPart = float32(proxy) / float32(total)
	}
	label := entry.Name
	switch {
	case isNodes:
		label = routeLabel([]string{entry.Name})
	case label == "":
		label = locales.StatsOtherProcess()
	}
	ui.Column(c.Key(fmt.Sprintf("rank-%d-%s", idx, entry.Name))).Gap(5).Padding(6, 4).Children(func() {
		ui.Row(c.Key("top")).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
			ui.Text(c, label).FontSize(fontSecondary).FontWeight(weightMedium).MaxLines(1).Grow(1).Basis(0)
			statsAmount(c, t, total, fontSecondary)
			captionText(c, t, fmt.Sprintf("%.0f%%", share*100)).FontFeatures("tnum").NoWrap()
		})
		statsRankBar(c, t, share, proxyPart, entry.Direct)
	})
}

// statsRankBar paints a slim proportional progress bar: filled to share of the
// total width, split between accent (proxy) and muted (direct).
func statsRankBar(c *ui.Context, t *ui.Theme, share, proxyPart float32, allDirect bool) {
	ui.Box(c.Key("bar")).Height(statRankBarH).Draw(func(p *ui.Painter, r ui.Rect) {
		radius := r.H / 2
		p.Fill(r, t.Text.Alpha(neutralFillAlpha), radius)
		if share <= 0 {
			return
		}
		fillW := max(r.H, r.W*min(1, share))
		directColor := t.TextMuted.Alpha(statsDirectAlpha)
		if allDirect {
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: fillW, H: r.H}, directColor, radius)
			return
		}
		p.Fill(ui.Rect{X: r.X, Y: r.Y, W: fillW, H: r.H}, directColor, radius)
		if proxyPart > 0 {
			pw := max(r.H, fillW*min(1, proxyPart))
			if pw > fillW {
				pw = fillW
			}
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: pw, H: r.H}, t.Accent, radius)
		}
	})
}

// statsSourceName names a bucket's source: its stored name, including one that
// was deleted, or what the bucket stands for when it never had one.
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

// amountText renders an amount with its unit, as a tooltip or subtitle value.
func amountText(bytes int64) string {
	r := formatBytes(bytes)
	return r.value + " " + r.unit
}

func clampStatX(value, low, high float32) float32 {
	return min(max(value, low), high)
}
