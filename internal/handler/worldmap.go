package handler

import (
	"image"
	"math"
	"sync"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
)

const (
	// The map is a dot matrix of the land in cells of 5 degrees: worldMapColumns
	// across the whole of longitude and one row per 5 degrees of latitude from
	// worldMapTopLat down. It fills the card with worldMapVisibleRows rows from
	// top to bottom, so a dot is a fixed distance apart whatever the card's
	// width; the land repeats beyond 180 degrees, as a map centred on the Pacific
	// does.
	worldMapColumns     = 72
	worldMapTopLat      = 70
	worldMapCell        = 5
	worldMapVisibleRows = 16
	// worldMapDot is a dot's radius as a share of a cell, worldMapDotAlpha its
	// opacity.
	worldMapDot      float32 = 0.2
	worldMapDotAlpha float32 = 0.42
	// The dots fade in from nothing at worldMapFadeFrom of the card's width to
	// full at worldMapFadeTo, in worldMapFadeSteps steps, so the exit's name and
	// the gauge stay on a quiet ground. The route is centred worldMapCenterX of
	// the way across, clear of the exit's name on the left and of the speeds on
	// the right; worldMapDefaultLon and worldMapDefaultLat are what the map
	// centres on while the route has no place.
	worldMapFadeFrom   float32 = 0.34
	worldMapFadeTo     float32 = 0.60
	worldMapFadeSteps          = 8
	worldMapCenterX    float32 = 0.67
	worldMapDefaultLon float32 = 100
	worldMapDefaultLat float32 = 25
	// Two places less than worldMapNearCells cells apart (12.5 degrees, as far as
	// Shanghai from Taiwan) are too close for a line to show anything: the map
	// marks only the exit.
	worldMapNearCells float32 = 2.5
	// The route: the width of its line and of the soft stroke under it with
	// its opacity, how far it bows (a share of its length, at least
	// worldMapMinBow points), and the radii of the two ends and the halo round
	// the exit.
	worldMapLine      float32 = 1.5
	worldMapLineGlow  float32 = 5
	worldMapGlowAlpha float32 = 0.16
	worldMapBow       float32 = 0.28
	worldMapMinBow    float32 = 10
	worldMapHomeDot   float32 = 2.5
	worldMapExitDot   float32 = 3
	worldMapExitHalo  float32 = 7
	worldMapHaloAlpha float32 = 0.2
)

// worldLand lists, for each row from 70 degrees north to 50 south, the runs of
// columns (inclusive, counted east from 180 degrees west) that hold land.
var worldLand = [][][2]int{
	{{3, 24}, {26, 31}, {38, 43}, {44, 71}},                      // 70
	{{3, 6}, {9, 23}, {27, 30}, {31, 32}, {37, 42}, {43, 71}},    // 65
	{{3, 5}, {8, 23}, {35, 35}, {37, 42}, {43, 70}},              // 60
	{{9, 24}, {35, 36}, {37, 43}, {44, 70}},                      // 55
	{{9, 24}, {35, 44}, {45, 66}},                                // 50
	{{10, 24}, {35, 44}, {45, 64}},                               // 45
	{{11, 23}, {34, 36}, {37, 44}, {45, 64}},                     // 40
	{{12, 22}, {33, 43}, {44, 51}, {52, 62}, {63, 64}},           // 35
	{{13, 21}, {34, 43}, {44, 48}, {49, 60}},                     // 30
	{{14, 20}, {20, 22}, {32, 43}, {44, 49}, {50, 54}, {55, 61}}, // 25
	{{15, 19}, {32, 44}, {46, 48}, {50, 53}, {55, 60}},           // 20
	{{17, 19}, {32, 44}, {51, 53}, {56, 59}, {61, 61}},           // 15
	{{21, 25}, {33, 44}, {51, 52}, {56, 58}, {61, 61}},           // 10
	{{21, 27}, {34, 45}, {56, 57}, {58, 60}},                     // 5
	{{20, 26}, {38, 44}, {55, 57}, {58, 60}, {61, 61}},           // 0
	{{20, 29}, {38, 44}, {55, 56}, {57, 60}, {63, 67}},           // -5
	{{20, 28}, {38, 44}, {57, 60}, {64, 67}},                     // -10
	{{21, 28}, {38, 43}, {59, 66}},                               // -15
	{{22, 28}, {38, 43}, {44, 45}, {58, 66}},                     // -20
	{{22, 26}, {39, 42}, {58, 66}},                               // -25
	{{22, 26}, {39, 42}, {59, 66}},                               // -30
	{{21, 24}, {40, 41}, {60, 66}, {70, 71}},                     // -35
	{{21, 23}, {64, 64}, {70, 71}},                               // -40
	{{21, 23}, {70, 70}},                                         // -45
	{{21, 22}},                                                   // -50
}

// worldMapRows is how many rows the grid has.
func worldMapRows() int { return len(worldLand) }

// wrapLon is degrees of longitude brought into -180 to 180.
func wrapLon(lon float32) float32 {
	return float32(math.Mod(math.Mod(float64(lon)+180, 360)+360, 360)) - 180
}

// worldMapRoute is what the map draws over the land.
type worldMapRoute struct {
	home      model.GeoPoint
	homeKnown bool
	exit      model.ExitPlace
	dot       ui.Color
	ink       ui.Color
	accent    ui.Color
}

// placed says the route has a line to draw: the user's place and an exit the
// map could place.
func (route worldMapRoute) placed() bool {
	return route.homeKnown && (route.exit.Kind == model.ExitPlaceMeasured || route.exit.Kind == model.ExitPlaceByName)
}

// near says the exit is too close to the user for a line.
func (route worldMapRoute) near() bool {
	if !route.placed() {
		return false
	}
	across := wrapLon(route.exit.Point.Lon-route.home.Lon) / worldMapCell
	down := (route.exit.Point.Lat - route.home.Lat) / worldMapCell
	return float32(math.Hypot(float64(across), float64(down))) < worldMapNearCells
}

// center is the longitude and latitude the map is centred on: the middle of
// the way from the user to the exit, going the short way round the earth; the
// exit itself when it is near.
func (route worldMapRoute) center() (float32, float32) {
	switch {
	case route.near():
		return route.exit.Point.Lon, route.exit.Point.Lat
	case route.placed():
		toExit := wrapLon(route.exit.Point.Lon - route.home.Lon)
		return wrapLon(route.home.Lon + toExit/2), (route.home.Lat + route.exit.Point.Lat) / 2
	case route.homeKnown:
		return route.home.Lon, route.home.Lat
	default:
		return worldMapDefaultLon, worldMapDefaultLat
	}
}

// worldMapGeometry places the earth in a rectangle: the size of a cell, where
// the middle longitude and latitude are, and the longitude there.
type worldMapGeometry struct {
	cell      float32
	x, y      float32
	centerLon float32
}

// newWorldMapGeometry fits worldMapVisibleRows rows into r and puts the route's
// centre at worldMapCenterX of r's width and the middle of its height, as far
// as the grid still covers r.
func newWorldMapGeometry(r ui.Rect, route worldMapRoute) worldMapGeometry {
	lon, lat := route.center()
	cell := r.H / worldMapVisibleRows
	height := cell * float32(worldMapRows())
	centerY := ((worldMapTopLat-lat)/worldMapCell + 0.5) * cell
	top := min(max(r.H/2-centerY, r.H-height), 0)
	return worldMapGeometry{cell: cell, x: r.X + r.W*worldMapCenterX, y: r.Y + top, centerLon: lon}
}

// point is where a place on the earth is drawn: the longitude counted from the
// map's centre the short way round.
func (g worldMapGeometry) point(place model.GeoPoint) (float32, float32) {
	row := (worldMapTopLat-place.Lat)/worldMapCell + 0.5
	return g.x + wrapLon(place.Lon-g.centerLon)/worldMapCell*g.cell, g.y + row*g.cell
}

// worldLeft is where the world's west edge is, the nearest to r's left side.
func (g worldMapGeometry) worldLeft(left float32) float32 {
	period := g.cell * worldMapColumns
	edge := g.x - (g.centerLon+180)/worldMapCell*g.cell
	return edge - period*float32(math.Ceil(float64((edge-left)/period)))
}

// worldMapKey names a baked map: its size in bitmap pixels and its colour.
type worldMapKey struct {
	w, h  int
	color ui.Color
}

// worldMapCache keeps the bitmap of the last size and colour, so the dots are
// painted again only when one of them changes.
var worldMapCache struct {
	sync.Mutex
	key    worldMapKey
	bitmap *ui.Bitmap
}

// worldMapBitmap is the land as dots of color for a grid of cell points a
// side, at scale pixels to the point.
func worldMapBitmap(cell float32, scale float32, color ui.Color) *ui.Bitmap {
	key := worldMapKey{w: int(math.Round(float64(cell * worldMapColumns * scale))), h: int(math.Round(float64(cell * float32(worldMapRows()) * scale))), color: color}
	worldMapCache.Lock()
	defer worldMapCache.Unlock()
	if worldMapCache.bitmap == nil || worldMapCache.key != key {
		worldMapCache.bitmap = ui.NewBitmap(worldMapImage(key))
		worldMapCache.key = key
	}
	return worldMapCache.bitmap
}

// worldMapImage paints the dots into a bitmap of key's size.
func worldMapImage(key worldMapKey) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, key.w, key.h))
	cell := float64(key.w) / worldMapColumns
	radius := cell * float64(worldMapDot)
	for row, runs := range worldLand {
		for _, run := range runs {
			for column := run[0]; column <= run[1]; column++ {
				paintDot(img, (float64(column)+0.5)*cell, (float64(row)+0.5)*cell, radius, key.color)
			}
		}
	}
	return img
}

// paintDot paints a round dot of colour at (cx, cy) with a one pixel soft edge.
func paintDot(img *image.RGBA, cx float64, cy float64, radius float64, color ui.Color) {
	bounds := img.Bounds()
	for y := max(int(cy-radius-1), bounds.Min.Y); y <= min(int(cy+radius+1), bounds.Max.Y-1); y++ {
		for x := max(int(cx-radius-1), bounds.Min.X); x <= min(int(cx+radius+1), bounds.Max.X-1); x++ {
			distance := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy)
			cover := min(max(radius+0.5-distance, 0), 1)
			if cover == 0 {
				continue
			}
			alpha := cover * float64(color.A) / 255
			offset := img.PixOffset(x, y)
			img.Pix[offset+0] = uint8(math.Round(float64(color.R) * alpha))
			img.Pix[offset+1] = uint8(math.Round(float64(color.G) * alpha))
			img.Pix[offset+2] = uint8(math.Round(float64(color.B) * alpha))
			img.Pix[offset+3] = uint8(math.Round(alpha * 255))
		}
	}
}

// drawWorldMap fills r (rounded by radius) with the land, then the route from
// where the user is to where the exit is: a bowed line with a soft stroke under
// it, a dot at each end and a halo round the exit. Nothing on it moves.
func drawWorldMap(p *ui.Painter, r ui.Rect, radius float32, route worldMapRoute) {
	geometry := newWorldMapGeometry(r, route)
	p.Clip(r, radius, func() {
		geometry.drawLand(p, r, route.dot)
		route.drawRoute(p, r, geometry)
	})
}

// drawLand draws the land in strips across r, each more opaque than the one
// before it, with the world repeated as often as r is wide.
func (g worldMapGeometry) drawLand(p *ui.Painter, r ui.Rect, color ui.Color) {
	bitmap := worldMapBitmap(g.cell, p.Scale(), color)
	period := g.cell * worldMapColumns
	height := g.cell * float32(worldMapRows())
	pixelsW, pixelsH := ui.ImageSize(bitmap)
	fadeFrom, fadeTo := r.X+r.W*worldMapFadeFrom, r.X+r.W*worldMapFadeTo
	step := (fadeTo - fadeFrom) / worldMapFadeSteps
	for left := g.worldLeft(r.X); left < r.X+r.W; left += period {
		for strip := 0; strip <= worldMapFadeSteps; strip++ {
			from, to := fadeFrom+float32(strip-1)*step, fadeFrom+float32(strip)*step
			opacity := float32(strip) / worldMapFadeSteps
			if strip == 0 {
				from = r.X
			}
			if strip == worldMapFadeSteps {
				to = r.X + r.W
			}
			from, to = max(from, left), min(to, left+period)
			if to <= from || opacity == 0 {
				continue
			}
			src := ui.Rect{X: (from - left) / period * pixelsW, Y: 0, W: (to - from) / period * pixelsW, H: pixelsH}
			dst := ui.Rect{X: from, Y: g.y, W: to - from, H: height}
			p.ImageRegion(bitmap, src, dst, ui.ImageOptions{Tint: ui.Color{R: 255, G: 255, B: 255, A: uint8(math.Round(float64(opacity) * 255))}})
		}
	}
}

// drawRoute draws the line and the dots of the route over the land.
func (route worldMapRoute) drawRoute(p *ui.Painter, r ui.Rect, geometry worldMapGeometry) {
	if !route.homeKnown {
		return
	}
	hx, hy := geometry.point(route.home)
	switch route.exit.Kind {
	case model.ExitPlaceUnknown:
		fillCircle(p, hx, hy, worldMapHomeDot, route.ink)
		return
	case model.ExitPlaceDirect:
		fillCircle(p, hx, hy, worldMapExitHalo, route.accent.Alpha(worldMapHaloAlpha))
		fillCircle(p, hx, hy, worldMapExitDot, route.accent)
		return
	}
	ex, ey := geometry.point(route.exit.Point)
	if !route.near() {
		length := float32(math.Hypot(float64(ex-hx), float64(ey-hy)))
		// The line bows upward, but never out of the card: a quadratic's
		// highest point is half way between its ends' mean and its control point.
		bow := max(length*worldMapBow, worldMapMinBow)
		controlY := max(min(hy, ey)-bow, 2*(r.Y+worldMapExitHalo)-(hy+ey)/2)
		arc := new(ui.Path).MoveTo(hx, hy).QuadTo((hx+ex)/2, controlY, ex, ey)
		p.StrokePath(arc, worldMapLineGlow, route.accent.Alpha(worldMapGlowAlpha))
		p.StrokePath(arc, worldMapLine, route.accent)
		fillCircle(p, hx, hy, worldMapHomeDot, route.ink)
	}
	fillCircle(p, ex, ey, worldMapExitHalo, route.accent.Alpha(worldMapHaloAlpha))
	fillCircle(p, ex, ey, worldMapExitDot, route.accent)
}

// fillCircle fills a circle of colour at (cx, cy).
func fillCircle(p *ui.Painter, cx float32, cy float32, radius float32, color ui.Color) {
	p.FillPath(new(ui.Path).Circle(cx, cy, radius), color)
}
