package handler

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/locales"
)

// The hero's plot: sparkCaptionH is the top strip that holds the caption.
// The plot takes the width of the column it sits in.
const sparkCaptionH float32 = 14

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

// sparkline is the last two minutes of traffic, height tall and as wide as
// its column. The newest sample sits at the right edge; a window without
// real traffic keeps its baseline and says so.
func (d *Dashboard) sparkline(c *ui.Context, t *ui.Theme, height float32) {
	history := d.history
	down, up, base := t.Accent, uploadColor(t), t.Border
	idle := historyPeak(history) < idleRate
	ui.Row(c).Key("sparkSlot").Height(height).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Key("spark").Grow(1).Basis(0).Height(height).
			Label("Traffic").Description("Download and upload speed over the last two minutes").Draw(func(p *ui.Painter, r ui.Rect) {
			p.Line(r.X, r.Y+r.H-0.5, r.X+r.W, r.Y+r.H-0.5, 1, base)
			if idle {
				return
			}
			plot := ui.Rect{X: r.X, Y: r.Y + sparkCaptionH, W: r.W, H: r.H - sparkCaptionH}
			sparkAreaLive(p, plot, history, down, up)
		}).Children(func() {
			ui.Column(c).Fill().Children(func() {
				ui.Row(c).Height(sparkCaptionH).AlignItems(ui.Center).Children(func() {
					captionText(c, t, locales.TrafficLast2Min())
				})
				if idle {
					ui.Row(c).Grow(1).Basis(0).Center().Children(func() {
						mutedText(c, t, locales.TrafficNone())
					})
				}
			})
		})
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

// historyPeak is the largest sample in the window, 0 when it has none. A
// peak below idleRate means no real traffic, so the plot says so instead of
// drawing a flat line.
func historyPeak(h model.TrafficHistory) float32 {
	peak := float32(0)
	for _, v := range h.Up {
		peak = max(peak, v)
	}
	for _, v := range h.Down {
		peak = max(peak, v)
	}
	return peak
}

func peakOf(h model.TrafficHistory) float32 {
	return max(minPeak, historyPeak(h))
}
