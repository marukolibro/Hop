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
// window's brand logo draws from markArcs/markBall, while the 18pt menu bar
// icon uses trayArcs/trayBall tuned for optical balance and clear separation
// between the second arc and the landing ball.
const (
	markSize      = 36
	markStroke    = 3.4
	markBallR     = 5.2
	markSamples   = 64
	markSupersamp = 4

	trayStroke     = 3.5
	trayBallR      = 3.6
	trayBallRingR  = 3.4
	trayBallHoleR  = 1.65
	trayIdleAlpha  = 0.42
	traySpeedIdleA = 0.58
	trayGradLeft   = 2.0
	trayGradRight  = 34.0
)

// markPoint is a point of the mark, in mark units.
type markPoint struct{ x, y float64 }

// markArc is a quadratic curve from a to b bent toward c, drawn up to end
// (1 for the whole curve).
type markArc struct {
	a, c, b markPoint
	end     float64
}

// trayCubic is a cubic Bezier curve in markSize (36x36) tray units.
type trayCubic struct {
	p0, c1, c2, p3 markPoint
}

// at returns the curve's point at t.
func (c trayCubic) at(t float64) markPoint {
	u := 1 - t
	uu, tt := u*u, t*t
	uuu, ttt := uu*u, tt*t
	return markPoint{
		x: uuu*c.p0.x + 3*uu*t*c.c1.x + 3*u*tt*c.c2.x + ttt*c.p3.x,
		y: uuu*c.p0.y + 3*uu*t*c.c1.y + 3*u*tt*c.c2.y + ttt*c.p3.y,
	}
}

var (
	// markArcs and markBall define the window's brand mark geometry.
	markArcs = []markArc{
		{a: markPoint{3, 28}, c: markPoint{10.5, -16}, b: markPoint{18, 28}, end: 1},
		{a: markPoint{18, 28}, c: markPoint{23.5, 6}, b: markPoint{28.5, 24}, end: 0.9},
	}
	markBall = markPoint{30.3, 25.6}

	// trayArcs and trayBall define the 18pt menu bar icon: optically centered
	// vertically (top/bottom margins ~6.8px at 2x) with a clear gap between
	// the second arc tip and the resting ball.
	trayArcs = []trayCubic{
		{p0: markPoint{3.6, 27.2}, c1: markPoint{6.8, 1.8}, c2: markPoint{14.6, 1.8}, p3: markPoint{17.2, 27.2}},
		{p0: markPoint{17.2, 27.2}, c1: markPoint{18.8, 13.8}, c2: markPoint{22.8, 13.2}, p3: markPoint{24.2, 22.0}},
	}
	trayBall = markPoint{30.5, 25.0}
)

// trayState describes what the menu bar icon communicates at a glance.
type trayState uint8

const (
	// trayStateIdle: core is connected (or starting), neither system proxy nor TUN is active.
	trayStateIdle trayState = iota
	// trayStateProxy: system proxy is active (TUN off).
	trayStateProxy
	// trayStateTun: TUN is capturing all system traffic.
	trayStateTun
	// trayStateError: core failed or cannot be reached.
	trayStateError
)

// trayVisual defines the menu bar mark's colors, ball style, template flag,
// and accompanying speed text ink for one state and menu bar appearance.
type trayVisual struct {
	from       color.RGBA
	to         color.RGBA
	alpha      float64
	hollowBall bool
	template   bool
	speedInk   ui.Color
}

// trayVisualFor returns the full-color or template styling for state over a
// dark or light menu bar. Idle uses a faded monochrome hollow-ring template so
// macOS tints it natively; active states use full-color brand gradients so the
// running state stands out immediately.
func trayVisualFor(state trayState, dark bool) trayVisual {
	speedInk := ui.RGBA(20, 25, 35, 0.88)
	if dark {
		speedInk = ui.RGBA(255, 255, 255, 0.92)
	}
	switch state {
	case trayStateProxy:
		if dark {
			return trayVisual{
				from:  color.RGBA{R: 0x4D, G: 0xA6, B: 0xFF, A: 0xFF},
				to:    color.RGBA{R: 0x38, G: 0xE0, B: 0xF5, A: 0xFF},
				alpha: 1, speedInk: speedInk,
			}
		}
		return trayVisual{
			from:  color.RGBA{R: 0x1A, G: 0x73, B: 0xE8, A: 0xFF},
			to:    color.RGBA{R: 0x02, G: 0x84, B: 0xC7, A: 0xFF},
			alpha: 1, speedInk: speedInk,
		}
	case trayStateTun:
		if dark {
			return trayVisual{
				from:  color.RGBA{R: 0x38, G: 0xBD, B: 0xF8, A: 0xFF},
				to:    color.RGBA{R: 0xA8, G: 0x55, B: 0xF7, A: 0xFF},
				alpha: 1, speedInk: speedInk,
			}
		}
		return trayVisual{
			from:  color.RGBA{R: 0x25, G: 0x63, B: 0xEB, A: 0xFF},
			to:    color.RGBA{R: 0x93, G: 0x33, B: 0xEA, A: 0xFF},
			alpha: 1, speedInk: speedInk,
		}
	case trayStateError:
		if dark {
			return trayVisual{
				from:  color.RGBA{R: 0xFB, G: 0x71, B: 0x85, A: 0xFF},
				to:    color.RGBA{R: 0xF8, G: 0x71, B: 0x71, A: 0xFF},
				alpha: 1, speedInk: speedInk,
			}
		}
		return trayVisual{
			from:  color.RGBA{R: 0xE1, G: 0x1D, B: 0x48, A: 0xFF},
			to:    color.RGBA{R: 0xDC, G: 0x26, B: 0x26, A: 0xFF},
			alpha: 1, speedInk: speedInk,
		}
	default:
		return trayVisual{
			from:       color.RGBA{A: 0xFF},
			to:         color.RGBA{A: 0xFF},
			alpha:      trayIdleAlpha,
			hollowBall: true,
			template:   true,
			speedInk:   ui.RGBA(0, 0, 0, traySpeedIdleA),
		}
	}
}

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

// encodeTrayIcon is img as the PNG the menu bar takes.
func encodeTrayIcon(img image.Image) ([]byte, error) {
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, fmt.Errorf("encode tray icon: %w", err)
	}
	return out.Bytes(), nil
}

// trayArcsMask precomputes the 4x4 supersampled alpha coverage of the tray
// mark's two arcs once at startup so state and animation frames color it in
// microseconds.
func trayArcsMask() *image.Alpha {
	segments := traySegments()
	mask := image.NewAlpha(image.Rect(0, 0, markSize, markSize))
	for y := range markSize {
		for x := range markSize {
			coverage := trayPixelCoverage(float64(x), float64(y), func(px float64, py float64) bool {
				return onTrayArcs(px, py, segments)
			})
			if coverage > 0 {
				mask.SetAlpha(x, y, color.Alpha{A: uint8(math.Round(coverage * 255))})
			}
		}
	}
	return mask
}

// trayMarkImage renders the 36x36 menu bar icon for state, dark menu bar
// polarity, and vertical ball lift (0 at rest).
func trayMarkImage(arcsMask *image.Alpha, state trayState, dark bool, ballLift float64) *image.NRGBA {
	vis := trayVisualFor(state, dark)
	ball := markPoint{x: trayBall.x, y: trayBall.y - ballLift}
	img := image.NewNRGBA(image.Rect(0, 0, markSize, markSize))
	for y := range markSize {
		for x := range markSize {
			arcCov := float64(arcsMask.AlphaAt(x, y).A) / 255
			ballCov := trayBallCoverage(float64(x), float64(y), ball, vis.hollowBall)
			cov := max(arcCov, ballCov) * vis.alpha
			if cov <= 0 {
				continue
			}
			r, g, b := gradientAt(vis.from, vis.to, float64(x)+0.5)
			img.SetNRGBA(x, y, color.NRGBA{
				R: r,
				G: g,
				B: b,
				A: uint8(math.Round(min(1, cov) * 255)),
			})
		}
	}
	return img
}

// gradientAt is the colour of the left-to-right gradient from from to to at
// column x of the mark.
func gradientAt(from, to color.RGBA, x float64) (uint8, uint8, uint8) {
	t := max(0, min(1, (x-trayGradLeft)/(trayGradRight-trayGradLeft)))
	lerp := func(a, b uint8) uint8 {
		return uint8(math.Round(float64(a) + (float64(b)-float64(a))*t))
	}
	return lerp(from.R, to.R), lerp(from.G, to.G), lerp(from.B, to.B)
}

// traySegments approximates the tray arcs with short straight segments.
func traySegments() [][2]markPoint {
	var segments [][2]markPoint
	for _, arc := range trayArcs {
		for i := range markSamples {
			t0 := float64(i) / markSamples
			t1 := float64(i+1) / markSamples
			segments = append(segments, [2]markPoint{arc.at(t0), arc.at(t1)})
		}
	}
	return segments
}

// trayPixelCoverage is how much of the pixel with its corner at (x, y) the
// shape inside covers, from a grid of samples inside the pixel.
func trayPixelCoverage(x float64, y float64, inside func(x float64, y float64) bool) float64 {
	hits := 0
	for sy := range markSupersamp {
		for sx := range markSupersamp {
			px := x + (float64(sx)+0.5)/markSupersamp
			py := y + (float64(sy)+0.5)/markSupersamp
			if inside(px, py) {
				hits++
			}
		}
	}
	return float64(hits) / (markSupersamp * markSupersamp)
}

// trayBallCoverage is how much of the pixel at (x, y) the ball centred on ball
// covers: a disc, or a ring when hollow.
func trayBallCoverage(x float64, y float64, ball markPoint, hollow bool) float64 {
	// Quick bounding check before 4x4 supersampling.
	maxR := trayBallR + 1.5
	if math.Abs(x+0.5-ball.x) > maxR || math.Abs(y+0.5-ball.y) > maxR {
		return 0
	}
	if hollow {
		return trayPixelCoverage(x, y, func(px float64, py float64) bool {
			d := math.Hypot(px-ball.x, py-ball.y)
			return d <= trayBallRingR && d >= trayBallHoleR
		})
	}
	return trayPixelCoverage(x, y, func(px float64, py float64) bool {
		return math.Hypot(px-ball.x, py-ball.y) <= trayBallR
	})
}

// onTrayArcs says whether a point lies on the tray arcs; strokes have round
// ends.
func onTrayArcs(x float64, y float64, segments [][2]markPoint) bool {
	for _, s := range segments {
		if segmentDistance(x, y, s[0], s[1]) <= trayStroke/2 {
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

// The speed beside the mark in the menu bar: upload over download, aligned
// right in a column wide enough for the longest compact rate, so the item
// keeps its width as the numbers change and their digits stay in place.
const (
	// traySpeedWidth is the speed column's width in points; it holds the
	// widest compact rates, such as "185M" at 23 points.
	traySpeedWidth = 24
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

// traySpeedPainter draws the upload and download rates in the column the menu
// bar item keeps at one width. It keeps one renderer for the app's life: every
// ui.Render makes a text system that loads the app's fonts (RegisterFonts)
// again and never lets them go, about 1 MB a call, and the column is drawn
// again every second the speed changes. It runs on the main thread with the
// MenuBar.
type traySpeedPainter struct {
	tester *ui.Tester
	rates  [2]string
	ink    ui.Color
}

func newTraySpeedPainter() *traySpeedPainter {
	p := &traySpeedPainter{ink: ui.RGBA(0, 0, 0, 1)}
	p.tester = ui.NewTester(func(c *ui.Context) {
		c.Root().Background(ui.Transparent)
		ui.Column(c).Fill().Justify(ui.Center).Children(func() {
			for _, rate := range p.rates {
				ui.Text(c, rate).FontSize(traySpeedFont).FontWeight(weightSemibold).FontFeatures("tnum").
					LineHeight(1.08).TextColor(p.ink).TextAlign(ui.End).NoWrap()
			}
		})
	}, traySpeedWidth, trayHeight)
	p.tester.SetScale(trayScale)
	return p
}

// draw returns the column with up over down in ink: a copy, since the renderer
// draws the next column into the same pixels.
func (p *traySpeedPainter) draw(up string, down string, ink ui.Color) *image.RGBA {
	p.rates = [2]string{up, down}
	p.ink = ink
	p.tester.Frame()
	frame := p.tester.Image()
	column := image.NewRGBA(frame.Rect)
	draw.Draw(column, column.Rect, frame, frame.Rect.Min, draw.Src)
	return column
}

// traySpeedIcon draws mark with speed beside it, as the PNG the menu bar
// takes.
func traySpeedIcon(mark *image.NRGBA, speed *image.RGBA) ([]byte, error) {
	gap := traySpeedGap * trayScale
	img := image.NewNRGBA(image.Rect(0, 0, markSize+gap+speed.Bounds().Dx(), markSize))
	draw.Draw(img, mark.Bounds(), mark, image.Point{}, draw.Src)
	draw.Draw(img, speed.Bounds().Add(image.Point{X: markSize + gap}), speed, image.Point{}, draw.Src)
	return encodeTrayIcon(img)
}
