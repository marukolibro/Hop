package handler

import (
	"math"
	"strings"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/locales"
)

const (
	// exitWidth fits the exit's public address on one line.
	exitWidth = 235
	// gaugeGlyphScale sizes the flag, or the globe standing in for one, to
	// sit inside the rings.
	gaugeGlyphScale float32 = 0.32
	// gaugeArcStep is the angle between the points a ring is drawn from,
	// fine enough that the polyline reads as an arc.
	gaugeArcStep float32 = 0.06
	// modeSegmentWidth fits a two-character mode name.
	modeSegmentWidth = 56
)

// The gauge rings open at the bottom: from the lower left, clockwise
// through the top, 270 degrees to the lower right.
const (
	gaugeStart = 135 * math.Pi / 180
	gaugeSweep = 270 * math.Pi / 180
)

// modeHints say what each of modeLabels does, index for index; the mode
// switch carries the current one as its tooltip, no text on the row.
func modeHints() []string {
	return []string{locales.ModeHintRule(), locales.ModeHintGlobal(), locales.ModeHintDirect()}
}

// routeGauge draws the exit as two rings: the outer sweeps with the download,
// the inner with the upload, each over a faint track, and both ease to the
// traffic and then rest. The exit's flag sits in the middle, or a globe when
// it has none: a group, DIRECT, or one of a provider's plan notes.
func routeGauge(c *ui.Context, t *ui.Theme, hop model.RouteHop, size float32, traffic model.Traffic) ui.Element {
	gauge := ui.Box(c).Key("gauge").Width(size).Height(size).Center()
	down := gauge.AnimateTo("down", flowIntensity(traffic.Down), ui.Smooth)
	up := gauge.AnimateTo("up", flowIntensity(traffic.Up), ui.Smooth)
	gauge.Draw(func(p *ui.Painter, r ui.Rect) {
		drawGauge(p, r, down, up, t)
	})
	gauge.Children(func() {
		glyph := size * gaugeGlyphScale
		if flag := shownFlag(hop.Node.Flag); flag != "" && !hop.Group {
			ui.Text(c, flag).FontSize(glyph)
			return
		}
		ui.Icon(c, iconGlobe).Width(glyph).Height(glyph).TextColor(t.TextMuted)
	})
	return gauge
}

func drawGauge(p *ui.Painter, r ui.Rect, down float32, up float32, t *ui.Theme) {
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	outer, inner := r.W*0.43, r.W*0.32
	outerWidth, innerWidth := r.W*0.075, r.W*0.065
	track := t.Text.Alpha(neutralLineAlpha)
	p.StrokePath(gaugeArc(cx, cy, outer, gaugeStart, gaugeSweep), outerWidth, track)
	p.StrokePath(gaugeArc(cx, cy, inner, gaugeStart, gaugeSweep), innerWidth, track)
	if down > 0.001 {
		p.StrokePath(gaugeArc(cx, cy, outer, gaugeStart, gaugeSweep*down), outerWidth, t.Accent)
	}
	if up > 0.001 {
		p.StrokePath(gaugeArc(cx, cy, inner, gaugeStart, gaugeSweep*up), innerWidth, uploadColor(t))
	}
}

// gaugeArc is a ring segment from start over sweep radians, clockwise in
// screen coordinates.
func gaugeArc(cx float32, cy float32, radius float32, start float32, sweep float32) *ui.Path {
	at := func(angle float32) (float32, float32) {
		return cx + radius*float32(math.Cos(float64(angle))), cy + radius*float32(math.Sin(float64(angle)))
	}
	path := &ui.Path{}
	x, y := at(start)
	path.MoveTo(x, y)
	end := start + sweep
	for angle := start + gaugeArcStep; angle < end; angle += gaugeArcStep {
		x, y = at(angle)
		path.LineTo(x, y)
	}
	x, y = at(end)
	path.LineTo(x, y)
	return path
}

// exitNode names the node traffic leaves by and adds its details, for the
// menu bar panel, which has no exit IP line.
func exitNode(c *ui.Context, t *ui.Theme, route []model.RouteHop) {
	exitIdentity(c, t, route)
	exitDetails(c, t, route)
}

// exitIdentity names the exit and the groups it passes on the way.
func exitIdentity(c *ui.Context, t *ui.Theme, route []model.RouteHop) {
	hop := route[len(route)-1]
	node := hop.Node
	switch {
	case hop.Group:
		ui.Text(c, hop.Name).FontSize(fontHeadline).FontWeight(weightSemibold)
	case node.PlanNote:
		headline(c, t, locales.PlanInfoNode(), node.Delay)
	case node.Via != "":
		headline(c, t, locales.RouteDirect(), node.Delay)
	default:
		headline(c, t, strings.TrimSpace(shownFlag(node.Flag)+" "+node.Region+" "+node.Title), node.Delay)
	}
	if via := routeVia(route); via != "" {
		mutedText(c, t, via).MaxLines(1)
	}
}

// exitDetails names the exit's line, multiplier and latency, below the exit
// identity and, on the nodes page, the exit IP line.
func exitDetails(c *ui.Context, t *ui.Theme, route []model.RouteHop) {
	hop := route[len(route)-1]
	node := hop.Node
	if hop.Group {
		return
	}
	switch {
	case node.PlanNote:
		mutedText(c, t, locales.RoutePlanNote(node.Name)).MaxLines(1)
	case node.Via != "":
		mutedText(c, t, node.Via)
	default:
		labels := nodeLabels(node)
		if len(labels) > 0 || node.Multiplier != "" {
			ui.Row(c).Gap(gapTight).Children(func() {
				for _, label := range labels {
					nodeChip(c, t, label)
				}
				if node.Multiplier != "" {
					nodeChip(c, t, node.Multiplier)
				}
			})
		}
	}
}

// routeVia names the groups between this Mac and the exit, the line under
// its name; "" when the route passes none.
func routeVia(route []model.RouteHop) string {
	var groups []string
	for _, hop := range route[:len(route)-1] {
		if hop.Group {
			groups = append(groups, shownName(hop.Name))
		}
	}
	if len(groups) == 0 {
		return ""
	}
	return locales.RouteVia(strings.Join(groups, " › "))
}

func headline(c *ui.Context, t *ui.Theme, text string, delay model.Delay) {
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, text).FontSize(fontHeadline).FontWeight(weightSemibold).LetterSpacing(-0.2).MaxLines(1).ContentTransition(ui.ContentTransition{})
		heroDelay(c, t, delay)
	})
}
