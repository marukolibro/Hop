package handler

import (
	"fmt"
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
