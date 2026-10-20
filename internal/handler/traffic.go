package handler

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
)

const (
	sparkWidth  = 132
	sparkHeight = 40
	speedWidth  = 92
)

// readout is a throughput split into the number and its unit.
type readout struct {
	value string
	unit  string
}

// formatRate renders a throughput in bytes per second.
func formatRate(bytesPerSecond int64) readout {
	r := formatBytes(bytesPerSecond)
	return readout{value: r.value, unit: r.unit + "/s"}
}

// formatBytes renders an amount of data in binary units.
func formatBytes(bytes int64) readout {
	const unit = 1024.0
	value := float64(bytes)
	for _, suffix := range []string{"B", "KB", "MB", "GB"} {
		if value < unit {
			return readout{value: formatNumber(value), unit: suffix}
		}
		value /= unit
	}
	return readout{value: formatNumber(value), unit: "TB"}
}

// formatNumber keeps three significant digits at most.
func formatNumber(v float64) string {
	if v >= 100 {
		return fmt.Sprintf("%.0f", v)
	}
	if v >= 10 {
		return fmt.Sprintf("%.1f", v)
	}
	return fmt.Sprintf("%.2f", v)
}

// speeds shows the current download and upload speed beside a sparkline of
// the last two minutes; each colour matches its line.
func (d *Dashboard) speeds(c *ui.Context, t *ui.Theme) {
	ui.Row(c).Gap(gapLarge).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Gap(3).Width(speedWidth).Children(func() {
			speedLine(c, t, "↓", formatRate(d.last.Down), t.Accent)
			speedLine(c, t, "↑", formatRate(d.last.Up), t.Warning)
		})
		d.sparkline(c, t)
	})
}

func speedLine(c *ui.Context, t *ui.Theme, arrow string, r readout, color ui.Color) {
	ui.Row(c).Key(arrow).Gap(4).AlignItems(ui.End).Children(func() {
		ui.Text(c, arrow).FontSize(fontSecondary).FontWeight(weightBold).TextColor(color)
		ui.Text(c, r.value).FontSize(fontEmphasis).FontWeight(weightSemibold).FontFeatures("tnum").ContentTransition(ui.ContentTransition{Numeric: true})
		ui.Text(c, r.unit).FontSize(fontCaption).TextColor(t.TextMuted)
	})
}

func (d *Dashboard) sparkline(c *ui.Context, t *ui.Theme) {
	history := d.history
	down, up, base := t.Accent, t.Warning, t.Border
	ui.Box(c).Width(sparkWidth).Height(sparkHeight).Label("Traffic").Description("Download and upload speed over the last two minutes").Draw(func(p *ui.Painter, r ui.Rect) {
		p.Line(r.X, r.Y+r.H-0.5, r.X+r.W, r.Y+r.H-0.5, 1, base)
		sparkArea(p, r, history, down, up)
	})
}

// series lays values out right-aligned across the plot, as fractions of its
// width and height, so the newest sample sits at the right edge.
func series(values []float32, peak float32) (xs []float32, ys []float32) {
	offset := historyLimit - len(values)
	xs = make([]float32, len(values))
	ys = make([]float32, len(values))
	for i, v := range values {
		xs[i] = float32(offset+i) / float32(historyLimit-1)
		ys[i] = v / peak
	}
	return xs, ys
}

func peakOf(h model.TrafficHistory) float32 {
	peak := minPeak
	for _, v := range h.Up {
		peak = max(peak, v)
	}
	for _, v := range h.Down {
		peak = max(peak, v)
	}
	return peak
}
