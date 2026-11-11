package handler

import (
	"embed"
	"fmt"

	"github.com/egoist/mygo/ui"
)

// hackFonts is JetBrains Mono (SIL Open Font License, fonts/OFL.txt), the face
// of the Hack theme. It travels with the app so macOS and Windows draw the
// same glyphs; the Chinese text falls back to the system's face.
//
//go:embed fonts/*.ttf
var hackFonts embed.FS

// hackFontFiles are the weights the design system uses: 400 for the body and
// 500, 600 and 700 for emphasis.
var hackFontFiles = []string{"Regular", "Medium", "SemiBold", "Bold"}

// RegisterFonts adds the fonts the app brings to the text engine. Call it once
// before the first window opens.
func RegisterFonts() error {
	for _, weight := range hackFontFiles {
		data, err := hackFonts.ReadFile("fonts/JetBrainsMono-" + weight + ".ttf")
		if err != nil {
			return fmt.Errorf("read font JetBrains Mono %s: %w", weight, err)
		}
		if err := ui.RegisterFont(data, hackFont); err != nil {
			return fmt.Errorf("register font JetBrains Mono %s: %w", weight, err)
		}
	}
	return nil
}
