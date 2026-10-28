package handler

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
	"hop/locales"
)

const (
	pillDot          float32 = 7
	pillOnFillAlpha  float32 = 0.08
	pillOnLineAlpha  float32 = 0.45
	pillOffDotAlpha  float32 = 0.35
	pillDotGlowAlpha float32 = 0.22

	statusOnFillAlpha  float32 = 0.14
	statusOnHoverAlpha float32 = 0.2
	statusOnLineAlpha  float32 = 0.55

	// A traffic switch shows a small switch: a track miniTrackW by miniTrackH
	// with a knob miniKnobInset inside its ends.
	miniTrackW    float32 = 22
	miniTrackH    float32 = 13
	miniKnobInset float32 = 2
	miniOffAlpha  float32 = 0.35

	// The hint ring breathes once every hintCycle, glowing for hintBreath
	// of it and resting for the rest: slow enough to be a nudge, not an
	// alarm, and kept up for as long as the hint applies, so it is there
	// whenever the user looks.
	hintCycle          = 4 * time.Second
	hintBreath         = 1600 * time.Millisecond
	hintSpread float32 = 4
	hintAlpha  float32 = 0.35
	// hintFPS is the frame rate the ring breathes at: a slow glow looks as
	// smooth at 30 frames a second as at the display's rate.
	hintFPS float32 = 30
)

// pillSpec is what a toggle pill shows and whether it takes clicks now.
type pillSpec struct {
	label    string
	on       bool
	disabled bool
	// busy shows a spinner in place of the dot or switch while the change
	// runs.
	busy bool
	// hint breathes a ring around the pill, to point at it.
	hint bool
}

// togglePill is a capsule that takes the accent's tint while its choice is
// on, with lead drawn before its label: the outbound modes, one of which is
// always on, and the traffic switches, which are on or off. Its fill and
// outline ease between the two.
func togglePill(c *ui.Context, t *ui.Theme, spec pillSpec, lead func(), onClick func()) ui.Element {
	b := ui.ButtonBase(c).Key(spec.label).Label(spec.label).Height(switchHeight).Padding(0, 12).Gap(gapTight).
		Radius(switchHeight / 2).AlignItems(ui.Center).Justify(ui.Center).Disabled(spec.disabled || spec.busy)
	// Off, a pill is only a faint fill; on, it takes the accent's tint and
	// outline, so the choices that are on stand out of the row. On glass
	// both sit on a near-opaque backing, so their text never meets the
	// desktop.
	fill, line, ink := t.Text.Alpha(softFillAlpha), ui.Transparent, t.TextMuted
	if onGlass(t) {
		backing := controlBacking(t)
		fill, line = backing, t.Text.Alpha(0.24)
		if spec.on {
			fill, line, ink = t.Accent.Alpha(pillOnFillAlpha).Over(backing), t.Accent.Alpha(pillOnLineAlpha), t.Text
		} else if b.Hovered() && !spec.disabled {
			fill = backing.Mix(t.Text, softFillAlpha)
		}
	} else if spec.on {
		fill, line, ink = t.Accent.Alpha(pillOnFillAlpha), t.Accent.Alpha(pillOnLineAlpha), t.Text
	} else if b.Hovered() && !spec.disabled {
		fill = t.Text.Alpha(softHoverAlpha)
	}
	b.Background(b.AnimateColor("fill", fill, ui.Smooth)).Border(1, b.AnimateColor("line", line, ui.Smooth))
	scale := float32(1)
	if b.Pressed() {
		scale = pressedScale
	}
	b.AnimatePose(ui.Pose{Scale: scale}, ui.Snappy)
	if spec.disabled {
		b.Opacity(disabledOpacity)
	}
	if !spec.disabled && !spec.busy {
		b.Cursor(ui.CursorPointer)
	}
	b.OnClick(onClick)
	b.Children(func() {
		if spec.hint {
			hintRing(c, t.Accent)
		}
		if spec.busy {
			spinner(c, iconInline, t.Accent)
		} else {
			lead()
		}
		ui.Text(c, spec.label).FontSize(fontSecondary).FontWeight(weightMedium).TextColor(ink).
			NoWrap().Translate(0, -opticalLift)
	})
	return b
}

// pillDotMark is the dot of a mode pill: it lights in the accent, with a
// faint glow, while its mode is on.
func pillDotMark(c *ui.Context, t *ui.Theme, on bool) {
	color, glow := t.TextMuted.Alpha(pillOffDotAlpha), ui.Transparent
	if on {
		color, glow = t.Accent, t.Accent.Alpha(pillDotGlowAlpha)
	}
	dot := ui.Box(c).Key("dot").Width(pillDot).Height(pillDot).Radius(pillDot / 2)
	dot.Background(dot.AnimateColor("dot", color, ui.Smooth)).Shadow(0, 0, 0, 3, glow)
}

// miniSwitch is the small switch a traffic switch shows, so it reads as on
// or off at a glance: a grey track with the knob at the left when off, a
// track in onColor with the knob at the right when on. The knob slides and
// the track eases between the two.
func miniSwitch(c *ui.Context, t *ui.Theme, on bool, onColor ui.Color) {
	track, target := t.TextMuted.Alpha(miniOffAlpha), float32(0)
	if on {
		track, target = onColor, 1
	}
	box := ui.Box(c).Key("miniSwitch").Width(miniTrackW).Height(miniTrackH)
	trackColor := box.AnimateColor("track", track, ui.Smooth)
	pos := box.AnimateTo("knob", target, ui.Snappy)
	knob := ui.RGBA(255, 255, 255, 1)
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(r, trackColor, r.H/2)
		radius := r.H/2 - miniKnobInset
		left, right := r.X+r.H/2, r.X+r.W-r.H/2
		p.Fill(circle(left+(right-left)*pos, r.Y+r.H/2, radius), knob, radius)
	})
}

// hintRing breathes a glow in color around the pill it is built in, to
// show where to start, for as long as it is built: the caller builds it
// only while the hint applies. It animates in Draw, so the breathing only
// repaints the ring, at hintFPS while it glows and not at all while it
// rests. The box reaches hintSpread past the pill on every side, where the
// glow is drawn.
func hintRing(c *ui.Context, color ui.Color) {
	ring := ui.Box(c).Key("hint").Absolute().Left(-hintSpread).Top(-hintSpread).Right(-hintSpread).Bottom(-hintSpread)
	ring.Draw(func(p *ui.Painter, r ui.Rect) {
		at := time.Duration(p.Now().UnixMilli()%hintCycle.Milliseconds()) * time.Millisecond
		if at >= hintBreath {
			p.After(hintCycle - at)
			return
		}
		p.AnimationFrameRate(hintFPS)
		v := breath(at)
		pill := ui.Rect{X: r.X + hintSpread, Y: r.Y + hintSpread, W: r.W - 2*hintSpread, H: r.H - 2*hintSpread}
		p.Shadow(pill, pill.H/2, 0, 0, 0, hintSpread*v, color.Alpha(hintAlpha*v))
	})
}

// breath is how far the hint ring glows at t into its breath, from 0 to 1:
// it eases up and back down over hintBreath.
func breath(t time.Duration) float32 {
	x := float64(t) / float64(hintBreath)
	return float32((1 - math.Cos(2*math.Pi*x)) / 2)
}

// statusToggle is a capsule button for something that is on or off, made to
// be found at a glance. Off, it is a plain outlined button with a grey
// switch; on, it turns green: a tinted fill and outline, and the switch
// slides on. A spinner takes the switch's place while the change runs,
// without dimming the button; tip says what it does.
func statusToggle(c *ui.Context, t *ui.Theme, spec pillSpec, tip string, onClick func()) ui.Element {
	b := ui.ButtonBase(c).Key(spec.label).Label(spec.label).Tooltip(tip).Height(switchHeight).Padding(0, 12, 0, 9).Gap(gapTight).
		Radius(switchHeight / 2).AlignItems(ui.Center).Justify(ui.Center).Disabled(spec.disabled)
	hovered := b.Hovered() && !spec.disabled && !spec.busy
	fill, line, weight, ink := t.Surface, t.Border, weightMedium, t.Text
	if onGlass(t) {
		// The design gives the header switches the same 38% button face as
		// the rest of the window; on keeps a green tint and ink.
		fill, line, ink = glassButton(t), glassMaterialLine(t), t.TextMuted
		if hovered {
			fill = glassFill(t, glassButtonHover)
		}
		if spec.on {
			fill, line, weight, ink = glassButton(t).Mix(t.Success, glassToggleOnTint), t.Success.Alpha(statusOnLineAlpha), weightSemibold, t.Success
		}
	} else {
		if hovered {
			fill = t.SurfaceHover
		}
		if spec.on {
			fill, line, weight = t.Success.Alpha(statusOnFillAlpha), t.Success.Alpha(statusOnLineAlpha), weightSemibold
			if hovered {
				fill = t.Success.Alpha(statusOnHoverAlpha)
			}
		}
	}
	b.Background(b.AnimateColor("fill", fill, ui.Smooth)).Border(1, b.AnimateColor("line", line, ui.Smooth))
	scale := float32(1)
	if b.Pressed() && !spec.busy {
		scale = pressedScale
	}
	b.AnimatePose(ui.Pose{Scale: scale}, ui.Snappy)
	if spec.disabled {
		b.Opacity(disabledOpacity)
	}
	if !spec.disabled && !spec.busy {
		b.Cursor(ui.CursorPointer)
	}
	b.OnClick(func() {
		if !spec.busy {
			onClick()
		}
	})
	b.Children(func() {
		if spec.hint {
			hintRing(c, t.Success)
		}
		// The switch is built in every state, hidden under the spinner while
		// a change runs, so that it keeps its motion state across the change
		// and slides on when the change is done.
		face := float32(1)
		if spec.busy {
			face = 0
		}
		ui.Box(c).Key("lead").Width(miniTrackW).Height(16).Center().Children(func() {
			ui.Box(c).Key("face").Opacity(face).Children(func() {
				miniSwitch(c, t, spec.on, t.Success)
			})
			if spec.busy {
				ui.Box(c).Absolute().Left((miniTrackW - iconInline) / 2).Top(2).Width(iconInline).Height(iconInline).Children(func() {
					spinner(c, iconInline, t.Success)
				})
			}
		})
		ui.Text(c, spec.label).FontSize(fontSecondary).FontWeight(weight).TextColor(ink).
			NoWrap().Translate(0, -opticalLift)
	})
	return b
}

// modePill is the pill of the i'th outbound mode.
func (d *Dashboard) modePill(c *ui.Context, t *ui.Theme, i int) ui.Element {
	on := d.modeIndex == i
	lead := func() { pillDotMark(c, t, on) }
	return togglePill(c, t, pillSpec{label: modeLabels()[i], on: on, disabled: d.modeLocked()}, lead, func() {
		if d.modeIndex == i {
			return
		}
		d.modeIndex = i
		d.setMode(service.Modes[i])
	})
}

// systemProxyPill turns the system proxy on and off. While another app holds
// the system proxy it cannot be turned on.
func (d *Dashboard) systemProxyPill(c *ui.Context, t *ui.Theme) ui.Element {
	s := &d.sysProxy
	taken := s.status.Owner == model.ProxyOwnerOther
	return togglePill(c, t, pillSpec{
		label: locales.SystemProxy(), on: s.on, busy: s.busy, hint: d.trafficHint(),
		disabled: !s.loaded || (taken && !s.on),
	}, func() { miniSwitch(c, t, s.on, t.Accent) }, func() { d.beginSystemProxy(!s.on) })
}

// tunPill turns TUN mode on and off. While another client's TUN holds the
// default routes it cannot be turned on.
func (d *Dashboard) tunPill(c *ui.Context, t *ui.Theme) ui.Element {
	s := &d.tun
	return togglePill(c, t, pillSpec{
		label: locales.Tun(), on: s.on, busy: s.busy,
		disabled: !s.loaded || d.controlsLocked() || (s.taken && !s.on),
	}, func() { miniSwitch(c, t, s.on, t.Accent) }, func() { d.beginTun(!s.on) })
}

// trafficHint says whether to point at the system proxy switch: the core
// runs, yet neither the system proxy nor TUN sends traffic to it, and no
// other client holds either, so Hop is doing nothing for the user until one
// is turned on.
func (d *Dashboard) trafficHint() bool {
	sp, tun := &d.sysProxy, &d.tun
	return d.status != statusFailed && sp.loaded && !sp.on && !sp.busy && !tun.on && !tun.busy &&
		sp.status.Owner != model.ProxyOwnerOther && !tun.taken
}
