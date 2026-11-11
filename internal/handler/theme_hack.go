package handler

import (
	"image"
	"image/color"
	"math"

	"github.com/egoist/mygo/ui"
)

// The Hack theme is one fixed dark look: a terminal's phosphor green on black,
// in a monospaced face, a pale mint for upload. It has no light or dark variant
// and never uses the system's window material, so macOS and Windows draw the
// same pixels: the backdrop is baked here, the regions are plain veils of the
// base over it, and the flyout is opaque.
var (
	hackBase    = ui.Hex("#010502")
	hackSurface = ui.Hex("#04100A")
	hackAccent  = ui.Hex("#00FF66")
	hackUpload  = ui.Hex("#B8FFCF")
	hackInk     = ui.Hex("#CFFFDC")
	hackMuted   = ui.Hex("#3FA060")
	hackLine    = ui.Hex("#145A2C")
	hackDanger  = ui.Hex("#FF4A3D")
	hackWarning = ui.Hex("#D7FF3C")
)

const (
	// hackFont is the theme's face (fonts.go): every glyph the same width, as on a
	// terminal.
	hackFont = "JetBrains Mono"
	// hackHeaderVeil, hackSidebarVeil and hackNodeVeil are how much of the
	// base covers the backdrop in each region.
	hackHeaderVeil  float32 = 0.86
	hackSidebarVeil float32 = 0.82
	hackNodeVeil    float32 = 0.60
	// hackGlow is the strength of the green light at the window's top left.
	hackGlow float32 = 0.10
	// hackHeroTint tints the hero card with the accent.
	hackHeroTint float32 = 0.10
	// hackTextGlow and hackTextGlowAlpha are the light around every text.
	hackTextGlow      float32 = 8
	hackTextGlowAlpha float32 = 0.45
	// hackScanStep is the scanlines' period in points and hackScanAlpha
	// how dark a line is; hackVignette darkens the window's corners.
	hackScanStep  float32 = 3
	hackScanAlpha float32 = 0.22
	hackVignette  float32 = 0.45
)

// hackTheme says whether t is the Hack palette.
func hackTheme(t *ui.Theme) bool { return t.Accent == hackAccent }

// hopHackTheme is the Hack palette, whatever the system's appearance.
func hopHackTheme() *ui.Theme {
	t := ui.DarkTheme()
	t.Background = hackBase
	t.Surface = hackSurface
	t.SurfaceHover = hackSurface.Mix(hackLine, 0.55)
	t.SurfacePressed = hackSurface.Mix(hackLine, 0.9)
	t.Border = hackLine
	t.Text = hackInk
	t.TextMuted = hackMuted
	t.Accent = hackAccent
	t.AccentHover = hackAccent.Mix(hackInk, 0.18)
	t.AccentPressed = hackAccent.Mix(hackInk, 0.32)
	t.AccentText = ui.Hex("#001A08")
	t.Danger = hackDanger
	t.Warning = hackWarning
	t.Success = hackAccent
	t.Font = hackFont
	return withMetrics(*t)
}

// hackImage bakes the backdrop for a window of w by h points: the base with a
// faint green light at the top left, one pixel per seaCell points.
func hackImage(w float32, h float32) *image.RGBA {
	nx, ny := max(2, int(math.Ceil(float64(w/seaCell)))), max(2, int(math.Ceil(float64(h/seaCell))))
	img := image.NewRGBA(image.Rect(0, 0, nx, ny))
	reach := max(w, h) * 0.8
	to8 := func(v float32) uint8 { return uint8(min(1, max(0, v))*255 + 0.5) }
	for j := range ny {
		for i := range nx {
			x, y := (float32(i)+0.5)/float32(nx)*w, (float32(j)+0.5)/float32(ny)*h
			falloff := max(0, 1-float32(math.Hypot(float64(x), float64(y)))/reach)
			px := duskColor(hackAccent, hackGlow*falloff*falloff).over(duskColor(hackBase, 1))
			img.SetRGBA(i, j, color.RGBA{R: to8(px.r), G: to8(px.g), B: to8(px.b), A: 255})
		}
	}
	return img
}

// drawHack paints the backdrop behind the window rect r.
func drawHack(p *ui.Painter, r ui.Rect, palette windowMaterialPalette) {
	p.Image(wallpaperBitmap(r.W, r.H, palette), r, ui.FillBox)
}

// hackScanlines lays a CRT's scanlines and darkened corners over the whole
// window. The pointer goes through them.
func hackScanlines(c *ui.Context) {
	ui.Box(c.Key("hackScanlines")).Absolute().Left(0).Top(0).Right(0).Bottom(0).PassThrough().IgnoreInput(true).
		AccessHidden(true).RadialGradient(ui.RadialGradient{
		Stops:  []ui.GradientStop{{Color: ui.Transparent, At: 0.6}, {Color: ui.RGBA(0, 0, 0, hackVignette), At: 1}},
		Center: ui.Point{X: 0.5, Y: 0.45},
	}).Draw(func(p *ui.Painter, r ui.Rect) {
		line := ui.RGBA(0, 0, 0, hackScanAlpha)
		for y := r.Y; y < r.Y+r.H; y += hackScanStep {
			p.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: hairlineWidth}, line, 0)
		}
	})
}
