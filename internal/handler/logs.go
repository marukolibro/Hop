package handler

import (
	"slices"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/locales"
)

const logTimeWidth float32 = 62

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

// logsPage shows the core's log as it arrives, following the newest line
// until the user scrolls away from it.
func (d *Dashboard) logsPage(c *ui.Context, t *ui.Theme) {
	visible := filterLogs(d.logs.entries, logFilters()[d.logs.level].levels)
	labels := make([]string, len(logFilters()))
	for i, f := range logFilters() {
		labels[i] = f.label
	}
	ui.Column(c).Fill().Children(func() {
		ui.Column(c).Padding(28, pageGutter(c), 26, pageGutter(c)).Children(func() {
			pageHeading(c, t, locales.LogsTitle(), locales.LogsSubtitle())
		})
		toolbar(c, func() {
			slidingSwitch(c, locales.LogsLevel(), &d.logs.level, labels, pageSwitchStyle(t, 52), func(int) {})
			mutedText(c, t, locales.LogsLines(len(visible))).FontFeatures("tnum")
			ui.Box(c).Grow(1)
			button(c, buttonSpec{label: locales.LogsClear(), style: mainSoftButton(t), disabled: len(d.logs.entries) == 0}, func() { d.logs.entries = nil })
		})
		if len(visible) == 0 {
			emptyState(c, t, locales.LogsEmpty(), locales.LogsEmptyHint())
			return
		}
		ui.Box(c).Grow(1).Padding(0, pageGutter(c), 18, pageGutter(c)).Children(func() {
			panelCard(c, t).Fill().Padding(8, 18).Children(func() {
				ui.List(c, &d.logs.list, len(visible), func(i int) {
					logRow(c, t, visible[i], i == len(visible)-1)
				}).Fill()
			})
		})
	})
}

func logRow(c *ui.Context, t *ui.Theme, entry model.LogEntry, last bool) {
	ui.Column(c).Children(func() {
		ui.Row(c).Gap(14).AlignItems(ui.Start).Padding(9, 0).Children(func() {
			captionText(c, t, entry.Time.Format("15:04:05")).FontFeatures("tnum").Width(logTimeWidth).NoWrap()
			ui.Box(c).Width(38).Children(func() {
				ui.Text(c, levelLabel(entry.Level)).FontSize(fontSecondary).TextColor(levelColor(t, entry.Level)).NoWrap()
			})
			ui.Text(c, entry.Message).FontSize(fontSecondary).Grow(1).Selectable()
		})
		if !last {
			horizontalHairline(c, t)
		}
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
		return t.Danger
	case model.LogWarning:
		return t.Warning
	default:
		return t.TextMuted
	}
}

// filterLogs keeps the entries whose level is one of levels. The input is
// not modified.
func filterLogs(entries []model.LogEntry, levels []string) []model.LogEntry {
	var out []model.LogEntry
	for _, e := range entries {
		if slices.Contains(levels, e.Level) {
			out = append(out, e)
		}
	}
	return out
}
