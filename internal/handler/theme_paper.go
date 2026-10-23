package handler

import (
	"image"
	"image/color"
	"sync"

	"github.com/egoist/mygo/ui"
)

// The paper theme is one flat sheet with a fine grain: the paper look of a
// printed page, without gradients. The grain is baked once into a small opaque
// tile and repeated over the window, so a resize costs only a few draw calls.
//
// The grain is luminance noise with a mean of zero around the paper colour, so
// the sheet's average tone is exactly the palette's base. The glass veils the
// window lays over it are that same base, and keep every region one tone.
const (
	// paperTile is the grain tile's side, in bitmap pixels and in points.
	paperTile = 192
	// paperGrainLight and paperGrainDark are the noise's full swing, as a
	// fraction of the 0..255 range.
	paperGrainLight float32 = 0.035
	paperGrainDark  float32 = 0.030
)

// paperHash is a small integer hash that makes the grain deterministic.
func paperHash(x uint32, y uint32) uint32 {
	h := x*374761393 + y*668265263
	h = (h ^ (h >> 13)) * 1274126177
	return h ^ (h >> 16)
}

// paperShade moves one 8-bit channel by delta, kept inside the range.
func paperShade(v uint8, delta float32) uint8 {
	return uint8(min(255, max(0, float32(v)+delta)+0.5))
}

// paperGrainImage bakes the grain tile around the paper colour base.
func paperGrainImage(base ui.Color, swing float32) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, paperTile, paperTile))
	for y := range paperTile {
		for x := range paperTile {
			delta := (float32(paperHash(uint32(x), uint32(y))&0xFF)/255 - 0.5) * swing * 255
			img.SetRGBA(x, y, color.RGBA{R: paperShade(base.R, delta), G: paperShade(base.G, delta), B: paperShade(base.B, delta), A: 255})
		}
	}
	return img
}

// paperGrainCache keeps the two tiles, baked the first time each is drawn.
var paperGrainCache struct {
	sync.Mutex
	light, dark *ui.Bitmap
}

func paperGrain(palette windowMaterialPalette) *ui.Bitmap {
	paperGrainCache.Lock()
	defer paperGrainCache.Unlock()
	slot, swing := &paperGrainCache.light, paperGrainLight
	if palette.dark {
		slot, swing = &paperGrainCache.dark, paperGrainDark
	}
	if *slot == nil {
		*slot = ui.NewBitmap(paperGrainImage(palette.base, swing))
	}
	return *slot
}

// drawPaper repeats the grained sheet over rect r.
func drawPaper(p *ui.Painter, r ui.Rect, palette windowMaterialPalette) {
	grain := paperGrain(palette)
	for y := float32(0); y < r.H; y += paperTile {
		for x := float32(0); x < r.W; x += paperTile {
			tile := ui.Rect{X: r.X + x, Y: r.Y + y, W: min(paperTile, r.W-x), H: min(paperTile, r.H-y)}
			p.ImageRegion(grain, ui.Rect{W: tile.W, H: tile.H}, tile, ui.ImageOptions{})
		}
	}
}
