package handler

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/locales"
)

const logTimeWidth float32 = 60

// logFilters are the level filter's segments and the levels each shows.
func logFilters() []logFilter {
	return []logFilter{
		{locales.All(), []string{model.LogDebug, model.LogInfo, model.LogWarning, model.LogError}},
		{locales.LogLevelWarning(), []string{model.LogWarning, model.LogError}},
		{locales.LogLevelError(), []string{model.LogError}},
	}
}

// logFilter is one segment of the level filter.
type logFilter struct {
	label  string
	levels []string
}

// logsView shows the core's log as it arrives, following the newest line until
// the user scrolls away from it.
func (d *Dashboard) logsView(c *ui.Context, t *ui.Theme, contentW float32) {
	labels := make([]string, len(logFilters()))
	for i, f := range logFilters() {
		labels[i] = f.label
	}
	ui.Column(c.Key("log")).Grow(1).Basis(0).MinHeight(0).Gap(gapMedium).Children(func() {
		ui.Row(c.Key("logToolbar")).Wrap().Gap(gapSmall).GapY(gapSmall).AlignItems(ui.Center).Children(func() {
			slidingSwitch(c, locales.LogsLevel(), &d.logs.level, labels, pageSwitchStyle(t, 50), func(int) {})
			ui.TextInput(c.Key("logSearch"), &d.logs.filter).
				Placeholder(locales.LogsFilter()).Width(min(220, max(140, contentW*0.26)))
			visibleCount := len(filterLogs(d.logs.entries, logFilters()[d.logs.level].levels, d.logs.filter))
			captionText(c, t, locales.LogsLines(visibleCount)).FontFeatures("tnum").NoWrap()
			ui.Box(c).Grow(1)
			button(c, buttonSpec{label: locales.LogsOpenFolder(), style: mainSoftButton(t)}, d.openLogFolder)
			button(c, buttonSpec{
				label:    locales.LogsClear(),
				style:    mainSoftButton(t),
				disabled: len(d.logs.entries) == 0,
			}, func() { d.logs.entries = nil })
		})
		visible := filterLogs(d.logs.entries, logFilters()[d.logs.level].levels, d.logs.filter)
		if len(visible) == 0 {
			emptyState(c, t, locales.LogsEmpty(), locales.LogsEmptyHint())
			return
		}
		panelCard(c, t).Key("logListCard").Grow(1).Basis(0).MinHeight(0).
			Padding(8, 18).Radius(radiusPanel+2).Shadow(0, 3, 12, 0, glassCardShadow(t)).Children(func() {
			ui.List(c, &d.logs.list, len(visible), func(i int) {
				logRow(c, t, visible[i], i == len(visible)-1)
			}).Fill()
		})
	})
}

// openLogFolder shows the folder of the log files, which outlive the window.
func (d *Dashboard) openLogFolder() {
	if err := d.services.Folders.OpenPath(d.services.LogDir); err != nil {
		d.fail(actionOpenLogFolder, err)
	}
}

func logRow(c *ui.Context, t *ui.Theme, entry model.LogEntry, last bool) {
	ui.Column(c).Children(func() {
		ui.Row(c).Gap(gapMedium).AlignItems(ui.Center).Padding(9, 0).Children(func() {
			ui.Text(c, entry.Time.Format("15:04:05")).Font("monospace").FontSize(fontCaption).
				TextColor(t.TextMuted).Width(logTimeWidth).NoWrap()
			logLevelPill(c, t, entry.Level)
			ui.Text(c, entry.Message).FontSize(fontSecondary).Grow(1).Basis(0).Selectable()
		})
		if !last {
			horizontalHairline(c, t)
		}
	})
}

// logLevelPill renders a compact tinted pill for a log entry's severity.
func logLevelPill(c *ui.Context, t *ui.Theme, level string) {
	ink := levelColor(t, level)
	fill := t.Text.Alpha(neutralFillAlpha)
	switch level {
	case model.LogError:
		fill = t.Danger.Alpha(dangerFillAlpha)
	case model.LogWarning:
		fill = t.Warning.Alpha(0.14)
	}
	ui.Box(c).MinWidth(42).Padding(2, 7).Radius(radiusChip).Center().Background(fill).Children(func() {
		ui.Text(c, levelLabel(level)).FontSize(fontCaption).FontWeight(weightMedium).TextColor(ink).
			NoWrap().Translate(0, -opticalLift)
	})
}

func levelLabel(level string) string {
	switch level {
	case model.LogError:
		return locales.LogLevelError()
	case model.LogWarning:
		return locales.LogLevelWarning()
	case model.LogDebug:
		return locales.LogLevelDebug()
	default:
		return locales.LogLevelInfo()
	}
}

func levelColor(t *ui.Theme, level string) ui.Color {
	switch level {
	case model.LogError:
		return textOn(t, t.Danger)
	case model.LogWarning:
		return textOn(t, t.Warning)
	default:
		return t.TextMuted
	}
}

// filterLogs keeps the entries matching both levels and the optional search
// query (case-insensitive). The input is not modified.
func filterLogs(entries []model.LogEntry, levels []string, query string) []model.LogEntry {
	needle := strings.ToLower(strings.TrimSpace(query))
	var out []model.LogEntry
	for _, e := range entries {
		if !slices.Contains(levels, e.Level) {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(e.Message), needle) {
			continue
		}
		out = append(out, e)
	}
	return out
}
