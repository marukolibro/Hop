package handler

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
)

// markInk colours Hop's mark from left to right; a solid ink has from and
// to alike.
type markInk struct {
	from ui.Color
	to   ui.Color
}

// brandInk is Hop's gradient, the accent into sky blue, a step brighter in
// the dark appearance; a theme with its own accent gets a solid mark.
func brandInk(t *ui.Theme) markInk {
	if !hopBlueAccent(t) {
		return solidInk(t.Accent)
	}
	if t.Dark {
		return markInk{from: accentDark, to: ui.Hex("#67D3FB")}
	}
	return markInk{from: accentLight, to: ui.Hex("#22B0EE")}
}

// solidInk is a single colour, for the mark where it marks rather than
// brands, such as a group's icon.
func solidInk(c ui.Color) markInk {
	return markInk{from: c, to: c}
}

// horizontalGradient fills what it paints from From at the left of its box
// to To at the right; colours arrive unpremultiplied, as Options.Uniforms
// passes a ui.Color.
var horizontalGradient = shader.MustCompile(`package main

var From vec4
var To vec4

func Fragment(dst vec4, pos vec2) vec4 {
	c := mix(From, To, clamp(pos.x/rectSize().x, 0, 1))
	return vec4(c.rgb*c.a, c.a)
}
`)

const wordmarkSize float32 = 19

// wordmark is "Hop" in the brand gradient.
func wordmark(c *ui.Context, ink markInk) {
	span := ui.Span{Text: "Hop", Size: wordmarkSize, Weight: weightBold, LetterSpacing: -0.3}
	w, h := c.MeasureText(0, span)
	ui.Box(c).Width(w + 1).Height(h).Label("Hop").Draw(func(p *ui.Painter, r ui.Rect) {
		horizontalGradient.Fill(p, r, shader.Options{Uniforms: map[string]any{"From": ink.from, "To": ink.to}}, func() {
			p.RichText(r.X, r.Y, r.W, span)
		})
	})
}

// brandLockup is the mark beside the wordmark, as the window and the panel
// show Hop's name.
func brandLockup(c *ui.Context, t *ui.Theme, markSizeDIP float32, hop markHop) {
	ink := brandInk(t)
	ui.Row(c).Key("brand").Gap(gapSmall).AlignItems(ui.Center).Children(func() {
		hopMark(c, markSizeDIP, ink, hop)
		wordmark(c, ink)
	})
}

// brandMark is the mark alone, for a header too narrow to hold the wordmark
// beside the page switch.
func brandMark(c *ui.Context, t *ui.Theme, markSizeDIP float32, hop markHop) {
	ui.Row(c).Key("brand").AlignItems(ui.Center).Children(func() {
		hopMark(c, markSizeDIP, brandInk(t), hop)
	})
}
