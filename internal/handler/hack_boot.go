package handler

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"hop/locales"
)

const (
	// hackBootTime is the power-on, in four parts by share of it: the boot log
	// on black, a line drawn across the black, the screen opened from that
	// line, then the phosphor settling.
	hackBootTime = 2200 * time.Millisecond
	// hackBootLogEnd, hackBootLineEnd and hackBootOpenEnd end the first three
	// parts.
	hackBootLogEnd  float32 = 0.40
	hackBootLineEnd float32 = 0.52
	hackBootOpenEnd float32 = 0.82
	// hackBootFade is how much of the log's end it takes to leave.
	hackBootFade float32 = 0.04
	// hackBootFirst is when the first log line appears, hackBootStep the time
	// between one and the next, and hackBootDone how long after its line
	// appears it is marked done, all as a share of the whole.
	hackBootFirst float32 = 0.04
	hackBootStep  float32 = 0.10
	hackBootDone  float32 = 0.06
	// hackBootLogWidth is the log's column.
	hackBootLogWidth float32 = 260
	// hackBootLine is the thickness of the bright line and hackBootBloom the
	// light around it.
	hackBootLine  float32 = 2
	hackBootBloom float32 = 14
	// hackBootWash is how bright the screen starts when it opens, and
	// hackBootFlicker how fast that light wavers while it settles.
	hackBootWash    float32 = 0.4
	hackBootFlicker float32 = 70
)

// hackBoot plays the screen's power-on when the theme turns into Hack: the
// window goes black, a line draws across it, opens to the whole screen, on the
// home page, and the light settles. Starting with the theme already Hack plays nothing, and
// leaving it plays nothing either.
func (d *Dashboard) hackBoot(c *ui.Context, t *ui.Theme) {
	hack := hackTheme(t)
	if hack && d.hackShownKnown && !d.hackShown {
		d.hackBootRun++
		d.hackBooting = true
		// The screen powers on at the home page, not under the settings that
		// changed the theme.
		d.page = pageNodes
	}
	d.hackShown, d.hackShownKnown = hack, true
	if !hack || !d.hackBooting {
		return
	}
	box := ui.Column(c.Key("hackBoot")).Absolute().Left(0).Top(0).Right(0).Bottom(0).PassThrough().IgnoreInput(true).
		AccessHidden(true).Justify(ui.Center).AlignItems(ui.Center)
	v := box.Keyframes("boot", []ui.Keyframe{{At: 0, Value: 0}, {At: hackBootTime, Value: 1}}, ui.KeyframeOptions{Run: d.hackBootRun})
	if v >= 1 {
		d.hackBooting = false
	}
	box.Draw(func(p *ui.Painter, r ui.Rect) { hackBootFrame(p, r, t, v) })
	box.Children(func() { hackBootLog(c, t, v) })
}

// hackBootLog is what the screen says while it is black: three lines typed one
// after the other, the first two marked done, the last with a cursor.
func hackBootLog(c *ui.Context, t *ui.Theme, v float32) {
	opacity := min(1, max(0, (hackBootLogEnd-v)/hackBootFade))
	if opacity == 0 {
		return
	}
	lines := []string{locales.HackBootTheme(), locales.HackBootDisplay(), locales.HackBootReady()}
	ui.Column(c.Key("log")).Width(hackBootLogWidth).Gap(gapTight).Opacity(opacity).Children(func() {
		for i, line := range lines {
			at := hackBootFirst + hackBootStep*float32(i)
			if v < at {
				return
			}
			text := line
			switch {
			case i == len(lines)-1:
				text += " █"
			case v >= at+hackBootDone:
				text += " ... OK"
			}
			ui.Text(c.Key(line), text).FontSize(fontBody).FontWeight(weightSemibold).TextColor(t.Accent).NoWrap()
		}
	})
}

// hackBootFrame paints the power-on at progress v, 0 to 1, over the window r.
func hackBootFrame(p *ui.Painter, r ui.Rect, t *ui.Theme, v float32) {
	black := ui.RGB(0, 0, 0)
	across := easeOut(min(1, max(0, (v-hackBootLogEnd)/(hackBootLineEnd-hackBootLogEnd))))
	open := easeOut(min(1, max(0, (v-hackBootLineEnd)/(hackBootOpenEnd-hackBootLineEnd))))
	w := r.W * across
	h := hackBootLine + (r.H-hackBootLine)*open
	x, y := r.X+(r.W-w)/2, r.Y+(r.H-h)/2
	// The black around the lit part.
	p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: y - r.Y}, black, 0)
	p.Fill(ui.Rect{X: r.X, Y: y + h, W: r.W, H: r.Y + r.H - y - h}, black, 0)
	p.Fill(ui.Rect{X: r.X, Y: y, W: x - r.X, H: h}, black, 0)
	p.Fill(ui.Rect{X: x + w, Y: y, W: r.X + r.W - x - w, H: h}, black, 0)
	// While the screen is only a line, the line is all there is: bright, with
	// its bloom; once open, the edges keep a thin glow until they leave.
	light := t.Accent.Mix(ui.RGB(255, 255, 255), 0.6)
	edge := 1 - open
	if open == 0 {
		p.Fill(ui.Rect{X: x, Y: y - hackBootBloom/2, W: w, H: h + hackBootBloom}, t.Accent.Alpha(0.18), hackBootBloom/2)
		p.Fill(ui.Rect{X: x, Y: y, W: w, H: h}, light, 0)
		return
	}
	p.Fill(ui.Rect{X: x, Y: y, W: w, H: hackBootLine}, light.Alpha(edge), 0)
	p.Fill(ui.Rect{X: x, Y: y + h - hackBootLine, W: w, H: hackBootLine}, light.Alpha(edge), 0)
	// The phosphor settling: a wash of light over the open screen that fades
	// with a flicker.
	settle := max(0, (v-hackBootLineEnd)/(1-hackBootLineEnd))
	flicker := 0.75 + 0.25*float32(math.Sin(float64(v*hackBootFlicker)))
	wash := hackBootWash * (1 - settle) * (1 - settle) * flicker
	p.Fill(ui.Rect{X: x, Y: y, W: w, H: h}, t.Accent.Alpha(max(0, wash)), 0)
}

// easeOut is a cubic ease-out from 0 to 1.
func easeOut(v float32) float32 {
	rest := 1 - v
	return 1 - rest*rest*rest
}
