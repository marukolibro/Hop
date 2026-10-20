package handler

import "github.com/egoist/mygo/ui"

// hopTheme returns Hop's palette and control metrics for the system's
// current appearance.
//
// Accent (cobalt) marks the active route and download, Warning (amber) marks
// upload, and Success (green) marks a live connection.
func hopTheme(dark bool) *ui.Theme {
	if dark {
		t := ui.DarkTheme()
		t.Background = ui.Hex("#141821")
		t.Surface = ui.Hex("#1D2230")
		t.SurfaceHover = ui.Hex("#232A3B")
		t.SurfacePressed = ui.Hex("#2B3348")
		t.Border = ui.Hex("#2A3042")
		t.Text = ui.Hex("#E7EBF3")
		t.TextMuted = ui.Hex("#8D96AA")
		t.Accent = ui.Hex("#5B8CFF")
		t.AccentHover = ui.Hex("#7AA1FF")
		t.AccentPressed = ui.Hex("#4A78E6")
		t.AccentText = ui.Hex("#0E1220")
		t.Danger = ui.Hex("#FF6B84")
		t.Warning = ui.Hex("#F5B13A")
		t.Success = ui.Hex("#3DCB94")
		return withMetrics(*t)
	}
	t := ui.LightTheme()
	t.Background = ui.Hex("#F4F6F9")
	t.Surface = ui.Hex("#FFFFFF")
	t.SurfaceHover = ui.Hex("#E9EDF3")
	t.SurfacePressed = ui.Hex("#DEE4EE")
	t.Border = ui.Hex("#DDE2EA")
	t.Text = ui.Hex("#18202E")
	t.TextMuted = ui.Hex("#667085")
	t.Accent = ui.Hex("#2F6BFF")
	t.AccentHover = ui.Hex("#1F5AEE")
	t.AccentPressed = ui.Hex("#174BCB")
	t.AccentText = ui.Hex("#FFFFFF")
	t.Danger = ui.Hex("#D92D4A")
	t.Warning = ui.Hex("#E8940C")
	t.Success = ui.Hex("#14A06B")
	return withMetrics(*t)
}

// withMetrics sizes every control alike: one body size for buttons, inputs
// and segmented controls, a compact spacing unit, and one corner radius
// shared with the node tiles.
func withMetrics(t ui.Theme) *ui.Theme {
	t.FontSize = fontBody
	t.Spacing = controlSpacing
	t.Radius = radiusControl
	return &t
}
