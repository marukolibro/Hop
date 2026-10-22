package handler

import (
	"image"
	"image/color"
	"math"
	"sync"

	"github.com/egoist/mygo/ui"
)

// The sea wallpaper is the design page's desktop, baked into a small bitmap
// that the window material stretches over the window: a gradient sky, one soft
// glow and two tilted blobs, in the page's own proportions. The wallpaper is
// smooth, so a coarse bitmap shows it as it is, and one small texture costs
// far less than painting window-sized gradient paths every frame.
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
	// applied to every wallpaper colour because the bitmap has no filter.
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
	// seaCell is the size of one bitmap pixel in points.
	seaCell float32 = 6
	// seaEdge is the width in design pixels over which a blob's edge fades:
	// the page's 2px blur plus its 32px glass blur, which the bitmap has not.
	seaEdge float32 = 40
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

// seaRGBA is a colour in 0..1 floats with premultiplied alpha, so stops and
// layers interpolate and composite as CSS does.
type seaRGBA struct{ r, g, b, a float32 }

// seaColor turns a stop into a colour with the wallpaper's saturation, lifted
// toward white by lift, its alpha scaled by opacity, premultiplied.
func seaColor(stop seaStop, opacity float32, lift float32) seaRGBA {
	r, g, b := float32(stop.rgb>>16&0xFF)/255, float32(stop.rgb>>8&0xFF)/255, float32(stop.rgb&0xFF)/255
	luma := 0.2126*r + 0.7152*g + 0.0722*b
	adjust := func(v float32) float32 {
		v = min(1, max(0, luma+(v-luma)*seaSaturation))
		return v + (1-v)*lift
	}
	a := stop.alpha * opacity
	return seaRGBA{adjust(r) * a, adjust(g) * a, adjust(b) * a, a}
}

// seaRamp samples a gradient's stops at t, interpolating premultiplied
// colours and holding the first and last colour outside the stops.
func seaRamp(stops []seaStop, opacity float32, lift float32, t float32) seaRGBA {
	first, last := stops[0], stops[len(stops)-1]
	if t <= first.at {
		return seaColor(first, opacity, lift)
	}
	for i := 1; i < len(stops); i++ {
		if t <= stops[i].at {
			span := stops[i].at - stops[i-1].at
			k := (t - stops[i-1].at) / span
			a, b := seaColor(stops[i-1], opacity, lift), seaColor(stops[i], opacity, lift)
			return seaRGBA{a.r + (b.r-a.r)*k, a.g + (b.g-a.g)*k, a.b + (b.b-a.b)*k, a.a + (b.a-a.a)*k}
		}
	}
	return seaColor(last, opacity, lift)
}

// seaLineT is where (x, y) falls along a CSS linear gradient of the given
// angle across a w by h box centred on the origin, from 0 to 1.
func seaLineT(x float32, y float32, w float32, h float32, angle float32) float32 {
	rad := float64(angle) * math.Pi / 180
	sin, cos := float32(math.Sin(rad)), float32(math.Cos(rad))
	length := float32(math.Abs(float64(w*sin))) + float32(math.Abs(float64(h*cos)))
	return (x*sin-y*cos)/length + 0.5
}

// over composites top over bottom, both premultiplied.
func (top seaRGBA) over(bottom seaRGBA) seaRGBA {
	k := 1 - top.a
	return seaRGBA{top.r + bottom.r*k, top.g + bottom.g*k, top.b + bottom.b*k, top.a + bottom.a*k}
}

// seaBlobMask is how much of a blob covers the point (lx, ly), in the blob's
// own frame centred on the origin: a box whose corners are ellipses of
// seaCornerPct of its sides, fading over seaEdge at its edge.
func seaBlobMask(lx float32, ly float32, w float32, h float32) float32 {
	rx, ry := w*seaCornerPct, h*seaCornerPct
	ax, ay := float32(math.Abs(float64(lx)))-(w/2-rx), float32(math.Abs(float64(ly)))-(h/2-ry)
	var dist float32
	if ax > 0 && ay > 0 {
		dist = (float32(math.Hypot(float64(ax/rx), float64(ay/ry))) - 1) * min(rx, ry)
	} else {
		dist = max(float32(math.Abs(float64(lx)))-w/2, float32(math.Abs(float64(ly)))-h/2)
	}
	return min(1, max(0, 0.5-dist/seaEdge))
}

// seaPixel is the wallpaper's colour at a point of the design's viewport.
func seaPixel(wall seaWallpaper, u float32, v float32) seaRGBA {
	px := seaRamp(wall.sky, 1, wall.lift, seaLineT(u-seaViewportW/2, v-seaViewportH/2, seaViewportW, seaViewportH, wall.skyAngle))
	glow := wall.glow
	const farthestCorner = 1.4142
	rx := max(glow.atX, 1-glow.atX) * seaViewportW * farthestCorner * glow.reach
	ry := max(glow.atY, 1-glow.atY) * seaViewportH * farthestCorner * glow.reach
	d := float32(math.Hypot(float64((u-glow.atX*seaViewportW)/rx), float64((v-glow.atY*seaViewportH)/ry)))
	if a := max(0, 1-d); a > 0 {
		px = seaColor(seaStop{rgb: glow.rgb, alpha: a}, 1, wall.lift).over(px)
	}
	for _, blob := range wall.blobs {
		w, h := blob.widthVW*seaViewportW/100, blob.heightVW*seaViewportW/100
		cx, cy := blob.leftVW*seaViewportW/100+w/2, blob.topVH*seaViewportH/100+h/2
		rad := float64(blob.tilt) * math.Pi / 180
		sin, cos := float32(math.Sin(rad)), float32(math.Cos(rad))
		dx, dy := u-cx, v-cy
		lx, ly := dx*cos+dy*sin, -dx*sin+dy*cos
		mask := seaBlobMask(lx, ly, w, h)
		if mask <= 0 {
			continue
		}
		c := seaRamp(blob.stops, blob.opacity, wall.lift, seaLineT(lx, ly, w, h, blob.angle))
		px = seaRGBA{c.r * mask, c.g * mask, c.b * mask, c.a * mask}.over(px)
	}
	return px
}

// seaImage bakes the sea wallpaper for a window of w by h points, one pixel
// per seaCell points, with the window mapped onto the design page's window.
func seaImage(w float32, h float32, dark bool) *image.RGBA {
	wall := seaLight()
	if dark {
		wall = seaDark()
	}
	nx, ny := max(2, int(math.Ceil(float64(w/seaCell)))), max(2, int(math.Ceil(float64(h/seaCell))))
	img := image.NewRGBA(image.Rect(0, 0, nx, ny))
	for j := range ny {
		for i := range nx {
			u := (float32(i)+0.5)/float32(nx)*seaWindowW + seaWindowX
			v := (float32(j)+0.5)/float32(ny)*seaWindowH + seaWindowY
			px := seaPixel(wall, u, v)
			img.SetRGBA(i, j, color.RGBA{R: uint8(min(1, px.r)*255 + 0.5), G: uint8(min(1, px.g)*255 + 0.5), B: uint8(min(1, px.b)*255 + 0.5), A: 255})
		}
	}
	return img
}

// seaBitmapCache keeps the bitmap of the last window size and appearance: the
// wallpaper is baked again only when the window is resized or switches
// appearance. The material is rebuilt every frame, so the cache outlives it.
var seaBitmapCache struct {
	sync.Mutex
	key    [3]int
	bitmap *ui.Bitmap
}

// seaBitmap is the sea wallpaper as a bitmap for a window of w by h points.
func seaBitmap(w float32, h float32, dark bool) *ui.Bitmap {
	key := [3]int{int(math.Round(float64(w / seaCell))), int(math.Round(float64(h / seaCell))), 0}
	if dark {
		key[2] = 1
	}
	seaBitmapCache.Lock()
	defer seaBitmapCache.Unlock()
	if seaBitmapCache.bitmap == nil || seaBitmapCache.key != key {
		seaBitmapCache.bitmap = ui.NewBitmap(seaImage(w, h, dark))
		seaBitmapCache.key = key
	}
	return seaBitmapCache.bitmap
}

// drawSeaWallpaper paints the sea wallpaper behind the window rect r.
func drawSeaWallpaper(p *ui.Painter, r ui.Rect, dark bool) {
	p.Image(seaBitmap(r.W, r.H, dark), r, ui.FillBox)
}
