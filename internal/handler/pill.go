package handler

import (
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
	statusOffDotAlpha  float32 = 0.55
)

// pillSpec is what a toggle pill shows and whether it takes clicks now.
type pillSpec struct {
	label    string
	on       bool
	disabled bool
	// busy shows a spinner in place of the dot while the change runs.
	busy bool
}

// togglePill is a capsule with a dot that lights in the accent while its
// choice is on: the outbound modes, one of which is always on, and the
// system proxy, which is on or off. Its fill, outline and dot ease between
// the two.
func togglePill(c *ui.Context, t *ui.Theme, spec pillSpec, onClick func()) ui.Element {
	b := ui.ButtonBase(c).Key(spec.label).Label(spec.label).Height(switchHeight).Padding(0, 12).Gap(gapTight).
		Radius(switchHeight / 2).AlignItems(ui.Center).Justify(ui.Center).Disabled(spec.disabled || spec.busy)
	// Off, a pill is only a faint fill; on, it takes the accent's tint and
	// outline, so the choices that are on stand out of the row. On glass
	// both sit on a near-opaque backing, so their text never meets the
	// desktop.
	fill, line, dot, ink := t.Text.Alpha(softFillAlpha), ui.Transparent, t.TextMuted.Alpha(pillOffDotAlpha), t.TextMuted
	if onGlass(t) {
		backing := controlBacking(t)
		fill, line = backing, t.Text.Alpha(0.24)
		if spec.on {
			fill, line, dot, ink = t.Accent.Alpha(pillOnFillAlpha).Over(backing), t.Accent.Alpha(pillOnLineAlpha), t.Accent, t.Text
		} else if b.Hovered() && !spec.disabled {
			fill = backing.Mix(t.Text, softFillAlpha)
		}
	} else if spec.on {
		fill, line, dot, ink = t.Accent.Alpha(pillOnFillAlpha), t.Accent.Alpha(pillOnLineAlpha), t.Accent, t.Text
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
	dotColor := b.AnimateColor("dot", dot, ui.Smooth)
	b.Children(func() {
		if spec.busy {
			spinner(c, iconInline, t.Accent)
		} else {
			glow := ui.Transparent
			if spec.on {
				glow = t.Accent.Alpha(pillDotGlowAlpha)
			}
			ui.Box(c).Key("dot").Width(pillDot).Height(pillDot).Radius(pillDot/2).Background(dotColor).
				Shadow(0, 0, 0, 3, glow)
		}
		ui.Text(c, spec.label).FontSize(fontSecondary).FontWeight(weightMedium).TextColor(ink).
			NoWrap().Translate(0, -opticalLift)
	})
	return b
}

// statusToggle is a capsule button for something that is on or off, made to
// be found at a glance. Off, it is a plain outlined button with a grey dot;
// on, it turns green: a tinted fill and outline, and a dot that ripples once
// as the core takes the traffic. A spinner takes the dot's place while the
// change runs, without dimming the button; tip says what it does.
func statusToggle(c *ui.Context, t *ui.Theme, spec pillSpec, tip string, onClick func()) ui.Element {
	b := ui.ButtonBase(c).Key(spec.label).Label(spec.label).Tooltip(tip).Height(switchHeight).Padding(0, 12, 0, 8).Gap(2).
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
		// The dot is built in every state, hidden under the spinner while a
		// change runs, so that it keeps its motion state across the change and
		// ripples once when the switch turns on.
		dotColor := t.Success
		if !spec.on {
			dotColor = t.TextMuted.Alpha(statusOffDotAlpha)
		}
		if spec.busy {
			dotColor = dotColor.Alpha(0)
		}
		ui.Box(c).Key("dot").Width(16).Height(16).Center().Children(func() {
			statusDot(c, dotColor, spec.on && !spec.busy)
			if spec.busy {
				ui.Box(c).Absolute().Left(2).Top(2).Width(iconInline).Height(iconInline).Children(func() {
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
	return togglePill(c, t, pillSpec{label: modeLabels()[i], on: d.modeIndex == i, disabled: d.modeLocked()}, func() {
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
		label: locales.SystemProxy(), on: s.on, busy: s.busy,
		disabled: !s.loaded || (taken && !s.on),
	}, func() { d.beginSystemProxy(!s.on) })
}

// tunPill turns TUN mode on and off. While another client's TUN holds the
// default routes it cannot be turned on.
func (d *Dashboard) tunPill(c *ui.Context, t *ui.Theme) ui.Element {
	s := &d.tun
	return togglePill(c, t, pillSpec{
		label: locales.Tun(), on: s.on, busy: s.busy,
		disabled: !s.loaded || d.controlsLocked() || (s.taken && !s.on),
	}, func() { d.beginTun(!s.on) })
}
