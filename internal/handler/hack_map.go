package handler

import (
	"image"
	"image/color"
	"math"
	"strconv"
	"sync"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
	"hop/locales"
)

// The Hack theme's map is the land as a fine grid of square dots, the coast
// brighter than the inland, with the route bowed over it: a box marks each
// end and a framed label names it. Routing groups that leave elsewhere get a
// thin dotted line and a label of their own. Nothing on it moves.
const (
	// hackMapSpan is how many degrees of longitude the map's width shows;
	// hackMapStretch how much taller a degree of latitude is drawn.
	hackMapSpan    float32 = 220
	hackMapStretch float32 = 1.08
	// hackDotStep is the grid's spacing in points; hackDot and hackCoastDot
	// the side of an inland and a coastal dot; hackInlandAlpha and
	// hackCoastAlpha their strength.
	hackDotStep     float32 = 6
	hackDot         float32 = 1.6
	hackCoastDot    float32 = 2.2
	hackInlandAlpha float32 = 0.42
	hackCoastAlpha  float32 = 0.9
	// The route's line, the light under it, and the dotted line of another
	// exit with its dashes.
	hackRouteLine  float32 = 2.2
	hackRouteGlow  float32 = 7
	hackRouteLight float32 = 0.22
	hackOtherLine  float32 = 1.4
	hackOtherDash  float32 = 2
	hackOtherGap   float32 = 5
	hackOtherAlpha float32 = 0.75
	// hackMarker is the side of an end's box and hackMarkerFrame the frame
	// around it; hackFrameAlpha the frame's strength.
	hackMarker      float32 = 6
	hackMarkerFrame float32 = 15
	hackFrameAlpha  float32 = 0.6
	// hackBow is how far a line bows, a share of its length.
	hackBow float32 = 0.3
	// hackLabelGap is how far a label sits from its end; hackLabelLift how
	// far the user's label is raised.
	hackLabelGap  float32 = 24
	hackLabelLift float32 = 30
	// hackLabelChar estimates a character's width as a share of the font
	// size, wide ones twice that, to keep a label inside the map.
	hackLabelChar float32 = 0.62
	// hackMapMargin is the room kept between a marked place and the map's
	// edge, for its label.
	hackMapMargin float32 = 44
	// hackLabelLine is a label's line height as a share of its font size.
	hackLabelLine float32 = 1.4
	// hackOtherExits is the most other exits the map labels.
	hackOtherExits = 2
)

// hackMapView places the earth in the map: its size, the longitude and
// latitude at its middle, and points per degree.
type hackMapView struct {
	w, h           float32
	lon, lat       float32
	perLon, perLat float32
}

// newHackMapView frames places in a map of w by h: it centres them, shows
// hackMapSpan degrees across unless they need more room, and keeps
// hackMapMargin clear at the edges for the labels. With no place it centres on
// the route's default.
func newHackMapView(w float32, h float32, places []model.GeoPoint, route worldMapRoute) hackMapView {
	lon, lat := route.center()
	if len(places) > 0 {
		// Longitudes are counted from the first place the short way round, so
		// a frame across the 180th meridian stays whole.
		west, east := float32(0), float32(0)
		south, north := places[0].Lat, places[0].Lat
		for _, p := range places {
			off := wrapLon(p.Lon - places[0].Lon)
			west, east = min(west, off), max(east, off)
			south, north = min(south, p.Lat), max(north, p.Lat)
		}
		lon, lat = wrapLon(places[0].Lon+(west+east)/2), (south+north)/2
		perLon := w / hackMapSpan
		if east > west {
			perLon = min(perLon, (w-2*hackMapMargin)/(east-west))
		}
		if north > south {
			perLon = min(perLon, (h-2*hackMapMargin)/(north-south)/hackMapStretch)
		}
		return hackMapView{w: w, h: h, lon: lon, lat: lat, perLon: perLon, perLat: perLon * hackMapStretch}
	}
	perLon := w / hackMapSpan
	return hackMapView{w: w, h: h, lon: lon, lat: lat, perLon: perLon, perLat: perLon * hackMapStretch}
}

// point is where a place is drawn, its longitude counted from the middle the
// short way round.
func (v hackMapView) point(place model.GeoPoint) (float32, float32) {
	return v.w/2 + wrapLon(place.Lon-v.lon)*v.perLon, v.h/2 - (place.Lat-v.lat)*v.perLat
}

// hackIsLand says whether the degree cell holding lon and lat is land.
func hackIsLand(lon float32, lat float32) bool {
	row := int(math.Floor(float64(hackLandTop - lat)))
	if row < 0 || row >= len(hackLand) {
		return false
	}
	col := int(math.Floor(float64(wrapLon(lon)+180))) % 360
	for _, run := range hackLand[row] {
		if col >= run[0] && col <= run[1] {
			return true
		}
	}
	return false
}

// hackLandKey names a baked land: its size in pixels, where it is centred,
// and its colour.
type hackLandKey struct {
	w, h     int
	lon, lat float32
	color    ui.Color
}

// hackLandCache keeps the land of the last key, painted again only when the
// size, the centre or the colour changes.
var hackLandCache struct {
	sync.Mutex
	key    hackLandKey
	bitmap *ui.Bitmap
}

// hackLandBitmap is the land of view in color at scale pixels to the point.
func hackLandBitmap(view hackMapView, scale float32, color ui.Color) *ui.Bitmap {
	key := hackLandKey{w: int(view.w * scale), h: int(view.h * scale), lon: view.lon, lat: view.lat, color: color}
	hackLandCache.Lock()
	defer hackLandCache.Unlock()
	if hackLandCache.bitmap == nil || hackLandCache.key != key {
		hackLandCache.bitmap = ui.NewBitmap(hackLandImage(view, scale, color))
		hackLandCache.key = key
	}
	return hackLandCache.bitmap
}

// hackLandImage paints the grid of dots: a dot is coastal when a neighbour
// is sea.
func hackLandImage(view hackMapView, scale float32, ink ui.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, int(view.w*scale), int(view.h*scale)))
	land := func(x float32, y float32) bool {
		return hackIsLand(view.lon+(x-view.w/2)/view.perLon, view.lat-(y-view.h/2)/view.perLat)
	}
	for y := hackDotStep / 2; y < view.h; y += hackDotStep {
		for x := hackDotStep / 2; x < view.w; x += hackDotStep {
			if !land(x, y) {
				continue
			}
			coast := !land(x-hackDotStep, y) || !land(x+hackDotStep, y) || !land(x, y-hackDotStep) || !land(x, y+hackDotStep)
			side, alpha := hackDot, hackInlandAlpha
			if coast {
				side, alpha = hackCoastDot, hackCoastAlpha
			}
			paintSquare(img, x*scale, y*scale, side*scale, ink.Alpha(alpha))
		}
	}
	return img
}

// paintSquare paints a square of side pixels centred at (cx, cy).
func paintSquare(img *image.RGBA, cx float32, cy float32, side float32, c ui.Color) {
	premultiplied := color.RGBA{
		R: uint8(float32(c.R) * float32(c.A) / 255), G: uint8(float32(c.G) * float32(c.A) / 255),
		B: uint8(float32(c.B) * float32(c.A) / 255), A: c.A,
	}
	x0, y0 := int(math.Round(float64(cx-side/2))), int(math.Round(float64(cy-side/2)))
	n := max(1, int(math.Round(float64(side))))
	for y := y0; y < y0+n; y++ {
		for x := x0; x < x0+n; x++ {
			if image.Pt(x, y).In(img.Bounds()) {
				img.SetRGBA(x, y, premultiplied)
			}
		}
	}
}

// hackOtherExit is an exit routing groups leave by that is not the route's:
// where it is and what its label says.
type hackOtherExit struct {
	point model.GeoPoint
	name  string
	note  string
}

// hackOtherExits are the exits of the routing groups that leave in another
// country than the route, at most hackOtherExits, each placed by its flag.
func (d *Dashboard) hackOtherExits(main model.ExitPlace) []hackOtherExit {
	var out []hackOtherExit
	seen := map[string]bool{main.Country: true}
	for _, branch := range service.ExitBranches(d.view.Groups) {
		var routing *model.GroupView
		for i, g := range branch.Groups {
			if g.Kind == model.GroupKindRouting {
				routing = &branch.Groups[i]
				break
			}
		}
		if routing == nil || branch.Exit.Name == "" {
			continue
		}
		place := service.ExitPlace([]model.RouteHop{{Name: branch.Exit.Name, Node: branch.Exit}}, "")
		if place.Kind != model.ExitPlaceByName || seen[place.Country] {
			continue
		}
		seen[place.Country] = true
		out = append(out, hackOtherExit{
			point: place.Point, name: hackExitLabel(branch.Exit),
			note: shownName(routing.Label) + " · " + delayText(branch.Exit.Delay),
		})
		if len(out) == hackOtherExits {
			break
		}
	}
	return out
}

// hackMapSize is the map's size in points: what the hero box leaves beside
// the exit's words.
func hackMapSize(c *ui.Context) (float32, float32) {
	width, _ := c.Size()
	return width - 2*gapLarge - 3*hairlineWidth - hackHeroText, hackHeroHeight - 2*hairlineWidth
}

// hackMap is the map beside the exit's words: the land, the lines and the
// ends drawn, the labels laid over them.
func (d *Dashboard) hackMap(c *ui.Context, t *ui.Theme) {
	exit := service.ExitPlace(d.view.Route, d.measuredExitCountry())
	route := worldMapRoute{home: d.home.point, homeKnown: d.home.known, exit: exit, ink: t.Text, accent: t.Accent}
	w, h := hackMapSize(c)
	others := []hackOtherExit{}
	if route.placed() {
		others = d.hackOtherExits(exit)
	}
	var places []model.GeoPoint
	if route.homeKnown {
		places = append(places, route.home)
	}
	if route.placed() {
		places = append(places, exit.Point)
	}
	for _, other := range others {
		places = append(places, other.point)
	}
	view := newHackMapView(w, h, places, route)
	box := ui.Box(c.Key("hackMap")).Grow(1).Basis(0).MinWidth(0).Clip().Label(locales.HeroMapLabel())
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Image(hackLandBitmap(view, p.Scale(), t.Accent), ui.Rect{X: r.X, Y: r.Y, W: view.w, H: view.h}, ui.FillBox)
		at := func(place model.GeoPoint) (float32, float32) {
			x, y := view.point(place)
			return r.X + x, r.Y + y
		}
		if !route.homeKnown {
			return
		}
		hx, hy := at(route.home)
		for _, other := range others {
			ox, oy := at(other.point)
			hackArc(p, hx, hy, ox, oy, r.Y, new(ui.Path).Dash(0, hackOtherDash, hackOtherGap), hackOtherLine, uploadColor(t).Alpha(hackOtherAlpha), false)
			hackMarkerBox(p, ox, oy, uploadColor(t))
		}
		switch {
		case route.placed() && !route.near():
			ex, ey := at(exit.Point)
			hackArc(p, hx, hy, ex, ey, r.Y, new(ui.Path), hackRouteLine, t.Accent, true)
			hackMarkerBox(p, hx, hy, t.Text)
			hackMarkerBox(p, ex, ey, t.Accent)
		case route.placed():
			ex, ey := at(exit.Point)
			hackMarkerBox(p, ex, ey, t.Accent)
		default:
			hackMarkerBox(p, hx, hy, t.Text)
		}
	})
	box.Children(func() {
		if !route.homeKnown {
			return
		}
		hx, hy := view.point(route.home)
		if !route.placed() || !route.near() {
			hackMapLabel(c, t, "you", view, hx, hy, hackLabelGap, -hackLabelLift, locales.HackYou(), "", t.Text)
		}
		if route.placed() && len(d.view.Route) > 0 {
			hop := d.view.Route[len(d.view.Route)-1]
			ex, ey := view.point(exit.Point)
			km := strconv.Itoa(int(math.Round(service.DistanceKm(route.home, exit.Point))))
			note := delayText(hop.Node.Delay) + " · " + locales.HackDistance(km)
			hackMapLabel(c, t, "exit", view, ex, ey, hackLabelGap, 0, heroExitName(hop), note, t.Accent)
		}
		for i, other := range others {
			ox, oy := view.point(other.point)
			hackMapLabel(c, t, "other-"+strconv.Itoa(i), view, ox, oy, -hackLabelGap, hackLabelGap/2, other.name, other.note, uploadColor(t))
		}
	})
}

// hackArc draws a line bowed upward from one end to the other, never above
// the map's top, on path; lit lays a soft light under it.
func hackArc(p *ui.Painter, x0 float32, y0 float32, x1 float32, y1 float32, top float32, path *ui.Path, width float32, c ui.Color, lit bool) {
	length := float32(math.Hypot(float64(x1-x0), float64(y1-y0)))
	controlY := max(min(y0, y1)-length*hackBow, 2*(top+hackMarkerFrame)-(y0+y1)/2)
	path.MoveTo(x0, y0).QuadTo((x0+x1)/2, controlY, x1, y1)
	if lit {
		p.StrokePath(path, hackRouteGlow, c.Alpha(hackRouteLight))
	}
	p.StrokePath(path, width, c)
}

// hackMarkerBox marks an end: a filled box in a frame.
func hackMarkerBox(p *ui.Painter, x float32, y float32, c ui.Color) {
	p.Fill(ui.Rect{X: x - hackMarker/2, Y: y - hackMarker/2, W: hackMarker, H: hackMarker}, c, 0)
	p.Stroke(ui.Rect{X: x - hackMarkerFrame/2, Y: y - hackMarkerFrame/2, W: hackMarkerFrame, H: hackMarkerFrame}, c.Alpha(hackFrameAlpha), 0, hairlineWidth)
}

// hackMapLabel is a framed label for an end at (x, y): dx and dy set where
// it goes, a negative dx to the left; it moves to the other side when it
// would leave the map.
func hackMapLabel(c *ui.Context, t *ui.Theme, key string, view hackMapView, x float32, y float32, dx float32, dy float32, title string, note string, line ui.Color) {
	width := max(hackTextWidth(title, fontBody), hackTextWidth(note, fontCaption)) + 2*gapTight + 2*hairlineWidth
	height := fontBody*hackLabelLine + 2*gapLine + 2*hairlineWidth
	if note != "" {
		height += fontCaption*hackLabelLine + gapLine
	}
	left := x + dx
	if dx < 0 {
		left = x + dx - width
	}
	if left+width > view.w {
		left = x - abs32(dx) - width
	}
	if left < 0 {
		left = x + abs32(dx)
	}
	top := min(max(y+dy-height/2, 0), view.h-height)
	ui.Column(c.Key("label-"+key)).Absolute().Left(left).Top(top).Padding(gapLine, gapTight).Gap(gapLine).
		Background(t.Background.Alpha(0.9)).Border(hairlineWidth, line).Children(func() {
		ui.Text(c, title).FontSize(fontBody).FontWeight(weightSemibold).NoWrap()
		if note != "" {
			ui.Text(c, note).FontSize(fontCaption).TextColor(line).NoWrap()
		}
	})
}

// hackTextWidth estimates how wide text is at size, a wide character counted
// twice.
func hackTextWidth(text string, size float32) float32 {
	var units float32
	for _, r := range text {
		if r > 0x2E80 {
			units += 2
		} else {
			units++
		}
	}
	return units * size * hackLabelChar
}

// abs32 is v without its sign.
func abs32(v float32) float32 { return float32(math.Abs(float64(v))) }
