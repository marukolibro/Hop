package handler

import (
	"github.com/egoist/mygo/ui"
)

// The sea wallpaper is the design page's desktop painted into the window:
// a gradient sky, one soft glow and two tilted blobs, in the page's own
// proportions, so the glass in front of it blurs the same shapes and colours.
// Positions are in the design's viewport units (1261x826 CSS px around a
// 1160x714 window at 50.5,75) and scale with the window.
const (
	seaViewportW float32 = 1261
	seaViewportH float32 = 826
	seaWindowX   float32 = 50.5
	seaWindowY   float32 = 75
	seaWindowW   float32 = 1160
	seaWindowH   float32 = 714
	// seaSaturation is the page's saturate(135%) on its blurred backdrop,
	// applied to every wallpaper colour because the blur cannot do it here.
	seaSaturation float32 = 1.35
	seaCornerPct  float32 = 0.48
	// seaLightLift mixes the light wallpaper toward white so its blues sit
	// softer behind the glass than the design page's; the dark wallpaper
	// keeps its depth.
	seaLightLift float32 = 0.30
	// seaLightBlobA and seaLightBlobB are the opacities of the two light
	// blobs: below the design page's 1 and 0.78 so the deep blue lobes in
	// front of the glass stay a soft sky-blue.
	seaLightBlobA float32 = 0.55
	seaLightBlobB float32 = 0.50
	seaBlobSteps          = 6
)

// seaStop is a colour stop: 0xRRGGBB, its alpha, its place along the line.
type seaStop struct {
	rgb   uint32
	alpha float32
	at    float32
}

// seaBlob is one tilted rounded shape: its size and place in viewport units
// (width units for the sizes and the left edge, height units for the top),
// its tilt in degrees, gradient angle and stops, and the opacity it has.
type seaBlob struct {
	widthVW, heightVW, leftVW, topVH float32
	tilt, angle                      float32
	opacity                          float32
	stops                            []seaStop
}

// seaGlow is a soft light fading out from a point of the viewport, as CSS's
// radial-gradient(ellipse at x y, colour 0%, transparent reach) does.
type seaGlow struct {
	atX, atY float32
	reach    float32
	rgb      uint32
}

type seaWallpaper struct {
	// lift is how far every colour is mixed toward white, from 0 to 1.
	lift     float32
	skyAngle float32
	sky      []seaStop
	glow     seaGlow
	blobs    []seaBlob
}

func seaLight() seaWallpaper {
	return seaWallpaper{
		lift:     seaLightLift,
		skyAngle: 128,
		sky:      []seaStop{{0xA5C2EF, 1, 0}, {0xBAC9E3, 1, 0.35}, {0xC9D7E5, 1, 0.57}, {0xB2CADF, 1, 1}},
		glow:     seaGlow{atX: 0.94, atY: 0.06, reach: 0.47, rgb: 0xEDF3FF},
		blobs: []seaBlob{
			{
				widthVW: 120, heightVW: 85, leftVW: -33, topVH: 15, tilt: -29, angle: 165, opacity: seaLightBlobA,
				stops: []seaStop{{0xF9FCFF, 0.68, 0.08}, {0xDBE8FF, 0.56, 0.32}, {0x789FDA, 1, 0.54}, {0x8AA9D9, 1, 0.59}, {0xB8CFE9, 1, 0.67}, {0xE0EFFA, 0.65, 0.82}},
			},
			{
				widthVW: 80, heightVW: 75, leftVW: 46, topVH: 12, tilt: 32, angle: 130, opacity: seaLightBlobB,
				stops: []seaStop{{0xD7DDF0, 0, 0.07}, {0xCFDCF2, 0.69, 0.30}, {0xC8C8ED, 1, 0.44}, {0x8DAFE3, 1, 0.59}, {0x88B7DF, 1, 0.65}, {0xC6DBE9, 1, 0.82}},
			},
		},
	}
}

func seaDark() seaWallpaper {
	return seaWallpaper{
		skyAngle: 135,
		sky:      []seaStop{{0x152339, 1, 0}, {0x1C3452, 1, 0.46}, {0x284966, 1, 1}},
		glow:     seaGlow{atX: 0.80, atY: 0.10, reach: 0.53, rgb: 0x254875},
		blobs: []seaBlob{
			{
				widthVW: 120, heightVW: 85, leftVW: -33, topVH: 15, tilt: -29, angle: 165, opacity: 0.72,
				stops: []seaStop{{0x5784B9, 0.27, 0.08}, {0x385E9C, 1, 0.32}, {0x254376, 1, 0.54}, {0x3E68A0, 1, 0.59}, {0x6D89AD, 1, 0.67}, {0x375A7E, 1, 0.82}},
			},
			{
				widthVW: 80, heightVW: 75, leftVW: 46, topVH: 12, tilt: 32, angle: 130, opacity: 0.64,
				stops: []seaStop{{0x354275, 0, 0.07}, {0x655B9A, 0.53, 0.30}, {0x5D5E9A, 1, 0.44}, {0x304981, 1, 0.59}, {0x36628B, 1, 0.65}, {0x5984A2, 1, 0.82}},
			},
		},
	}
}

// seaColor turns a stop into a colour with the wallpaper's saturation, lifted
// toward white by lift, its alpha scaled by opacity.
func seaColor(rgb uint32, alpha float32, opacity float32, lift float32) ui.Color {
	r, g, b := float32(rgb>>16&0xFF), float32(rgb>>8&0xFF), float32(rgb&0xFF)
	luma := 0.2126*r + 0.7152*g + 0.0722*b
	saturate := func(v float32) uint8 {
		v = min(255, max(0, luma+(v-luma)*seaSaturation))
		return uint8(v + (255-v)*lift + 0.5)
	}
	return ui.RGBA(saturate(r), saturate(g), saturate(b), alpha*opacity)
}

func seaGradient(angle float32, stops []seaStop, opacity float32, lift float32) ui.LinearGradient {
	out := make([]ui.GradientStop, len(stops))
	for i, s := range stops {
		out[i] = ui.GradientStop{Color: seaColor(s.rgb, s.alpha, opacity, lift), At: s.at}
	}
	return ui.LinearGradient{Angle: angle, Stops: out}
}

// seaRoundedPath is a rect whose corners are ellipses of pct of its sides,
// as CSS's border-radius in percent draws it.
func seaRoundedPath(r ui.Rect, pct float32) *ui.Path {
	const kappa = 0.5523
	rx, ry := r.W*pct, r.H*pct
	x0, y0, x1, y1 := r.X, r.Y, r.X+r.W, r.Y+r.H
	path := &ui.Path{}
	path.MoveTo(x0+rx, y0).LineTo(x1-rx, y0)
	path.CubeTo(x1-rx+rx*kappa, y0, x1, y0+ry-ry*kappa, x1, y0+ry)
	path.LineTo(x1, y1-ry)
	path.CubeTo(x1, y1-ry+ry*kappa, x1-rx+rx*kappa, y1, x1-rx, y1)
	path.LineTo(x0+rx, y1)
	path.CubeTo(x0+rx-rx*kappa, y1, x0, y1-ry+ry*kappa, x0, y1-ry)
	path.LineTo(x0, y0+ry)
	path.CubeTo(x0, y0+ry-ry*kappa, x0+rx-rx*kappa, y0, x0+rx, y0)
	return path.Close()
}

// drawSeaWallpaper paints the sea wallpaper behind the window rect r.
func drawSeaWallpaper(p *ui.Painter, r ui.Rect, dark bool) {
	wall := seaLight()
	if dark {
		wall = seaDark()
	}
	sx, sy := r.W/seaWindowW, r.H/seaWindowH
	view := ui.Rect{X: r.X - seaWindowX*sx, Y: r.Y - seaWindowY*sy, W: seaViewportW * sx, H: seaViewportH * sy}
	p.Clip(r, 0, func() {
		p.FillGradient(view, seaGradient(wall.skyAngle, wall.sky, 1, wall.lift), 0)
		drawSeaGlow(p, view, wall.glow, wall.lift)
		vw, vh := view.W/100, view.H/100
		for _, blob := range wall.blobs {
			box := ui.Rect{X: view.X + blob.leftVW*vw, Y: view.Y + blob.topVH*vh, W: blob.widthVW * vw, H: blob.heightVW * vw}
			p.Transform(ui.Rotation(blob.tilt), box.X+box.W/2, box.Y+box.H/2, func() {
				p.FillPathGradient(seaRoundedPath(box, seaCornerPct), seaGradient(blob.angle, blob.stops, blob.opacity, wall.lift))
			})
		}
	})
}

// drawSeaGlow stacks soft ellipses to fade a light out from a point; the
// ellipse reaches farthest-corner times reach of the viewport, as CSS sizes it.
func drawSeaGlow(p *ui.Painter, view ui.Rect, glow seaGlow, lift float32) {
	const farthestCorner = 1.4142
	rx := max(glow.atX, 1-glow.atX) * view.W * farthestCorner * glow.reach
	ry := max(glow.atY, 1-glow.atY) * view.H * farthestCorner * glow.reach
	cx, cy := view.X+glow.atX*view.W, view.Y+glow.atY*view.H
	layer := seaColor(glow.rgb, 0.22, 1, lift)
	for i := seaBlobSteps; i >= 1; i-- {
		k := float32(i) / seaBlobSteps
		ellipse := ui.Rect{X: cx - rx*k, Y: cy - ry*k, W: 2 * rx * k, H: 2 * ry * k}
		p.Fill(ellipse, layer, min(ellipse.W, ellipse.H)/2)
	}
}
