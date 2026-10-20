package handler

import (
	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
)

const (
	pillDot          float32 = 7
	pillOnFillAlpha  float32 = 0.08
	pillOnLineAlpha  float32 = 0.45
	pillOffDotAlpha  float32 = 0.35
	pillDotGlowAlpha float32 = 0.22
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
	// outline, so the choices that are on stand out of the row.
	fill, line, dot, ink := t.Text.Alpha(softFillAlpha), ui.Transparent, t.TextMuted.Alpha(pillOffDotAlpha), t.TextMuted
	if spec.on {
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

// modePill is the pill of the i'th outbound mode. It waits for the core to
// report its mode and for any subscription mutation to finish, so an apply
// cannot carry the mode the user just left back in.
func (d *Dashboard) modePill(c *ui.Context, t *ui.Theme, i int) ui.Element {
	disabled := d.view.Mode == "" || d.subscription.mutations > 0 || d.core.busy
	return togglePill(c, t, pillSpec{label: modeLabels[i], on: d.modeIndex == i, disabled: disabled}, func() {
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
		label: "系统代理", on: s.on, busy: s.busy,
		disabled: !s.loaded || (taken && !s.on),
	}, func() { d.beginSystemProxy(!s.on) })
}
