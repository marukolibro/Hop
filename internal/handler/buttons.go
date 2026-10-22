package handler

import "github.com/egoist/mygo/ui"

const (
	buttonIconSize   float32 = 14
	pressedScale     float32 = 0.96
	disabledOpacity  float32 = 0.45
	softFillAlpha    float32 = 0.06
	softHoverAlpha   float32 = 0.11
	dangerFillAlpha  float32 = 0.1
	dangerHoverAlpha float32 = 0.18
	// disabledFillAlpha is the neutral wash of a disabled button: quiet, but
	// on the same surface as the rest of the window, never the accent at low
	// opacity.
	disabledFillAlpha float32 = 0.10
)

// buttonStyle colours a button at rest and under the pointer.
type buttonStyle struct {
	fill    ui.Color
	hover   ui.Color
	ink     ui.Color
	outline bool
}

// primaryButton is the one action a view leads to; softButton every other
// action; dangerButton one that cuts something off.
func primaryButton(t *ui.Theme) buttonStyle {
	return buttonStyle{fill: t.Accent, hover: t.AccentHover, ink: t.AccentText, outline: true}
}

func softButton(t *ui.Theme) buttonStyle {
	return buttonStyle{fill: t.Text.Alpha(softFillAlpha), hover: t.Text.Alpha(softHoverAlpha), ink: t.Text}
}

func dangerButton(t *ui.Theme) buttonStyle {
	return buttonStyle{fill: t.Danger.Alpha(dangerFillAlpha), hover: t.Danger.Alpha(dangerHoverAlpha), ink: textOn(t, t.Danger)}
}

// buttonSpec is what a button shows and whether it takes clicks now.
type buttonSpec struct {
	label    string
	icon     *ui.SVG
	style    buttonStyle
	disabled bool
	// busy shows a spinner in place of the icon while the action runs.
	busy bool
}

// button is Hop's button: a capsule as tall as the switches, its icon and
// label lifted to the optical centre. Its fill eases in under the pointer
// and it gives a little as it is pressed. It keeps ButtonBase's keyboard,
// focus and accessibility.
func button(c *ui.Context, spec buttonSpec, onClick func()) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Key(spec.label).Label(spec.label).Height(switchHeight).Padding(0, 12).Gap(5).
		Radius(switchHeight / 2).Disabled(spec.disabled || spec.busy)
	// A disabled button keeps its shape and takes a neutral face with the
	// secondary ink: legible, plainly inactive, with no accent and no
	// transparency over the page.
	fill, ink, outline := spec.style.fill, spec.style.ink, spec.style.outline
	switch {
	case spec.disabled:
		fill, ink, outline = t.Text.Alpha(disabledFillAlpha), t.TextMuted, false
	case b.Hovered():
		fill = spec.style.hover
	}
	b.Background(b.AnimateColor("fill", fill, ui.Smooth))
	if outline {
		b.Border(1, glassMaterialLine(t)).Shadow(0, 2, 8, 0, glassButtonShadow(t))
	}
	scale := float32(1)
	if b.Pressed() {
		scale = pressedScale
	}
	b.AnimatePose(ui.Pose{Scale: scale}, ui.Snappy)
	if !spec.disabled && !spec.busy {
		b.Cursor(ui.CursorPointer)
	}
	b.OnClick(onClick)
	b.Children(func() {
		switch {
		case spec.busy:
			spinner(c, buttonIconSize, ink)
		case spec.icon != nil:
			ui.Icon(c, spec.icon).Width(buttonIconSize).Height(buttonIconSize).TextColor(ink)
		}
		ui.Text(c, spec.label).FontSize(fontSecondary).FontWeight(weightMedium).TextColor(ink).
			NoWrap().Translate(0, -opticalLift)
	})
	return b
}

// textButton is the design's quiet text action, with an optional icon. It
// has no capsule or fill; the ink turns accent under the pointer.
func textButton(c *ui.Context, t *ui.Theme, label string, icon *ui.SVG, disabled bool, busy bool, onClick func()) ui.Element {
	b := ui.ButtonBase(c).Key(label).Label(label).Height(switchHeight).Padding(0).Gap(6).
		Radius(0).Background(ui.Transparent).Disabled(disabled || busy)
	ink := t.TextMuted
	if b.Hovered() && !disabled {
		ink = accentInk(t)
	}
	if !disabled && !busy {
		b.Cursor(ui.CursorPointer)
	}
	b.OnClick(onClick)
	b.Children(func() {
		switch {
		case busy:
			spinner(c, iconInline, t.Accent)
		case icon != nil:
			ui.Icon(c, icon).Width(buttonIconSize).Height(buttonIconSize).TextColor(ink)
		}
		ui.Text(c, label).FontSize(fontSecondary).FontWeight(weightMedium).TextColor(ink).
			NoWrap().Translate(0, -opticalLift)
	})
	return b
}

// iconButton is a round button with only an icon, for actions repeated on
// every row; label names it for assistive technology.
func iconButton(c *ui.Context, label string, icon *ui.SVG, style buttonStyle, onClick func()) ui.Element {
	const size float32 = 24
	b := ui.ButtonBase(c).Key(label).Label(label).Width(size).Height(size).Radius(size / 2).Cursor(ui.CursorPointer)
	fill := ui.Transparent
	if b.Hovered() {
		fill = style.fill
	}
	b.Background(b.AnimateColor("fill", fill, ui.Smooth))
	b.OnClick(onClick)
	b.Children(func() {
		ui.Icon(c, icon).Width(12).Height(12).TextColor(style.ink)
	})
	return b
}

// iconMenuButton is iconButton that opens a menu instead of acting right away;
// build fills the menu, label names the button for assistive technology, and
// disabled keeps the menu from opening.
func iconMenuButton(c *ui.Context, label string, icon *ui.SVG, style buttonStyle, disabled bool, build func(m *ui.Menu)) ui.Element {
	const size float32 = 24
	b := ui.ButtonBase(c).Key(label).Label(label).Width(size).Height(size).Radius(size / 2).Disabled(disabled)
	fill := ui.Transparent
	if b.Hovered() && !disabled {
		fill = style.fill
	}
	b.Background(b.AnimateColor("fill", fill, ui.Smooth))
	if !disabled {
		b.Cursor(ui.CursorPointer)
	}
	b.Menu(build)
	b.Children(func() {
		ui.Icon(c, icon).Width(iconInline).Height(iconInline).TextColor(style.ink)
	})
	return b
}
