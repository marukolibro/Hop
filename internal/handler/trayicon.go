package handler

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"time"

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
	return encodeTrayIcon(markImage(arcsImage(), 0))
}

// encodeTrayIcon is img as the PNG the menu bar takes.
func encodeTrayIcon(img image.Image) ([]byte, error) {
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, fmt.Errorf("encode tray icon: %w", err)
	}
	return out.Bytes(), nil
}

// markImage is the mark's pixels, black on transparent, markSize square: arcs
// with the ball over them, raised by ballLift mark units (0 at rest). The
// arcs do not change, so a hop draws them once and moves only the ball.
func markImage(arcs *image.NRGBA, ballLift float64) *image.NRGBA {
	ball := markPoint{x: markBall.x, y: markBall.y - ballLift}
	img := image.NewNRGBA(arcs.Bounds())
	copy(img.Pix, arcs.Pix)
	for y := range markSize {
		for x := range markSize {
			coverage := ballCoverage(float64(x), float64(y), ball)
			if coverage == 0 {
				continue
			}
			alpha := max(img.NRGBAAt(x, y).A, uint8(math.Round(coverage*255)))
			img.SetNRGBA(x, y, color.NRGBA{A: alpha})
		}
	}
	return img
}

// arcsImage is the mark's two arcs, black on transparent, markSize square.
func arcsImage() *image.NRGBA {
	segments := markSegments()
	img := image.NewNRGBA(image.Rect(0, 0, markSize, markSize))
	for y := range markSize {
		for x := range markSize {
			coverage := pixelCoverage(float64(x), float64(y), func(px float64, py float64) bool {
				return onArcs(px, py, segments)
			})
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

// pixelCoverage is how much of the pixel with its corner at (x, y) the shape
// inside covers, raised by trayLift, from a grid of samples inside the pixel.
func pixelCoverage(x float64, y float64, inside func(x float64, y float64) bool) float64 {
	hits := 0
	for sy := range markSupersamp {
		for sx := range markSupersamp {
			px := x + (float64(sx)+0.5)/markSupersamp
			py := y + (float64(sy)+0.5)/markSupersamp + trayLift
			if inside(px, py) {
				hits++
			}
		}
	}
	return float64(hits) / (markSupersamp * markSupersamp)
}

// ballCoverage is how much of the pixel at (x, y) a ball centred on ball
// covers.
func ballCoverage(x float64, y float64, ball markPoint) float64 {
	return pixelCoverage(x, y, func(px float64, py float64) bool {
		return math.Hypot(px-ball.x, py-ball.y) <= markBallR
	})
}

// onArcs says whether a point lies on the arcs; strokes have round ends.
func onArcs(x float64, y float64, segments [][2]markPoint) bool {
	for _, s := range segments {
		if segmentDistance(x, y, s[0], s[1]) <= markStroke/2 {
			return true
		}
	}
	return false
}

func segmentDistance(x float64, y float64, a markPoint, b markPoint) float64 {
	dx, dy := b.x-a.x, b.y-a.y
	t := max(0, min(1, ((x-a.x)*dx+(y-a.y)*dy)/(dx*dx+dy*dy)))
	return math.Hypot(x-(a.x+t*dx), y-(a.y+t*dy))
}

// The ball's hop, played when the route's exit changes: two bounces, the
// second lower, and it lands where it started.
const (
	// trayHopFrames is how many frames the hop is drawn in after the first.
	trayHopFrames = 14
	// trayHopHigh and trayHopLow are the bounces' heights in mark units.
	trayHopHigh = 8.0
	trayHopLow  = 3.5
	// trayHopSplit is how far through the hop the first bounce ends.
	trayHopSplit = 0.58
)

// markRunDuration is how long the logo in the window takes to run its whole
// course once.
const markRunDuration = 1100 * time.Millisecond

// The run's timeline, as fractions of markRunDuration: the mark gives up its
// ink, the ball draws the tall arc, then the short one, and settles where it
// rests.
const (
	runResetEnd  = 0.08
	runFirstEnd  = 0.52
	runSecondEnd = 0.90
	// runGhost is how visible the arcs stay as a track while the ball draws
	// them again.
	runGhost = 0.22
	// runSamples is how many dots ink one arc.
	runSamples = 48
)

// markRun is the run the logo plays when the route's exit changes; the zero
// value is none.
type markRun struct{ started time.Time }

// progress is how far through its course the run is at now, and whether it is
// still playing.
func (r markRun) progress(now time.Time) (float64, bool) {
	if r.started.IsZero() {
		return 0, false
	}
	progress := float64(now.Sub(r.started)) / float64(markRunDuration)
	if progress < 0 || progress >= 1 {
		return 0, false
	}
	return progress, true
}

// markPose is the mark partway through its run.
type markPose struct {
	// drawn is how much of each arc is inked, from 0 to 1.
	drawn [2]float64
	// ghost is how visible the whole track is under the ink, from 0 to 1.
	ghost float64
	// ball is where the ball is and ballAlpha how visible; restAlpha is how
	// visible the ball at rest is, which fades out as the run begins.
	ball      markPoint
	ballAlpha float64
	restAlpha float64
}

// poseAt is the mark at progress (0 to 1) of its run. The ball follows the
// arcs the way a thrown ball does, one step of its course after the other, so
// the ink follows it: a full mark gives up its ink to a faint track, the ball
// draws the tall arc, then the short one, and settles into its resting place.
func poseAt(progress float64) markPose {
	tall, short := markArcs[0], markArcs[1]
	switch {
	case progress < runResetEnd:
		s := progress / runResetEnd
		return markPose{ghost: 1 - (1-runGhost)*s, ball: tall.a, ballAlpha: s, restAlpha: 1 - s}
	case progress < runFirstEnd:
		u := (progress - runResetEnd) / (runFirstEnd - runResetEnd)
		return markPose{drawn: [2]float64{u, 0}, ghost: runGhost, ball: tall.at(u), ballAlpha: 1}
	case progress < runSecondEnd:
		u := (progress - runFirstEnd) / (runSecondEnd - runFirstEnd)
		return markPose{drawn: [2]float64{1, u}, ghost: runGhost, ball: short.at(u * short.end), ballAlpha: 1}
	}
	v := (progress - runSecondEnd) / (1 - runSecondEnd)
	v = 1 - (1-v)*(1-v)
	from := short.at(short.end)
	return markPose{
		drawn: [2]float64{1, 1}, ghost: runGhost, ballAlpha: 1,
		ball: markPoint{x: from.x + (markBall.x-from.x)*v, y: from.y + (markBall.y-from.y)*v},
	}
}

// hopLift is how high the ball is, in mark units, at progress (0 to 1)
// through a hop.
func hopLift(progress float64) float64 {
	if progress < trayHopSplit {
		return trayHopHigh * parabola(progress/trayHopSplit)
	}
	return trayHopLow * parabola((progress-trayHopSplit)/(1-trayHopSplit))
}

// parabola is 0 at both ends of 0..1 and 1 in the middle.
func parabola(t float64) float64 {
	return 4 * t * (1 - t)
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

// traySpeedColumn draws the upload and download rates, black on transparent,
// in the column the menu bar item keeps at one width.
func traySpeedColumn(up string, down string) *image.RGBA {
	return ui.Render(func(c *ui.Context) {
		c.Root().Background(ui.Transparent)
		ui.Column(c).Fill().Justify(ui.Center).Children(func() {
			for _, line := range []string{"↑ " + up, "↓ " + down} {
				ui.Text(c, line).FontSize(traySpeedFont).FontWeight(weightSemibold).FontFeatures("tnum").
					LineHeight(1.08).TextColor(ui.RGBA(0, 0, 0, 1)).TextAlign(ui.Start).NoWrap()
			}
		})
	}, traySpeedWidth, trayHeight, trayScale)
}

// traySpeedIcon draws mark with speed beside it, as the PNG the menu bar
// takes.
func traySpeedIcon(mark *image.NRGBA, speed *image.RGBA) ([]byte, error) {
	gap := traySpeedGap * trayScale
	img := image.NewNRGBA(image.Rect(0, 0, markSize+gap+speed.Bounds().Dx(), markSize))
	draw.Draw(img, mark.Bounds(), mark, image.Point{}, draw.Src)
	draw.Draw(img, speed.Bounds().Add(image.Point{X: markSize + gap}), speed, image.Point{}, draw.Over)
	return encodeTrayIcon(img)
}
