package handler

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// dialogScrim dims the window under a dialog; dialogShadow lifts the dialog's
// panel, and the pickers' popups, off it.
var (
	dialogScrim  = ui.RGBA(0, 0, 0, 0.4)
	dialogShadow = ui.RGBA(0, 0, 0, 0.3)
	// popupShadow keeps selection menus lighter than modal dialogs.
	popupShadow = ui.RGBA(0, 0, 0, 0.14)
)

// Hop's palette is one hue family: a sky blue for shapes, a deeper sky-blue
// ink for coloured text, neutral text, and the status colours only where a
// state calls for them (a live proxy dot, a latency tier, an error).
var (
	accentLight    = ui.Hex("#237ABB")
	accentDark     = ui.Hex("#6CB8FF")
	accentInkLight = ui.Hex("#1B5C8E")
	accentInkDark  = accentDark
	// uploadLight/Dark mark upload: the ring's inner arc, the plot's upload
	// line and the ↑ arrow. Blue-slate, not orange.
	uploadLight = ui.Hex("#3A4F6B")
	uploadDark  = ui.Hex("#A9BBD6")
	// worstBackdropLight/Dark are the faintest surfaces text sits on: the
	// darkest light and the lightest dark backdrop of the design's glass.
	worstBackdropLight = ui.RGB(186, 212, 247)
	worstBackdropDark  = ui.RGB(27, 55, 100)
)

const (
	// neutralFillAlpha washes a chip in the text ink; neutralLineAlpha draws
	// a bar or gauge track. Both keep the chrome neutral, so hue means state.
	neutralFillAlpha float32 = 0.07
	neutralLineAlpha float32 = 0.08
)

// accentInk is the accent as text, where a fill's blue would be too faint.
func accentInk(t *ui.Theme) ui.Color {
	if t.Dark {
		return accentInkDark
	}
	return accentInkLight
}

// uploadColor is the upload side's ink: the ring's inner arc, the plot's
// upload line, the ↑ arrow.
func uploadColor(t *ui.Theme) ui.Color {
	if t.Dark {
		return uploadDark
	}
	return uploadLight
}

// contrastRatio is WCAG's ratio between two opaque colours.
func contrastRatio(a ui.Color, b ui.Color) float64 {
	lin := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	lum := func(c ui.Color) float64 {
		return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
	}
	la, lb := lum(a), lum(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// textOn keeps c at 4.5:1 on the worst backdrop of its appearance by mixing
// it toward the text ink; a tier colour keeps its hue and gains weight.
func textOn(t *ui.Theme, c ui.Color) ui.Color {
	bg := worstBackdropLight
	if t.Dark {
		bg = worstBackdropDark
	}
	for mix := float32(0); mix < 1; mix += 0.05 {
		out := c.Mix(t.Text, mix)
		if contrastRatio(out, bg) >= 4.5 {
			return out
		}
	}
	return t.Text
}

// hopTheme returns Hop's palette and control metrics for the system's
// current appearance.
//
// Accent marks the active route and download; upload has its own blue-slate;
// Success marks only a live proxy, TUN or core dot; Warning and Danger mark
// latency tiers, warnings and errors.
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
		t.Accent = accentDark
		t.AccentHover = t.Accent.Mix(t.Text, 0.12)
		t.AccentPressed = t.Accent.Mix(t.Text, 0.22)
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
	t.Accent = accentLight
	t.AccentHover = t.Accent.Mix(t.Text, 0.12)
	t.AccentPressed = t.Accent.Mix(t.Text, 0.22)
	t.AccentText = ui.Hex("#FFFFFF")
	t.Danger = ui.Hex("#D92D4A")
	t.Warning = ui.Hex("#E8940C")
	t.Success = ui.Hex("#14A06B")
	return withMetrics(*t)
}

// glassControlAlpha backs the flyout's toggle pills, whose text would
// otherwise sit straight on its panel material. The main window's own
// material lives in theme_glass.go.
const glassControlAlpha float32 = 0.80

// windowTheme is the main window's theme: glass when the window shows a
// material, solid otherwise. The flyout uses the selected palette through
// theme while keeping opaque control surfaces over its native panel.
func (d *Dashboard) windowTheme(c *ui.Context) *ui.Theme {
	dark := isDark(d.settings.settings.Appearance, c.Theme().Dark)
	if c.Vibrancy() {
		return hopWindowGlassTheme(dark, d.settings.settings.WindowClarity, d.settings.settings.Theme)
	}
	return hopColorTheme(dark, d.settings.settings.Theme)
}

// onGlass reports whether t is the glass palette: its control surfaces are
// translucent over the window's material. The solid palettes always have an
// opaque surface.
func onGlass(t *ui.Theme) bool { return t.Surface.A < 0xFF }

// overlaySurface is the opaque face of something that covers the window, as
// dialogs and picker popups do: a translucent surface there would show the
// page, and its text, through.
func overlaySurface(t *ui.Theme) ui.Color {
	return ui.RGB(t.Surface.R, t.Surface.G, t.Surface.B)
}

// controlBacking is the near-opaque face of a flyout toggle pill.
func controlBacking(t *ui.Theme) ui.Color {
	return ui.RGB(t.Surface.R, t.Surface.G, t.Surface.B).Alpha(glassControlAlpha)
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
