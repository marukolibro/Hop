package handler

import (
	"fmt"
	"slices"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
)

const logTimeWidth float32 = 64

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
	toolbar(c, func() {
		slidingSwitch(c, "日志级别", &d.logs.level, labels, pageSwitchStyle(t, 52), func(int) {})
		mutedText(c, t, fmt.Sprintf("%d 行", len(visible))).FontFeatures("tnum")
		ui.Box(c).Grow(1)
		button(c, buttonSpec{label: "清空", icon: iconTrash, style: softButton(t), disabled: len(d.logs.entries) == 0}, func() { d.logs.entries = nil })
	})
	if len(visible) == 0 {
		emptyState(c, t, "暂无日志", "内核的运行记录会实时出现在这里,比如连接匹配了哪条规则、节点连接失败的原因。")
		return
	}
	ui.Box(c).Grow(1).Margin(0, pagePadding, gapLarge, pagePadding).Radius(radiusControl).Border(1, t.Border).Background(t.Surface).Clip().Children(func() {
		ui.List(c, &d.logs.list, len(visible), func(i int) {
			logRow(c, t, visible[i])
		}).Fill().Padding(gapTight, 0)
	})
}

func logRow(c *ui.Context, t *ui.Theme, entry model.LogEntry) {
	ui.Row(c).Gap(gapMedium).AlignItems(ui.Start).Padding(5, gapMedium).Children(func() {
		ui.Text(c, entry.Time.Format("15:04:05")).FontSize(fontCaption).TextColor(t.TextMuted).FontFeatures("tnum").Width(logTimeWidth).NoWrap()
		ui.Box(c).Width(44).Children(func() {
			chip(c, levelLabel(entry.Level), levelColor(t, entry.Level))
		})
		ui.Text(c, entry.Message).FontSize(fontSecondary).Grow(1).Selectable()
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
