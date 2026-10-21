package handler

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"

	"github.com/egoist/mygo/ui"
)

// Hop's mark is a ball bouncing twice and landing: a tall arc, a short one,
// and the ball at rest. Its geometry is in units of a markSize square; the
// menu bar icon and the mark in the window both draw from it.
const (
	markSize      = 36
	markStroke    = 3.4
	markBallR     = 5.2
	markSamples   = 64
	markSupersamp = 4
	// trayLift raises the mark in the menu bar icon, in mark units: the
	// heavy ball pulls its weight low, so centred by geometry it looks
	// about a point and a half too low beside the other icons.
	trayLift = 1.5
)

// markPoint is a point of the mark, in mark units.
type markPoint struct{ x, y float64 }

// markArc is a quadratic curve from a to b bent toward c, drawn up to end
// (1 for the whole curve).
type markArc struct {
	a, c, b markPoint
	end     float64
}

var (
	// The mark fills the square: the tall arc's top is near 4 and the
	// ball's bottom near 31, so it reads at the 18 points of the menu bar.
	markArcs = []markArc{
		{a: markPoint{3, 28}, c: markPoint{10.5, -16}, b: markPoint{18, 28}, end: 1},
		{a: markPoint{18, 28}, c: markPoint{23.5, 6}, b: markPoint{28.5, 24}, end: 0.9},
	}
	markBall = markPoint{30.3, 25.6}
)

// at returns the arc's point at t.
func (arc markArc) at(t float64) markPoint {
	u := 1 - t
	return markPoint{
		x: u*u*arc.a.x + 2*u*t*arc.c.x + t*t*arc.b.x,
		y: u*u*arc.a.y + 2*u*t*arc.c.y + t*t*arc.b.y,
	}
}

// trimmed returns the arc cut at its end as a whole quadratic curve, by de
// Casteljau's split: same start, control halfway to the end's tangent.
func (arc markArc) trimmed() (markPoint, markPoint, markPoint) {
	t := arc.end
	control := markPoint{x: arc.a.x + (arc.c.x-arc.a.x)*t, y: arc.a.y + (arc.c.y-arc.a.y)*t}
	return arc.a, control, arc.at(t)
}

// trayIcon draws the mark black on transparent, for macOS to tint to the
// menu bar.
func trayIcon() ([]byte, error) {
	var out bytes.Buffer
	if err := png.Encode(&out, markImage()); err != nil {
		return nil, fmt.Errorf("encode tray icon: %w", err)
	}
	return out.Bytes(), nil
}

// markImage is the mark's pixels, black on transparent, markSize square.
func markImage() *image.NRGBA {
	segments := markSegments()
	img := image.NewNRGBA(image.Rect(0, 0, markSize, markSize))
	for y := range markSize {
		for x := range markSize {
			coverage := pixelCoverage(float64(x), float64(y), segments)
			if coverage > 0 {
				img.SetNRGBA(x, y, color.NRGBA{A: uint8(math.Round(coverage * 255))})
			}
		}
	}
	return img
}

// markSegments approximates the arcs with short straight segments.
func markSegments() [][2]markPoint {
	var segments [][2]markPoint
	for _, arc := range markArcs {
		for i := range markSamples {
			t0 := arc.end * float64(i) / markSamples
			t1 := arc.end * float64(i+1) / markSamples
			segments = append(segments, [2]markPoint{arc.at(t0), arc.at(t1)})
		}
	}
	return segments
}

// pixelCoverage is how much of the pixel with its corner at (x, y) the mark,
// raised by trayLift, covers, from a grid of samples inside it.
func pixelCoverage(x float64, y float64, segments [][2]markPoint) float64 {
	total := 0.0
	for sy := range markSupersamp {
		for sx := range markSupersamp {
			px := x + (float64(sx)+0.5)/markSupersamp
			py := y + (float64(sy)+0.5)/markSupersamp + trayLift
			total += pointCoverage(px, py, segments)
		}
	}
	return total / (markSupersamp * markSupersamp)
}

// pointCoverage is 1 inside the mark and 0 outside; strokes have round ends.
func pointCoverage(x float64, y float64, segments [][2]markPoint) float64 {
	if math.Hypot(x-markBall.x, y-markBall.y) <= markBallR {
		return 1
	}
	for _, s := range segments {
		if segmentDistance(x, y, s[0], s[1]) <= markStroke/2 {
			return 1
		}
	}
	return 0
}

func segmentDistance(x float64, y float64, a markPoint, b markPoint) float64 {
	dx, dy := b.x-a.x, b.y-a.y
	t := max(0, min(1, ((x-a.x)*dx+(y-a.y)*dy)/(dx*dx+dy*dy)))
	return math.Hypot(x-(a.x+t*dx), y-(a.y+t*dy))
}

// The speed beside the mark in the menu bar: upload over download, each
// left-aligned against the mark in a column wide enough for the longest
// compact rate, so the item keeps its width as the numbers change and a
// short rate such as "0K" does not drift away from the mark.
const (
	// traySpeedWidth is the speed column's width in points; it holds the
	// widest compact rates, such as "↑ 1023K" and "↓ 9.99M".
	traySpeedWidth = 40
	traySpeedGap   = 3
	// traySpeedFont is the speed's size in points: two lines fill the 18
	// points of the menu bar.
	traySpeedFont = 8
	// trayHeight is the menu bar icon's height in points; macOS draws it at
	// that height whatever the image's pixel size.
	trayHeight = 18
	// trayScale renders the icon for Retina menu bars.
	trayScale = 2
)

// traySpeedIcon draws the mark with the upload and download rates beside
// it, black on transparent, for macOS to tint to the menu bar.
func traySpeedIcon(up string, down string) ([]byte, error) {
	speed := ui.Render(func(c *ui.Context) {
		c.Root().Background(ui.Transparent)
		ui.Column(c).Fill().Justify(ui.Center).Children(func() {
			for _, line := range []string{"↑ " + up, "↓ " + down} {
				ui.Text(c, line).FontSize(traySpeedFont).FontWeight(weightSemibold).FontFeatures("tnum").
					LineHeight(1.08).TextColor(ui.RGBA(0, 0, 0, 1)).TextAlign(ui.Start).NoWrap()
			}
		})
	}, traySpeedWidth, trayHeight, trayScale)
	mark := markImage()
	gap := traySpeedGap * trayScale
	img := image.NewNRGBA(image.Rect(0, 0, markSize+gap+speed.Bounds().Dx(), markSize))
	draw.Draw(img, mark.Bounds(), mark, image.Point{}, draw.Src)
	draw.Draw(img, speed.Bounds().Add(image.Point{X: markSize + gap}), speed, image.Point{}, draw.Over)
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, fmt.Errorf("encode tray speed icon: %w", err)
	}
	return out.Bytes(), nil
}
