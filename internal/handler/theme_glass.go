package handler

import (
	"github.com/egoist/mygo/ui"

	"hop/internal/model"
)

// The main window's glass, derived from the design's material spec. Keep
// every number here: these alphas and inks are the look.
//
// The theme provides a stable colored backdrop above native vibrancy; the
// veil is painted above it, keeping all text and controls sharp.
const (
	glassHeaderAlpha       float32 = 0.16
	glassHeroAlpha         float32 = 0.10
	glassSubscriptionAlpha float32 = 0.18
	glassSidebarAlpha      float32 = 0.18
	glassNodeAreaAlpha     float32 = 0.04
	glassPanelAlpha        float32 = 0.44
	glassButtonAlpha       float32 = 0.38
	glassTrackAlpha        float32 = 0.18
	glassTrackSelected     float32 = 0.69
	glassSidebarActive     float32 = 0.48
	glassTileAlpha         float32 = 0.58
	glassTileHover         float32 = 0.70
	glassTileSelected      float32 = 0.67
	glassTileDarkAlpha     float32 = 0.32
	glassTileDarkHover     float32 = 0.34
	glassTileDarkSelected  float32 = 0.30
	glassMaterialLineLight float32 = 0.68
	glassMaterialLineDark  float32 = 0.17
	glassHairlineLight     float32 = 0.11
	glassHairlineDark      float32 = 0.09
	glassStrongLineLight   float32 = 0.19
	glassStrongLineDark    float32 = 0.20
	glassAccentLine        float32 = 0.62
	glassControlShadow     float32 = 0.025
	glassButtonHover       float32 = 0.46
	glassSidebarHover      float32 = 0.30
	glassChartHover        float32 = 0.25
	glassDangerFill        float32 = 0.08
	glassDangerHover       float32 = 0.12
	glassNoticeDanger      float32 = 0.10
	glassToggleOnTint      float32 = 0.10
	glassBadgeTint         float32 = 0.08
)

var (
	glassSuccessLight = ui.Hex("#13654D")
	glassWarningLight = ui.Hex("#755220")
	glassDangerLight  = ui.Hex("#C14D59")
	glassSuccessDark  = ui.Hex("#94E1C1")
	glassWarningDark  = ui.Hex("#D7B47C")
	glassDangerDark   = ui.Hex("#FFE0E6")
)

// hopWindowGlassTheme is the main window's palette at one clarity. It is
// separate from the flyout's opaque control surfaces.
func hopWindowGlassTheme(dark bool, clarity int, colorTheme model.ColorTheme) *ui.Theme {
	t := hopColorTheme(dark, colorTheme)
	palette := materialPalette(dark, colorTheme)
	alpha := glassClarityAlpha(clarity)
	if dark {
		t.Background = palette.base
		t.Surface = palette.base.Alpha(alpha)
		t.SurfaceHover = t.Surface.Mix(palette.ink, 0.04)
		t.SurfacePressed = t.Surface.Mix(palette.ink, 0.07)
		t.Border = ui.RGBA(225, 236, 255, glassHairlineDark)
		t.Text = palette.ink
		t.TextMuted = palette.muted
		t.Accent = palette.accent
		t.AccentHover = t.Accent.Mix(t.Text, 0.12)
		t.AccentPressed = t.Accent.Mix(t.Text, 0.22)
		t.AccentText = ui.Hex("#0E1220")
		t.Danger = glassDangerDark
		t.Warning = glassWarningDark
		t.Success = glassSuccessDark
		return t
	}
	t.Background = palette.base
	t.Surface = palette.base.Alpha(alpha)
	t.SurfaceHover = t.Surface.Mix(palette.ink, 0.04)
	t.SurfacePressed = t.Surface.Mix(palette.ink, 0.07)
	t.Border = ui.RGBA(70, 95, 135, glassHairlineLight)
	t.Text = palette.ink
	t.TextMuted = palette.muted
	t.Accent = palette.accent
	t.AccentHover = t.Accent.Mix(t.Text, 0.12)
	t.AccentPressed = t.Accent.Mix(t.Text, 0.22)
	t.AccentText = ui.Hex("#FFFFFF")
	t.Danger = glassDangerLight
	t.Warning = glassWarningLight
	t.Success = glassSuccessLight
	return t
}

// glassClarityAlpha turns the setting into the base's opacity.
func glassClarityAlpha(clarity int) float32 {
	if clarity < model.WindowClarityMin || clarity > model.WindowClarityMax {
		clarity = model.DefaultWindowClarity
	}
	return 1 - float32(clarity)/100
}

// glassBaseColor is the window's base layer at the chosen clarity.
func glassBaseColor(t *ui.Theme) ui.Color {
	return t.Surface
}

// windowGlass distinguishes the main window's material from the flyout's
// opaque flyout palette. The main palette's
// hairline carries the design's ink; the flyout's is white.
func windowGlass(t *ui.Theme) bool {
	if !onGlass(t) {
		return false
	}
	if t.Dark {
		return t.Border.R == 225 && t.Border.G == 236 && t.Border.B == 255
	}
	return t.Border.R == 70 && t.Border.G == 95 && t.Border.B == 135
}

// glassFill is a tonal veil of the window's own colour; solid windows use
// their opaque surface instead.
func glassFill(t *ui.Theme, alpha float32) ui.Color {
	if !onGlass(t) {
		return t.Surface
	}
	return ui.RGB(t.Surface.R, t.Surface.G, t.Surface.B).Alpha(alpha)
}

// glassTile is a node tile's face in its resting, hovered or selected state.
func glassTile(t *ui.Theme, hovered bool, selected bool) ui.Color {
	if !onGlass(t) {
		if hovered && !selected {
			return t.SurfaceHover
		}
		return t.Surface
	}
	if t.Dark {
		switch {
		case selected:
			return overlaySurface(t).Mix(t.Accent, 0.25).Alpha(glassTileDarkSelected)
		case hovered:
			return overlaySurface(t).Mix(t.TextMuted, 0.22).Alpha(glassTileDarkHover)
		default:
			return glassFill(t, glassTileDarkAlpha)
		}
	}
	switch {
	case selected:
		return glassFill(t, glassTileSelected)
	case hovered:
		return glassFill(t, glassTileHover)
	default:
		return glassFill(t, glassTileAlpha)
	}
}

// The window's regions use the material's tonal veils on glass and the
// opaque surface/background pair on solid windows.
func headerFill(t *ui.Theme) ui.Color {
	if onGlass(t) {
		return glassFill(t, glassHeaderAlpha)
	}
	return t.Surface
}

func heroFill(t *ui.Theme) ui.Color {
	if onGlass(t) {
		return glassFill(t, glassHeroAlpha)
	}
	return t.Surface
}

func subscriptionFill(t *ui.Theme) ui.Color {
	if onGlass(t) {
		return glassFill(t, glassSubscriptionAlpha)
	}
	return t.Surface
}

func sidebarFill(t *ui.Theme) ui.Color {
	if onGlass(t) {
		return glassFill(t, glassSidebarAlpha)
	}
	return t.Background
}

func nodeAreaFill(t *ui.Theme) ui.Color {
	if onGlass(t) {
		return glassFill(t, glassNodeAreaAlpha)
	}
	return t.Background
}

// glassPanel is the face of a list, chart or table card.
func glassPanel(t *ui.Theme) ui.Color {
	return glassFill(t, glassPanelAlpha)
}

// glassButton is the face of a button or header toggle.
func glassButton(t *ui.Theme) ui.Color {
	return glassFill(t, glassButtonAlpha)
}

// glassMaterialLine draws the bright edge that catches the material.
func glassMaterialLine(t *ui.Theme) ui.Color {
	if !onGlass(t) {
		return t.Border
	}
	if t.Dark {
		return ui.RGBA(255, 255, 255, glassMaterialLineDark)
	}
	return ui.RGBA(255, 255, 255, glassMaterialLineLight)
}

// glassStrongLine is the route track and the separators inside a control.
func glassStrongLine(t *ui.Theme) ui.Color {
	if !onGlass(t) {
		return t.Border
	}
	if t.Dark {
		return ui.RGBA(225, 236, 255, glassStrongLineDark)
	}
	return ui.RGBA(70, 95, 135, glassStrongLineLight)
}

// glassCardShadow is the soft lift under cards and tiles; solid windows do
// not need it.
func glassCardShadow(t *ui.Theme) ui.Color {
	if !onGlass(t) {
		return ui.Transparent
	}
	if t.Dark {
		return ui.RGBA(0, 0, 0, 0.16)
	}
	return ui.RGBA(30, 48, 80, 0.025)
}

// glassButtonShadow is the faint lift under a main-window button.
func glassButtonShadow(t *ui.Theme) ui.Color {
	if !onGlass(t) {
		return ui.Transparent
	}
	return ui.RGBA(30, 48, 80, 0.025)
}

// glassSelectedBorder is the accent edge of the chosen node.
func glassSelectedBorder(t *ui.Theme) ui.Color {
	return t.Accent.Alpha(glassAccentLine)
}

// themedWindowMaterial paints the wallpaper and the glass veil in one pass,
// below the content's text and hit targets.
type themedWindowMaterial struct {
	palette windowMaterialPalette
	veil    ui.Color
}

func (m themedWindowMaterial) PaintMaterial(p *ui.Painter, r ui.Rect, radii [4]float32) {
	drawWallpaper(p, r, m.palette)
	p.Fill(r, m.veil, radii[0])
}
