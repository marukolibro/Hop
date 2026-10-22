package handler

import (
	"github.com/egoist/mygo/ui"

	"hop/internal/model"
)

// windowMaterialPalette supplies both appearances of a named color theme.
// Its atmosphere colors are light fields behind the translucent UI regions.
type windowMaterialPalette struct {
	base   ui.Color
	ink    ui.Color
	muted  ui.Color
	accent ui.Color
	sky    ui.Color
	fold   ui.Color
	glow   ui.Color
	// sea is the default colour theme: its backdrop is the sea wallpaper, not
	// the three light fields above.
	sea  bool
	dark bool
}

// materialPalette resolves one validated theme and appearance into colors.
func materialPalette(dark bool, theme model.ColorTheme) windowMaterialPalette {
	if theme == model.ThemeDusk {
		if dark {
			return windowMaterialPalette{
				base: ui.Hex("#151D2F"), ink: ui.Hex("#F0F4FC"), muted: ui.Hex("#B7C5DC"), accent: accentDark,
				sky: ui.Hex("#625380"), fold: ui.Hex("#886482"), glow: ui.Hex("#454164"),
			}
		}
		return windowMaterialPalette{
			base: ui.Hex("#FFFFFF"), ink: ui.Hex("#1D2B43"), muted: ui.Hex("#4C5A6F"), accent: accentLight,
			sky: ui.Hex("#B9B0DF"), fold: ui.Hex("#ECC8D4"), glow: ui.Hex("#E7D7EB"),
		}
	}
	if dark {
		return windowMaterialPalette{
			base: ui.Hex("#151D2F"), ink: ui.Hex("#F0F4FC"), muted: ui.Hex("#B7C5DC"), accent: accentDark,
			sky: ui.Hex("#3C5C8D"), fold: ui.Hex("#686091"), glow: ui.Hex("#243D64"),
			sea: true, dark: true,
		}
	}
	return windowMaterialPalette{
		base: ui.Hex("#FFFFFF"), ink: ui.Hex("#1D2B43"), muted: ui.Hex("#4C5A6F"), accent: accentLight,
		sky: ui.Hex("#9FBCEF"), fold: ui.Hex("#C3C8EF"), glow: ui.Hex("#CBDCFA"),
		sea: true,
	}
}

// hopColorTheme applies the named palette to opaque windows and flyouts.
func hopColorTheme(dark bool, theme model.ColorTheme) *ui.Theme {
	t := hopTheme(dark)
	palette := materialPalette(dark, theme)
	t.Background = palette.base.Mix(palette.sky, 0.10)
	t.Surface = palette.base
	t.SurfaceHover = palette.base.Mix(palette.accent, 0.08)
	t.SurfacePressed = palette.base.Mix(palette.accent, 0.14)
	t.Text, t.TextMuted = palette.ink, palette.muted
	t.Accent = palette.accent
	t.AccentHover = palette.accent.Mix(palette.ink, 0.12)
	t.AccentPressed = palette.accent.Mix(palette.ink, 0.22)
	return t
}

// drawWindowAtmosphere paints static, soft light behind the window's regions.
// The theme supplies the reference colors; a small native desktop contribution
// remains underneath. The glass veil is painted above this backdrop, not below.
func drawWindowAtmosphere(p *ui.Painter, r ui.Rect, palette windowMaterialPalette) {
	if palette.sea {
		drawSeaWallpaper(p, r, palette.dark)
		return
	}
	p.FillGradient(r, ui.LinearGradient{From: palette.glow.Alpha(0.90), To: palette.sky.Alpha(0.90), Angle: 180}, 0)
	fold := (&ui.Path{}).
		MoveTo(r.X-r.W*0.12, r.Y+r.H*0.38).
		CubeTo(r.X+r.W*0.20, r.Y-r.H*0.02, r.X+r.W*0.49, r.Y+r.H*0.14, r.X+r.W*1.10, r.Y+r.H*1.05).
		LineTo(r.X-r.W*0.12, r.Y+r.H*1.05).Close()
	p.FillPathGradient(fold, ui.LinearGradient{From: palette.base.Alpha(0.12), To: palette.sky.Alpha(0.08), Angle: 160})
	p.FillGradient(r, ui.LinearGradient{From: palette.fold.Alpha(0), To: palette.fold.Alpha(0.32), Angle: 95}, 0)
	p.FillGradient(r, ui.LinearGradient{From: palette.base.Alpha(0.04), To: palette.base.Alpha(0), Angle: 180}, 0)
}
