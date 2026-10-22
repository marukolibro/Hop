package handler

import (
	"fmt"
	"slices"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
)

const logTimeWidth float32 = 62

// logFilters are the level filter's segments and the levels each shows.
var logFilters = []struct {
	label  string
	levels []string
}{
	{"全部", []string{model.LogDebug, model.LogInfo, model.LogWarning, model.LogError}},
	{"警告", []string{model.LogWarning, model.LogError}},
	{"错误", []string{model.LogError}},
}

// logsPage shows the core's log as it arrives, following the newest line
// until the user scrolls away from it.
func (d *Dashboard) logsPage(c *ui.Context, t *ui.Theme) {
	visible := filterLogs(d.logs.entries, logFilters[d.logs.level].levels)
	labels := make([]string, len(logFilters))
	for i, f := range logFilters {
		labels[i] = f.label
	}
	ui.Column(c).Fill().Children(func() {
		ui.Column(c).Padding(28, pageGutter(c), 26, pageGutter(c)).Children(func() {
			pageHeading(c, t, "运行日志", "连接与规则的运行记录")
		})
		toolbar(c, func() {
			slidingSwitch(c, "日志级别", &d.logs.level, labels, pageSwitchStyle(t, 52), func(int) {})
			mutedText(c, t, fmt.Sprintf("%d 行", len(visible))).FontFeatures("tnum")
			ui.Box(c).Grow(1)
			button(c, buttonSpec{label: "清空日志", style: mainSoftButton(t), disabled: len(d.logs.entries) == 0}, func() { d.logs.entries = nil })
		})
		if len(visible) == 0 {
			emptyState(c, t, "暂无日志", "内核的运行记录会实时出现在这里,比如连接匹配了哪条规则、节点连接失败的原因。")
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
		return "错误"
	case model.LogWarning:
		return "警告"
	case model.LogDebug:
		return "调试"
	default:
		return "信息"
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
