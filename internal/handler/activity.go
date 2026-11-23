package handler

import (
	"strconv"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/locales"
)

// The activity page's three views.
const (
	activityConnections = iota
	activityLog
	activityRules
)

// activityLabels name the activity page's views, index for index.
func activityLabels() []string {
	return []string{locales.PageConnections(), locales.PageLogs(), locales.PageRules()}
}

// activityPage is what is happening now: live connections, the core's log, or
// active routing rules and diagnostics, framed in the same centered optical
// column and borderless summary language as the Home and Statistics pages.
func (d *Dashboard) activityPage(c *ui.Context, t *ui.Theme) {
	width, _ := c.Size()
	contentW := min(max(0, width-2*pagePadding), homeContentMaxWidth)
	ui.Column(c.Key("activityPage")).Grow(1).Basis(0).MinHeight(0).AlignItems(ui.Center).
		Padding(24, pagePadding, 20, pagePadding).Children(func() {
		ui.Column(c.Key("activityBody")).Width(contentW).Grow(1).Basis(0).MinHeight(0).Gap(gapLarge).Children(func() {
			pageHeadingRow(c, t, locales.PageActivity(), locales.ActivitySubtitle(), func() {
				slidingSwitch(c, locales.ActivityViewLabel(), &d.activity, activityLabels(), pageSwitchStyle(t, 60), func(i int) {
					if i == activityRules {
						d.ensureRuleInspectLoaded()
					}
				})
			})
			switch d.activity {
			case activityLog:
				d.logsSummaryStrip(c, t)
				d.logsView(c, t, contentW)
			case activityRules:
				d.rulesView(c, t, contentW)
			default:
				d.connectionsSummaryStrip(c, t)
				d.connectionsView(c, t, contentW)
			}
		})
	})
}

// connectionsSummaryStrip shows live connection counts, real-time throughput,
// and active connection transfer totals in the same borderless KPI language as
// the Statistics overview strip.
func (d *Dashboard) connectionsSummaryStrip(c *ui.Context, t *ui.Theme) {
	conns := d.connections.data.Active
	var proxyCount, directCount int
	var sumDown, sumUp int64
	for _, conn := range conns {
		sumDown += conn.Download
		sumUp += conn.Upload
		if isDirectChain(conn.Chain) {
			directCount++
		} else {
			proxyCount++
		}
	}
	down := formatRate(d.last.Down)
	up := formatRate(d.last.Up)

	ui.Row(c.Key("connKpiStrip")).Gap(gapLarge).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c.Key("kpiActive")).Grow(1).Basis(0).MinWidth(0).Gap(gapLine).Children(func() {
			mutedText(c, t, locales.ActivityActiveConns())
			ui.Text(c, strconv.Itoa(len(conns))).FontSize(fontExit).FontWeight(weightSemibold).
				FontFeatures("tnum").NoWrap().ContentTransition(ui.ContentTransition{Numeric: true})
			captionText(c, t, locales.RouteProxy()+" "+strconv.Itoa(proxyCount)+" · "+locales.RouteDirect()+" "+strconv.Itoa(directCount)).
				FontFeatures("tnum").MaxLines(1)
		})
		verticalHairline(c, t)
		ui.Column(c.Key("kpiRate")).Grow(1).Basis(0).MinWidth(0).Gap(gapLine).Children(func() {
			mutedText(c, t, locales.ActivityLiveRate())
			ui.Row(c).Gap(4).AlignItems(ui.End).Children(func() {
				ui.Text(c, "↓").FontSize(fontBody).FontWeight(weightSemibold).TextColor(accentInk(t))
				ui.Text(c, down.value).FontSize(fontExit).FontWeight(weightSemibold).FontFeatures("tnum").
					NoWrap().ContentTransition(ui.ContentTransition{Numeric: true})
				captionText(c, t, down.unit).NoWrap()
			})
			captionText(c, t, "↑ "+up.value+" "+up.unit).FontFeatures("tnum").MaxLines(1)
		})
		verticalHairline(c, t)
		ui.Column(c.Key("kpiTransfer")).Grow(1).Basis(0).MinWidth(0).Gap(gapLine).Children(func() {
			mutedText(c, t, locales.ActivityConnTraffic())
			statsAmount(c, t, sumDown+sumUp, fontExit)
			captionText(c, t, "↓ "+amountText(sumDown)+" · ↑ "+amountText(sumUp)).FontFeatures("tnum").MaxLines(1)
		})
	})
}

// logsSummaryStrip shows recorded log counts, warnings, and errors at a glance.
func (d *Dashboard) logsSummaryStrip(c *ui.Context, t *ui.Theme) {
	var warnings, errors int
	for _, entry := range d.logs.entries {
		switch entry.Level {
		case model.LogWarning:
			warnings++
		case model.LogError:
			errors++
		}
	}
	warnInk := t.Text
	if warnings > 0 {
		warnInk = textOn(t, t.Warning)
	}
	errInk := t.Text
	if errors > 0 {
		errInk = textOn(t, t.Danger)
	}
	ui.Row(c.Key("logKpiStrip")).Gap(gapLarge).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c.Key("kpiLogged")).Grow(1).Basis(0).MinWidth(0).Gap(gapLine).Children(func() {
			mutedText(c, t, locales.ActivityLoggedLines())
			ui.Text(c, strconv.Itoa(len(d.logs.entries))).FontSize(fontExit).FontWeight(weightSemibold).
				FontFeatures("tnum").NoWrap().ContentTransition(ui.ContentTransition{Numeric: true})
		})
		verticalHairline(c, t)
		ui.Column(c.Key("kpiWarn")).Grow(1).Basis(0).MinWidth(0).Gap(gapLine).Children(func() {
			mutedText(c, t, locales.LogLevelWarning())
			ui.Text(c, strconv.Itoa(warnings)).FontSize(fontExit).FontWeight(weightSemibold).
				TextColor(warnInk).FontFeatures("tnum").NoWrap().ContentTransition(ui.ContentTransition{Numeric: true})
		})
		verticalHairline(c, t)
		ui.Column(c.Key("kpiErr")).Grow(1).Basis(0).MinWidth(0).Gap(gapLine).Children(func() {
			mutedText(c, t, locales.LogLevelError())
			ui.Text(c, strconv.Itoa(errors)).FontSize(fontExit).FontWeight(weightSemibold).
				TextColor(errInk).FontFeatures("tnum").NoWrap().ContentTransition(ui.ContentTransition{Numeric: true})
		})
	})
}

// isDirectChain reports whether a connection's exit chain goes directly out
// without a proxy node.
func isDirectChain(chain []string) bool {
	return len(chain) > 0 && chain[len(chain)-1] == "DIRECT"
}
