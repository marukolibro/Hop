package handler

import (
	"math"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"

	"hop/internal/model"
	"hop/internal/service"
)

// Motion in Hop is for what changes: the switch slides to the new choice,
// numbers roll, and packets flow along the route as fast as traffic does.
// Drawings that move for as long as they show (packets, pulses, spinners)
// only repaint, so a large page is not built again every frame.

const (
	switchHeight float32 = 28
	switchInset  float32 = 2
	pulsePeriod          = 2 * time.Second
	// statusTextMinWidth is the window width from which the status capsule
	// spells out what it does.
	statusTextMinWidth float32 = 720
	entranceTime               = 420 * time.Millisecond
	entranceStep               = 45 * time.Millisecond
	entranceShift      float32 = 10

	// opticalLift raises text in small controls: CJK glyphs sit about a
	// pixel below the centre of the line box the font's metrics give.
	opticalLift float32 = 1

	flowHeight     float32 = 34
	flowStopR      float32 = 5
	flowExitR      float32 = 7
	flowLane       float32 = 3.5
	flowMaxPackets         = 7
	flowBaseSpeed  float32 = 70
)

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

// pageSwitchStyle is the quiet switch between pages and between choices.
func pageSwitchStyle(t *ui.Theme, segmentWidth float32) switchStyle {
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

// statusDot draws a dot in color; while live, a ring ripples out of it
// every pulsePeriod, as a heartbeat of the core.
func statusDot(c *ui.Context, color ui.Color, live bool) {
	ui.Box(c).Width(16).Height(16).Draw(func(p *ui.Painter, r ui.Rect) {
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		if live {
			phase := float32(p.Now().UnixMilli()%pulsePeriod.Milliseconds()) / float32(pulsePeriod.Milliseconds())
			ripple := 3.5 + 4.5*easeOut(phase)
			p.Fill(circle(cx, cy, ripple), color.Alpha(0.35*(1-phase)), ripple)
			p.AnimationFrame()
		}
		p.Fill(circle(cx, cy, 3.5), color, 3.5)
	})
}

// statusCapsule's dot says whether Hop carries the system's traffic, or
// that the core cannot be reached. While the core runs, the capsule copies
// the command that sends a terminal's traffic through Hop.
func (d *Dashboard) statusCapsule(c *ui.Context, t *ui.Theme) {
	live := d.status == statusConnected
	capsule := ui.Row(c).Key("status").Gap(gapTight).AlignItems(ui.Center).Height(switchHeight).Padding(0, 12, 0, 6).
		Radius(switchHeight/2).Background(t.Surface).Border(1, t.Border)
	if live {
		capsule.Label("复制终端代理").Cursor(ui.CursorPointer)
		if capsule.Hovered() {
			capsule.Background(t.SurfaceHover)
		}
		capsule.OnClick(func() {
			c.WriteClipboard(service.TerminalProxyCommand(runtime.GOOS, d.proxyAddress))
			c.Toast("已复制终端代理命令,粘贴到终端即可")
		})
	}
	capsule.Children(func() {
		statusDot(c, d.statusColor(t), d.proxying())
		// A narrow window drops the words, leaving the dot and the icon.
		width, _ := c.Size()
		if live {
			ui.Icon(c, iconTerminal).Width(iconInline + 1).Height(iconInline + 1).TextColor(t.TextMuted)
		}
		if width < statusTextMinWidth {
			return
		}
		label := d.statusText()
		if live {
			label = "复制终端代理"
		}
		ui.Text(c, label).FontSize(fontSecondary).FontWeight(weightMedium).NoWrap().Translate(0, -opticalLift)
	})
}

// routeFlow draws the route as a line of stops, from this Mac through the
// groups to the exit, with packets flowing along it: download back from
// the exit, upload out to it, more and faster as traffic grows. Names sit
// under the stops.
func routeFlow(c *ui.Context, t *ui.Theme, route []model.RouteHop, traffic model.Traffic) {
	labels := flowLabels(route)
	line, up, paper, muted := t.Accent, t.Warning, t.Surface, t.TextMuted
	downN, downSpeed := packetFlow(traffic.Down)
	upN, upSpeed := packetFlow(traffic.Up)
	ui.Column(c).Gap(4).Children(func() {
		ui.Box(c).Height(flowHeight).Label("线路").Description(strings.Join(labels, " → ")).Draw(func(p *ui.Painter, r ui.Rect) {
			n := len(labels)
			cy := r.Y + r.H/2
			stopX := func(i int) float32 { return r.X + (float32(i)+0.5)*r.W/float32(n) }
			x0, x1 := stopX(0), stopX(n-1)
			p.Line(x0, cy, x1, cy, 2, line.Alpha(0.3))
			now := float32(p.Now().UnixMilli()%100000) / 1000
			span := x1 - x0
			for k := range downN {
				f := fract(now*downSpeed/span + float32(k)/float32(downN))
				packet(p, x1-f*span, cy-flowLane, line)
			}
			for k := range upN {
				f := fract(now*upSpeed/span + float32(k)/float32(upN))
				packet(p, x0+f*span, cy+flowLane, up)
			}
			for i := range n {
				x := stopX(i)
				switch {
				case i == n-1:
					glow := 0.18 + 0.1*float32(math.Sin(float64(now)*2.4))
					p.Fill(circle(x, cy, flowExitR+5), line.Alpha(glow), flowExitR+5)
					p.Fill(circle(x, cy, flowExitR), line, flowExitR)
				case i == 0:
					p.Fill(circle(x, cy, flowStopR), paper, flowStopR)
					p.Stroke(circle(x, cy, flowStopR), muted, flowStopR, 2)
				default:
					p.Fill(circle(x, cy, flowStopR), paper, flowStopR)
					p.Stroke(circle(x, cy, flowStopR), line, flowStopR, 2)
				}
			}
			p.AnimationFrame()
		})
		ui.Row(c).Children(func() {
			for i, label := range labels {
				text := ui.Text(c, label).Key(i).FontSize(fontCaption).MaxLines(1).TextAlign(ui.Center).Grow(1).Basis(0)
				if i == len(labels)-1 {
					text.TextColor(t.Accent).FontWeight(weightSemibold)
				} else {
					text.TextColor(t.TextMuted)
				}
			}
		})
	})
}

// flowLabels names the stops of a route: this Mac, the groups, the exit.
func flowLabels(route []model.RouteHop) []string {
	labels := []string{"本机"}
	for _, hop := range route {
		if hop.Group {
			labels = append(labels, hop.Name)
		}
	}
	return append(labels, "出口")
}

// packetFlow turns a throughput into how many packets travel the line at
// once and how fast, in DIPs per second; nothing moves while idle.
func packetFlow(bytesPerSecond int64) (int, float32) {
	if bytesPerSecond < idleRate {
		return 0, 0
	}
	level := math.Log2(float64(bytesPerSecond) / idleRate)
	n := min(flowMaxPackets, 1+int(level/2))
	return n, flowBaseSpeed + float32(level)*14
}

func packet(p *ui.Painter, x float32, y float32, color ui.Color) {
	p.Fill(circle(x, y, 4.5), color.Alpha(0.22), 4.5)
	p.Fill(circle(x, y, 2), color, 2)
}

// hopMark draws Hop's mark in ink, as the menu bar icon shows it: a ball
// bouncing twice and landing. The arcs blend from ink's first colour at the
// left to its second at the right, where the ball lands in it. Small dots
// give the arcs the round ends the path's own ends lack.
func hopMark(c *ui.Context, size float32, ink markInk) {
	ui.Box(c).Width(size).Height(size).Draw(func(p *ui.Painter, r ui.Rect) {
		scale := r.W / markSize
		at := func(m markPoint) (float32, float32) {
			return r.X + float32(m.x)*scale, r.Y + float32(m.y)*scale
		}
		inkAt := func(x float32) ui.Color { return ink.from.Mix(ink.to, (x-r.X)/r.W) }
		stroke := float32(markStroke) * scale
		path := &ui.Path{}
		for _, arc := range markArcs {
			a, control, end := arc.trimmed()
			ax, ay := at(a)
			cx, cy := at(control)
			ex, ey := at(end)
			path.MoveTo(ax, ay).QuadTo(cx, cy, ex, ey)
			p.Fill(circle(ax, ay, stroke/2), inkAt(ax), stroke/2)
			p.Fill(circle(ex, ey, stroke/2), inkAt(ex), stroke/2)
		}
		p.StrokePathGradient(path, stroke, ui.LinearGradient{From: ink.from, To: ink.to, Angle: 90})
		bx, by := at(markBall)
		ballR := float32(markBallR) * scale
		p.Fill(circle(bx, by, ballR), inkAt(bx), ballR)
	})
}

// entrance plays once, as the element first shows: it rises into place and
// fades in, step entrances after the first.
func entrance(e ui.Element, step int) ui.Element {
	v := e.Keyframes("entrance", []ui.Keyframe{{At: 0, Value: 0}, {At: entranceTime, Value: 1, Ease: ui.EaseOut}},
		ui.KeyframeOptions{Delay: time.Duration(step) * entranceStep})
	return e.Opacity(v).Translate(0, (1-v)*entranceShift)
}

// spinner turns while something runs.
func spinner(c *ui.Context, size float32, color ui.Color) {
	ui.Box(c).Width(size).Height(size).Draw(func(p *ui.Painter, r ui.Rect) {
		turn := float64(p.Now().UnixMilli()%900) / 900 * 2 * math.Pi
		cx, cy, rad := r.X+r.W/2, r.Y+r.H/2, r.W/2-1.5
		for k := range 8 {
			a := turn + float64(k)*math.Pi/4
			x, y := cx+rad*float32(math.Cos(a)), cy+rad*float32(math.Sin(a))
			p.Fill(circle(x, y, 1.4), color.Alpha(float32(k+1)/8), 1.4)
		}
		p.AnimationFrame()
	})
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
			From: down.Alpha(0.24), To: down.Alpha(0), Angle: 180,
		})
		p.StrokePath(chart.Line(r, xs, upYs, chart.CurveMonotone), 1.25, up)
		p.StrokePath(chart.Line(r, xs, downYs, chart.CurveMonotone), 1.5, down)
	})
}

func circle(cx float32, cy float32, radius float32) ui.Rect {
	return ui.Rect{X: cx - radius, Y: cy - radius, W: 2 * radius, H: 2 * radius}
}

func fract(v float32) float32 {
	return v - float32(math.Floor(float64(v)))
}

func easeOut(v float32) float32 {
	return 1 - (1-v)*(1-v)
}
