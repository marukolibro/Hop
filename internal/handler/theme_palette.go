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
	sea bool
	// paper is the flat, grained paper theme: its backdrop is one plain sheet.
	paper bool
	// hack is the Hack theme: a baked backdrop of the base and a faint glow.
	hack bool
	dark bool
}

// materialPalette resolves one validated theme and appearance into colors.
func materialPalette(dark bool, theme model.ColorTheme) windowMaterialPalette {
	switch theme {
	case model.ThemeHack:
		return windowMaterialPalette{
			base: hackBase, ink: hackInk, muted: hackMuted, accent: hackAccent,
			sky: hackSurface, fold: hackSurface, glow: hackSurface,
			hack: true, dark: true,
		}
	case model.ThemePaper:
		// Neutral grey sheet, near-black ink and one blue accent. The greys carry
		// no hue, so every region of the window reads as the same paper. sky is
		// the slightly deeper tone of the sidebar and the node area.
		if dark {
			return windowMaterialPalette{
				base: ui.Hex("#161616"), ink: ui.Hex("#EDEDED"), muted: ui.Hex("#A0A0A0"), accent: ui.Hex("#7AA2FF"),
				sky: ui.Hex("#2C2C2C"), fold: ui.Hex("#1D1D1D"), glow: ui.Hex("#1D1D1D"),
				paper: true, dark: true,
			}
		}
		return windowMaterialPalette{
			base: ui.Hex("#F4F4F4"), ink: ui.Hex("#111111"), muted: ui.Hex("#666666"), accent: ui.Hex("#447DFC"),
			sky: ui.Hex("#DDDDDD"), fold: ui.Hex("#EDEDED"), glow: ui.Hex("#EDEDED"),
			paper: true,
		}
	case model.ThemeDusk:
		if dark {
			return windowMaterialPalette{
				base: ui.Hex("#151D2F"), ink: ui.Hex("#F0F4FC"), muted: ui.Hex("#B7C5DC"), accent: accentDark,
				sky: ui.Hex("#625380"), fold: ui.Hex("#886482"), glow: ui.Hex("#454164"),
				dark: true,
			}
		}
		return windowMaterialPalette{
			base: ui.Hex("#FFFFFF"), ink: ui.Hex("#1D2B43"), muted: ui.Hex("#4C5A6F"), accent: accentLight,
			sky: ui.Hex("#B9B0DF"), fold: ui.Hex("#ECC8D4"), glow: ui.Hex("#E7D7EB"),
		}
	case model.ThemeForest:
		if dark {
			return windowMaterialPalette{
				base: ui.Hex("#111F1B"), ink: ui.Hex("#EDF7F3"), muted: ui.Hex("#A7C2B8"), accent: ui.Hex("#5DD9AC"),
				sky: ui.Hex("#2A5B4B"), fold: ui.Hex("#445B3F"), glow: ui.Hex("#1B3E35"),
				dark: true,
			}
		}
		return windowMaterialPalette{
			base: ui.Hex("#FFFFFF"), ink: ui.Hex("#172B24"), muted: ui.Hex("#496359"), accent: ui.Hex("#18795B"),
			sky: ui.Hex("#A6D9C4"), fold: ui.Hex("#CFE5C2"), glow: ui.Hex("#D4EEE6"),
		}
	case model.ThemeAmber:
		if dark {
			return windowMaterialPalette{
				base: ui.Hex("#221814"), ink: ui.Hex("#FAF2EC"), muted: ui.Hex("#CEB8AB"), accent: ui.Hex("#F6A868"),
				sky: ui.Hex("#7C4B30"), fold: ui.Hex("#7E454D"), glow: ui.Hex("#4B2E22"),
				dark: true,
			}
		}
		return windowMaterialPalette{
			base: ui.Hex("#FFFFFF"), ink: ui.Hex("#2E1F16"), muted: ui.Hex("#6C5649"), accent: ui.Hex("#C05A24"),
			sky: ui.Hex("#F2C9A0"), fold: ui.Hex("#F0B8B0"), glow: ui.Hex("#F9E3C6"),
		}
	case model.ThemeRose:
		if dark {
			return windowMaterialPalette{
				base: ui.Hex("#21151D"), ink: ui.Hex("#FAF0F5"), muted: ui.Hex("#CBB2C0"), accent: ui.Hex("#F48FB6"),
				sky: ui.Hex("#6F3E58"), fold: ui.Hex("#5A3E6B"), glow: ui.Hex("#412336"),
				dark: true,
			}
		}
		return windowMaterialPalette{
			base: ui.Hex("#FFFFFF"), ink: ui.Hex("#2E1A24"), muted: ui.Hex("#6E505E"), accent: ui.Hex("#B8426E"),
			sky: ui.Hex("#ECC0D3"), fold: ui.Hex("#DEC4E8"), glow: ui.Hex("#F8DFEA"),
		}
	case model.ThemeSlate:
		if dark {
			return windowMaterialPalette{
				base: ui.Hex("#14161A"), ink: ui.Hex("#F1F3F6"), muted: ui.Hex("#9BA3B0"), accent: ui.Hex("#B8C4D6"),
				sky: ui.Hex("#3D434F"), fold: ui.Hex("#4A4F5A"), glow: ui.Hex("#272B33"),
				dark: true,
			}
		}
		return windowMaterialPalette{
			base: ui.Hex("#FFFFFF"), ink: ui.Hex("#181B20"), muted: ui.Hex("#58606E"), accent: ui.Hex("#3F4B5C"),
			sky: ui.Hex("#C8CFD9"), fold: ui.Hex("#D9DDE3"), glow: ui.Hex("#E6E9EE"),
		}
	default:
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
}

// themeSwatchColors returns the two-stop gradient colors for a theme's preview
// orb in the settings picker.
func themeSwatchColors(theme model.ColorTheme) (ui.Color, ui.Color) {
	switch theme {
	case model.ThemeDusk:
		return ui.Hex("#B9B0DF"), ui.Hex("#886482")
	case model.ThemeForest:
		return ui.Hex("#5DD9AC"), ui.Hex("#2A5B4B")
	case model.ThemeAmber:
		return ui.Hex("#F6A868"), ui.Hex("#7E454D")
	case model.ThemeRose:
		return ui.Hex("#F48FB6"), ui.Hex("#6F3E58")
	case model.ThemeSlate:
		return ui.Hex("#C8CFD9"), ui.Hex("#3D434F")
	case model.ThemePaper:
		return ui.Hex("#F4F4F4"), ui.Hex("#9CA3AF")
	case model.ThemeHack:
		return hackAccent, ui.Hex("#0A1A0C")
	default:
		return ui.Hex("#6CB8FF"), ui.Hex("#3C5C8D")
	}
}

// hopColorTheme applies the named palette to opaque windows and flyouts.
func hopColorTheme(dark bool, theme model.ColorTheme) *ui.Theme {
	if theme == model.ThemeHack {
		return hopHackTheme()
	}
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
