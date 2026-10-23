package handler

import (
	"math"
	"time"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/plugins/loading"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"

	"hop/internal/model"
)

// Motion in Hop is for what changes: the switch slides to the new choice,
// numbers roll, and the route gauge's arcs follow the traffic. Nothing here
// moves for as long as it is shown; a spinner is the one exception, and only
// while something runs.

const (
	switchHeight  float32 = 28
	switchInset   float32 = 2
	statusRipple          = 900 * time.Millisecond
	entranceTime          = 420 * time.Millisecond
	entranceStep          = 45 * time.Millisecond
	entranceShift float32 = 10

	// A dialog opens quickly and eases out, with no overshoot: it rises from a
	// slightly smaller panel, never from a point, and the scrim fades beside it.
	// Opacity and position share one ease-out, and closing runs the same way.
	overlayTime              = 180 * time.Millisecond
	dialogEnterScale float32 = 0.96
	dialogEnterShift float32 = 6
	pageEnterTime            = 240 * time.Millisecond
	pageEnterShift   float32 = 8

	// The page switch: segments fit two CJK labels at 14 with air around
	// them, the track a floating pill 30 tall inside 36; below the narrow
	// breakpoint the segments tighten so the width still fits.
	tabWidth       float32 = 64
	tabNarrowWidth float32 = 50
	tabHeight      float32 = 36
	tabInset       float32 = 3

	// opticalLift raises text in small controls: CJK glyphs sit about a
	// pixel below the centre of the line box the font's metrics give.
	opticalLift float32 = 1

	// flowFullLevel is the doubling of idleRate at which the route gauge's
	// rings are full, about 4 MB/s.
	flowFullLevel = 12
)

// tabPillInk is the dark ink on the page switch's light pill, in both
// appearances the design gives it.
var tabPillInk = ui.Hex("#1D2B43")

// switchStyle colours a sliding switch.
type switchStyle struct {
	track ui.Color
	thumb ui.Color
	// thumbMaterial, when set, fills the thumb in place of its colour.
	thumbMaterial ui.Material
	active        ui.Color
	inactive      ui.Color
	segmentWidth  float32
}

// pageSwitchStyle is the quiet switch between choices in settings, the log
// filter and the outbound mode.
func pageSwitchStyle(t *ui.Theme, segmentWidth float32) switchStyle {
	if onGlass(t) {
		return switchStyle{track: glassFill(t, glassTrackAlpha), thumb: glassFill(t, glassTrackSelected),
			active: t.Text, inactive: t.TextMuted, segmentWidth: segmentWidth}
	}
	return switchStyle{track: t.SurfacePressed, thumb: t.Surface, thumbMaterial: glass.Glass{}, active: t.Text, inactive: t.TextMuted, segmentWidth: segmentWidth}
}

// slidingSwitch chooses one of labels into *selected, with a thumb that
// springs to the choice. onChange hears the new index.
func slidingSwitch(c *ui.Context, key string, selected *int, labels []string, style switchStyle, onChange func(int)) {
	inner := switchHeight - 2*switchInset
	track := ui.Box(c).Key(key).Label(key).Width(style.segmentWidth*float32(len(labels)) + 2*switchInset).Height(switchHeight).
		Radius(switchHeight / 2).Background(style.track)
	x := track.AnimateTo("thumb", float32(*selected)*style.segmentWidth, ui.Snappy)
	track.Children(func() {
		thumb := ui.Box(c).Absolute().Left(switchInset + x).Top(switchInset).Width(style.segmentWidth).Height(inner).Radius(inner / 2)
		if style.thumbMaterial != nil {
			thumb.Material(style.thumbMaterial)
		} else {
			thumb.Background(style.thumb).Shadow(0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.12))
		}
		ui.Row(c).Absolute().Left(switchInset).Top(switchInset).Children(func() {
			for i, label := range labels {
				segment := ui.Box(c).Key(label).Width(style.segmentWidth).Height(inner).Center().Cursor(ui.CursorPointer)
				ink := style.inactive
				if i == *selected {
					ink = style.active
				}
				color := segment.AnimateColor("ink", ink, ui.Smooth)
				segment.OnClick(func() {
					if *selected != i {
						*selected = i
						onChange(i)
					}
				})
				segment.Children(func() {
					ui.Text(c, label).FontSize(fontSecondary).FontWeight(weightMedium).TextColor(color).Translate(0, -opticalLift)
				})
			}
		})
	})
}

// pageTabs chooses the window's page into *selected: a translucent track
// with a solid light pill that springs to the chosen label, as the design's
// segmented page switch. The labels sit on the window tint; the pill marks
// the page.
func pageTabs(c *ui.Context, t *ui.Theme, key string, selected *int, labels []string, onChange func(int)) {
	width, _ := c.Size()
	width -= c.TitleBar().Right
	segment := tabWidth
	if width < 760 {
		segment = tabNarrowWidth
	}
	trackFill := t.SurfacePressed
	if onGlass(t) {
		trackFill = glassFill(t, glassTrackAlpha)
	}
	pill := ui.Hex("#E7EBF3")
	if !t.Dark {
		pill = ui.RGB(255, 255, 255)
	}
	inner := tabHeight - 2*tabInset
	track := ui.Box(c).Key(key).Label(key).Width(segment*float32(len(labels)) + 2*tabInset).
		Height(tabHeight).Radius(tabHeight / 2).Background(trackFill)
	x := track.AnimateTo("thumb", float32(*selected)*segment, ui.Snappy)
	track.Children(func() {
		ui.Box(c).Absolute().Left(tabInset+x).Top(tabInset).Width(segment).Height(inner).
			Radius(inner/2).Background(pill).Shadow(0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.12))
		ui.Row(c).Absolute().Left(tabInset).Top(tabInset).Children(func() {
			for i, label := range labels {
				tab := ui.Box(c).Key(label).Width(segment).Height(inner).Center().Cursor(ui.CursorPointer)
				ink, weight := t.TextMuted, weightMedium
				if i == *selected {
					ink, weight = tabPillInk, weightSemibold
				}
				color := tab.AnimateColor("ink", ink, ui.Smooth)
				tab.OnClick(func() {
					if *selected != i {
						*selected = i
						onChange(i)
					}
				})
				tab.Children(func() {
					ui.Text(c, label).FontSize(fontEmphasis).FontWeight(weight).TextColor(color).Translate(0, -opticalLift)
				})
			}
		})
	})
}

// statusDot draws a dot in color; when live turns on, one ring ripples out
// of it, as a heartbeat of the core, and the dot then rests without asking
// for another frame.
func statusDot(c *ui.Context, color ui.Color, live bool) {
	dot := ui.Box(c).Key("statusDot").Width(16).Height(16)
	ripple := float32(0)
	if live {
		ripple = dot.AnimateTo("ripple", 1, ui.Animation{Duration: statusRipple, Ease: ui.EaseOut})
	} else {
		dot.Jump("ripple", 0)
	}
	dot.Draw(func(p *ui.Painter, r ui.Rect) {
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		if live && ripple < 1 {
			radius := 3.5 + 4.5*ripple
			p.Fill(circle(cx, cy, radius), color.Alpha(0.35*(1-ripple)), radius)
		}
		p.Fill(circle(cx, cy, 3.5), color, 3.5)
	})
}

// trafficSwitches are the header's buttons for how traffic reaches Hop, the
// system proxy and TUN, each lit by a green dot while on. The header holds
// them so every page shows them. A switch another client holds says so
// before them, and so does a core Hop cannot reach.
func (d *Dashboard) trafficSwitches(c *ui.Context, t *ui.Theme) {
	if d.status == statusFailed {
		ui.Text(c, "内核未连接").FontSize(fontSecondary).TextColor(t.Danger).NoWrap()
	}
	for _, note := range []string{d.systemProxyHint(), d.tunHint()} {
		if note != "" {
			mutedText(c, t, note).MaxLines(1)
		}
	}
	sp, tun := &d.sysProxy, &d.tun
	statusToggle(c, t, pillSpec{
		label: "系统代理", on: sp.on, busy: sp.busy,
		disabled: !sp.loaded || (sp.status.Owner == model.ProxyOwnerOther && !sp.on),
	}, "浏览器和大多数应用经过 Hop", func() { d.beginSystemProxy(!sp.on) })
	statusToggle(c, t, pillSpec{
		label: "增强模式", on: tun.on, busy: tun.busy,
		disabled: !tun.loaded || (d.controlsLocked() && !tun.busy) || (tun.taken && !tun.on),
	}, "接管全部流量,包括终端和游戏", func() { d.beginTun(!tun.on) })
}

// flowIntensity maps a throughput onto 0..1, by doublings of idleRate so a
// trickle shows and a download does not saturate at once; 0 while idle.
func flowIntensity(bytesPerSecond int64) float32 {
	if bytesPerSecond < idleRate {
		return 0
	}
	level := math.Log2(float64(bytesPerSecond) / idleRate)
	return float32(min(1, 0.2+0.8*level/flowFullLevel))
}

// hopMark draws Hop's mark in ink, as the menu bar icon shows it: a ball
// bouncing twice and landing. The arcs blend from ink's first colour at the
// left to its second at the right, where the ball lands in it. While run
// plays, the mark draws itself again: the ball throws the tall arc, then the
// short one, and lands. Small dots give the arcs the round ends the path's
// own ends lack.
func hopMark(c *ui.Context, size float32, ink markInk, run markRun) {
	ui.Box(c).Width(size).Height(size).Draw(func(p *ui.Painter, r ui.Rect) {
		scale := r.W / markSize
		at := func(m markPoint) (float32, float32) {
			return r.X + float32(m.x)*scale, r.Y + float32(m.y)*scale
		}
		inkAt := func(x float32) ui.Color { return ink.from.Mix(ink.to, (x-r.X)/r.W) }
		stroke := float32(markStroke) * scale
		ballR := float32(markBallR) * scale
		drawBall := func(m markPoint, alpha float64) {
			if alpha <= 0 {
				return
			}
			bx, by := at(m)
			p.Fill(circle(bx, by, ballR), inkAt(bx).Alpha(float32(alpha)), ballR)
		}
		// drawTrack strokes both arcs whole, at alpha of their ink.
		drawTrack := func(alpha float64) {
			path := &ui.Path{}
			for _, arc := range markArcs {
				a, control, end := arc.trimmed()
				ax, ay := at(a)
				cx, cy := at(control)
				ex, ey := at(end)
				path.MoveTo(ax, ay).QuadTo(cx, cy, ex, ey)
				p.Fill(circle(ax, ay, stroke/2), inkAt(ax).Alpha(float32(alpha)), stroke/2)
				p.Fill(circle(ex, ey, stroke/2), inkAt(ex).Alpha(float32(alpha)), stroke/2)
			}
			p.StrokePathGradient(path, stroke, ui.LinearGradient{
				From: ink.from.Alpha(float32(alpha)), To: ink.to.Alpha(float32(alpha)), Angle: 90,
			})
		}
		progress, running := run.progress(p.Now())
		if !running {
			drawTrack(1)
			drawBall(markBall, 1)
			return
		}
		p.AnimationFrame()
		pose := poseAt(progress)
		drawTrack(pose.ghost)
		for i, arc := range markArcs {
			if pose.drawn[i] <= 0 {
				continue
			}
			// The ink is a row of dots as round as the stroke, in the colour
			// of the place they lie, so it grows without its gradient moving.
			for k := 0; k <= runSamples; k++ {
				pt := arc.at(pose.drawn[i] * arc.end * float64(k) / runSamples)
				x, y := at(pt)
				p.Fill(circle(x, y, stroke/2), inkAt(x), stroke/2)
			}
		}
		drawBall(markBall, pose.restAlpha)
		drawBall(pose.ball, pose.ballAlpha)
	})
}

// pageTransition fades a page in as the page switch lands on it, rising a few
// points. Changes inside the page, and resizing the window, do not animate.
var pageTransition = ui.ElementTransition{
	Duration: pageEnterTime, Colors: true,
	Enter: &ui.Motion{Opacity: 0, Y: pageEnterShift},
}

// animatedDialog is ui.DialogBase with an entrance and an exit. DialogBase
// builds nothing once *open is false, so the dialog's presence is a number of
// its own that eases toward 1 while it is open and toward 0 after: the dialog
// stays built until it reaches 0, fading with the scrim and sinking back to a
// slightly smaller panel. Opacity and scale share one ease-out, closing runs
// the opening backwards, and a click on the scrim or Escape still closes it at
// once.
func animatedDialog(c *ui.Context, key string, open *bool, fn func(backdrop ui.Element, panel ui.Element)) {
	target := float32(0)
	if *open {
		target = 1
	}
	presence := ui.Box(c).Key("presence-"+key).AnimateTo("presence", target, ui.Animation{Duration: overlayTime, Ease: ui.EaseOut})
	shown := *open || presence > 0
	ui.DialogBase(c, &shown, func(backdrop ui.Element, panel ui.Element) {
		fn(backdrop, panel)
		backdrop.Opacity(presence)
		scale := dialogEnterScale + (1-dialogEnterScale)*presence
		panel.Opacity(presence).Scale(scale, scale).Translate(0, (1-presence)*dialogEnterShift).Disabled(!*open)
	})
	if !shown {
		*open = false
	}
}

// entrance plays once, as the element first shows: it rises into place and
// fades in, step entrances after the first.
func entrance(e ui.Element, step int) ui.Element {
	v := e.Keyframes("entrance", []ui.Keyframe{{At: 0, Value: 0}, {At: entranceTime, Value: 1, Ease: ui.EaseOut}},
		ui.KeyframeOptions{Delay: time.Duration(step) * entranceStep})
	return e.Opacity(v).Translate(0, (1-v)*entranceShift)
}

// spinner turns while something runs: MyGo's Atom, three rings tumbling
// inside a circle, drawn in color.
func spinner(c *ui.Context, size float32, color ui.Color) {
	loading.Atom(c, loading.Options{Size: size, Color: color}).Label("处理中")
}

// sparkArea draws history across r, download filled, upload as a line.
func sparkArea(p *ui.Painter, r ui.Rect, history model.TrafficHistory, down ui.Color, up ui.Color) {
	if len(history.Down) < 2 {
		return
	}
	peak := peakOf(history)
	xs, downYs := series(history.Down, peak)
	_, upYs := series(history.Up, peak)
	p.Clip(r, 0, func() {
		p.FillPathGradient(chart.Area(r, xs, nil, downYs, chart.CurveMonotone), ui.LinearGradient{
			From: down.Alpha(0.18), To: down.Alpha(0), Angle: 180,
		})
		p.StrokePath(chart.Line(r, xs, upYs, chart.CurveMonotone), 1.25, up)
		p.StrokePath(chart.Line(r, xs, downYs, chart.CurveMonotone), 1.5, down)
	})
}

// sparkAreaLive draws the main window's plot without smoothing: straight
// segments keep a spike a spike. The window is right-aligned, so a young
// history starts inside the plot; the area begins on the baseline one step
// before its first sample, rising over a segment instead of closing with a
// vertical edge, the "L" a startup spike used to draw.
func sparkAreaLive(p *ui.Painter, r ui.Rect, history model.TrafficHistory, down ui.Color, up ui.Color) {
	if len(history.Down) < 2 {
		return
	}
	peak := peakOf(history)
	xs, downYs := series(history.Down, peak)
	_, upYs := series(history.Up, peak)
	areaXs, areaYs := sparkLeadIn(xs, downYs)
	p.Clip(r, 0, func() {
		p.FillPathGradient(chart.Area(r, areaXs, nil, areaYs, chart.CurveLinear), ui.LinearGradient{
			From: down.Alpha(0.18), To: down.Alpha(0), Angle: 180,
		})
		p.StrokePath(chart.Line(r, xs, upYs, chart.CurveLinear), 1.25, up)
		p.StrokePath(chart.Line(r, xs, downYs, chart.CurveLinear), 1.5, down)
	})
}

// sparkLeadIn returns the area's series with a zero at one sample before a
// partial history, so its left boundary slopes up from the baseline. A full
// window already starts at the plot's edge, where no lead-in fits.
func sparkLeadIn(xs, ys []float32) ([]float32, []float32) {
	if len(xs) == 0 || xs[0] <= 0 {
		return xs, ys
	}
	lead := xs[0] - 1/float32(historyLimit-1)
	if lead < 0 {
		lead = 0
	}
	if lead == xs[0] {
		return xs, ys
	}
	areaXs := make([]float32, 0, len(xs)+1)
	areaYs := make([]float32, 0, len(ys)+1)
	return append(append(areaXs, lead), xs...), append(append(areaYs, 0), ys...)
}

func circle(cx float32, cy float32, radius float32) ui.Rect {
	return ui.Rect{X: cx - radius, Y: cy - radius, W: 2 * radius, H: 2 * radius}
}
