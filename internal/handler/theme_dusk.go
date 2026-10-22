package handler

import (
	"image"
	"image/color"
	"math"

	"github.com/egoist/mygo/ui"
)

// The dusk atmosphere is the dusk theme's layers of gradient light, baked into
// a small bitmap the way the sea wallpaper is: the same look, one texture
// instead of window-sized gradients painted every frame.

const (
	// duskLayerAlpha is the opacity of the sky gradient over the base.
	duskLayerAlpha float32 = 0.90
	// duskFoldFrom and duskFoldTo are the opacities of the fold's light.
	duskFoldFrom float32 = 0.12
	duskFoldTo   float32 = 0.08
	// duskHazeAlpha is how far the fold colour rises toward the lower right.
	duskHazeAlpha float32 = 0.32
	// duskTopLight is the faint base-coloured light along the top edge.
	duskTopLight float32 = 0.04
)

// The fold is one cubic curve from the left edge to below the lower right
// corner, closed under the window, in the window's own proportions.
var (
	duskFoldP0 = [2]float32{-0.12, 0.38}
	duskFoldP1 = [2]float32{0.20, -0.02}
	duskFoldP2 = [2]float32{0.49, 0.14}
	duskFoldP3 = [2]float32{1.10, 1.05}
)

// duskRGBA is a colour in 0..1 floats with straight alpha.
type duskRGBA struct{ r, g, b, a float32 }

func duskColor(c ui.Color, alpha float32) duskRGBA {
	return duskRGBA{float32(c.R) / 255, float32(c.G) / 255, float32(c.B) / 255, alpha}
}

// over composites top over bottom, where bottom is opaque.
func (top duskRGBA) over(bottom duskRGBA) duskRGBA {
	return duskRGBA{top.r*top.a + bottom.r*(1-top.a), top.g*top.a + bottom.g*(1-top.a), top.b*top.a + bottom.b*(1-top.a), 1}
}

func duskMix(from duskRGBA, to duskRGBA, k float32) duskRGBA {
	k = min(1, max(0, k))
	return duskRGBA{from.r + (to.r-from.r)*k, from.g + (to.g-from.g)*k, from.b + (to.b-from.b)*k, from.a + (to.a-from.a)*k}
}

// duskBezier is the fold curve's point at t, in window proportions.
func duskBezier(t float32) (float32, float32) {
	u := 1 - t
	a, b, c, d := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
	return a*duskFoldP0[0] + b*duskFoldP1[0] + c*duskFoldP2[0] + d*duskFoldP3[0],
		a*duskFoldP0[1] + b*duskFoldP1[1] + c*duskFoldP2[1] + d*duskFoldP3[1]
}

// duskFoldEdge is the height of the fold's curve over x, in window
// proportions. The curve only moves right as t grows, so each x has one.
func duskFoldEdge(x float32) float32 {
	lo, hi := float32(0), float32(1)
	for range 24 {
		mid := (lo + hi) / 2
		if px, _ := duskBezier(mid); px < x {
			lo = mid
		} else {
			hi = mid
		}
	}
	_, y := duskBezier((lo + hi) / 2)
	return y
}

// duskPixel is the atmosphere's colour at (u, v), the window's proportions,
// for a window of w by h points.
func duskPixel(palette windowMaterialPalette, u float32, v float32, w float32, h float32) duskRGBA {
	x, y := (u-0.5)*w, (v-0.5)*h
	px := duskColor(palette.base, 1)
	sky := duskMix(duskColor(palette.glow, duskLayerAlpha), duskColor(palette.sky, duskLayerAlpha), seaLineT(x, y, w, h, 180))
	px = sky.over(px)
	if u >= duskFoldP0[0] && v <= duskFoldP3[1] {
		cover := min(1, max(0, (v-duskFoldEdge(u))*h/seaCell+0.5))
		if cover > 0 {
			// The fold's light runs across its bounding box.
			k := seaLineT((u-(duskFoldP0[0]+duskFoldP3[0])/2)*w, (v-(duskFoldP1[1]+duskFoldP3[1])/2)*h,
				(duskFoldP3[0]-duskFoldP0[0])*w, (duskFoldP3[1]-duskFoldP1[1])*h, 160)
			light := duskMix(duskColor(palette.base, duskFoldFrom), duskColor(palette.sky, duskFoldTo), k)
			light.a *= cover
			px = light.over(px)
		}
	}
	haze := duskColor(palette.fold, duskHazeAlpha*min(1, max(0, seaLineT(x, y, w, h, 95))))
	px = haze.over(px)
	top := duskColor(palette.base, duskTopLight*(1-min(1, max(0, seaLineT(x, y, w, h, 180)))))
	return top.over(px)
}

// duskImage bakes the dusk atmosphere for a window of w by h points, one
// pixel per seaCell points.
func duskImage(w float32, h float32, palette windowMaterialPalette) *image.RGBA {
	nx, ny := max(2, int(math.Ceil(float64(w/seaCell)))), max(2, int(math.Ceil(float64(h/seaCell))))
	img := image.NewRGBA(image.Rect(0, 0, nx, ny))
	to8 := func(v float32) uint8 { return uint8(min(1, max(0, v))*255 + 0.5) }
	for j := range ny {
		for i := range nx {
			px := duskPixel(palette, (float32(i)+0.5)/float32(nx), (float32(j)+0.5)/float32(ny), w, h)
			img.SetRGBA(i, j, color.RGBA{R: to8(px.r), G: to8(px.g), B: to8(px.b), A: 255})
		}
	}
	return img
}
