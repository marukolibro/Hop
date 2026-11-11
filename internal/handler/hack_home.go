package handler

import (
	"image"
	"image/color"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
	"hop/locales"
)

// The Hack theme's home is a terminal console rather than the glass layout:
// the exit across the top, its words on the left and the map on the right,
// then three boxes side by side: how traffic reaches Hop with the
// subscriptions, the routes by where they leave, and the services. Each box
// carries its title in its top border.
const (
	// hackHeroHeight is the exit's box; hackHeroText the column of its words.
	hackHeroHeight float32 = 262
	hackHeroText   float32 = 380
	// hackCaptureWidth and hackServicesWidth are the side boxes of the lower
	// row; the routes take what is left.
	hackCaptureWidth  float32 = 262
	hackServicesWidth float32 = 250
	// hackStackWidth is the window width under which the routes move under the
	// other two boxes; hackLowerMin and hackStackedMin are the least height of
	// the lower part, hackStackedTop the share of it the upper row takes.
	hackStackWidth float32 = 920
	hackLowerMin   float32 = 300
	hackStackedMin float32 = 640
	hackStackedTop float32 = 0.56
	// hackRowGap is the space between the console's rows: a gap and the lift of
	// the titles set into the next row's top border.
	hackRowGap float32 = gapMedium + hackTitleLift
	// hackBoxPad pads a box; hackTitleLift raises its title onto the border,
	// and hackTitleInset is how far from the corner the title starts.
	hackBoxPad     float32 = 14
	hackTitleLift  float32 = 9
	hackTitleInset float32 = 10
	// hackPanelAlpha is how much of the surface a box lays over the backdrop.
	hackPanelAlpha float32 = 0.72
	// hackToggleHeight is a capture switch; hackCell and hackCellHeight the
	// four cells beside its state.
	hackToggleHeight float32 = 32
	hackCellWidth    float32 = 7
	hackCellHeight   float32 = 12
	hackCells                = 4
	// hackLight is the light around what is on.
	hackLight     float32 = 0.45
	hackLightBlur float32 = 16
	// hackExitRoom is what the exit's line keeps beside its name: the
	// column's padding and the latency.
	hackExitRoom float32 = 120
	// hackSignalCells is how many blocks a latency bar has in the hero, and
	// hackBranchCells beside an exit of the routes; hackSignalSpan is the
	// latency, in milliseconds, at which a bar runs empty.
	hackSignalCells = 20
	hackBranchCells = 10
	hackSignalSpan  = 500
	// hackBarCell and hackBarHeight are a bar cell's size; hackStippleStep
	// the stipple's period in points and hackStippleTint how much of the
	// accent its dots take.
	hackBarCell     float32 = 7
	hackBarHeight   float32 = 13
	hackStippleStep float32 = 2
	hackStippleTint float32 = 0.3
	// hackUsageCells is the blocks of a subscription's usage bar.
	hackUsageCells = 20
	// hackRail is the line before what is open under a subscription, and the
	// line before the speeds.
	hackRail float32 = 2
	// hackStatusPad pads the header's status box.
	hackStatusPad float32 = 12
	// hackStatusSquare is the status box's light.
	hackStatusSquare float32 = 8
)

// hackHome is the Hack theme's nodes page.
func (d *Dashboard) hackHome(c *ui.Context, t *ui.Theme) {
	width, height := c.Size()
	// The window's height minus the header, the hero and the gaps is the lower
	// part's; a shorter window scrolls instead of squeezing it.
	spare := height - headerHeight - hackTitleLift - gapLarge - hackHeroHeight - hackRowGap
	ui.Scroll(c.Key("hackHome")).Grow(1).Basis(0).MinHeight(0).Background(nodeAreaFill(t)).Children(func() {
		ui.Column(c).Gap(hackRowGap).Padding(hackTitleLift, gapLarge, gapLarge, gapLarge).Children(func() {
			d.hackHero(c, t)
			if width < hackStackWidth {
				d.hackLowerStacked(c, t, max(hackStackedMin, spare))
				return
			}
			d.hackLowerRow(c, t, max(hackLowerMin, spare))
		})
	})
}

// hackLowerRow is the capture box, the routes and the services side by side.
func (d *Dashboard) hackLowerRow(c *ui.Context, t *ui.Theme, height float32) {
	ui.Row(c.Key("hackLower")).Height(height).Gap(gapLarge).AlignItems(ui.Stretch).Children(func() {
		d.hackCaptureBox(c, t).Width(hackCaptureWidth)
		d.hackRoutesBox(c, t).Grow(1).Basis(0).MinWidth(0)
		d.hackServicesBox(c, t).Width(hackServicesWidth)
	})
}

// hackLowerStacked is the lower part of a narrow window: the capture box and
// the services side by side, the routes under them across the width.
func (d *Dashboard) hackLowerStacked(c *ui.Context, t *ui.Theme, height float32) {
	top := height * hackStackedTop
	ui.Row(c.Key("hackLowerTop")).Height(top).Gap(gapLarge).AlignItems(ui.Stretch).Children(func() {
		d.hackCaptureBox(c, t).Width(hackCaptureWidth)
		d.hackServicesBox(c, t).Grow(1).Basis(0).MinWidth(0)
	})
	d.hackRoutesBox(c, t).Height(height - top - hackRowGap)
}

// hackBox is a box of the console: a hairline frame over a veil of the
// surface, with its title, and what trails it, set into the top border.
func hackBox(c *ui.Context, t *ui.Theme, key string, title string, aside func(), body func()) ui.Element {
	box := ui.Column(c.Key(key)).MinHeight(0).Padding(hackBoxPad).Gap(gapSmall).
		Border(hairlineWidth, t.Border).Background(t.Surface.Alpha(hackPanelAlpha))
	box.Children(func() {
		hackBorderLabel(c, t, "title").Left(hackTitleInset).Children(func() {
			ui.Text(c, title).FontSize(fontSecondary).FontWeight(weightSemibold).TextColor(t.Accent).NoWrap()
		})
		if aside != nil {
			hackBorderLabel(c, t, "aside").Right(hackTitleInset).Children(aside)
		}
		body()
	})
	return box
}

// hackBorderLabel is a label set into a box's top border: a patch of the
// window's base that hides the line behind it.
func hackBorderLabel(c *ui.Context, t *ui.Theme, key string) ui.Element {
	return ui.Box(c.Key(key)).Absolute().Top(-hackTitleLift).Padding(0, gapTight).Background(t.Background)
}

// hackHeader is the Hack theme's header row: the brand, the pages as words
// with the chosen one inverted, and the status box at the right.
func (d *Dashboard) hackHeader(c *ui.Context, t *ui.Theme) {
	width, _ := c.Size()
	if width >= 760 {
		brandLockup(c, t, 28, d.markRun)
	} else {
		brandMark(c, t, 28, d.markRun)
	}
	ui.Row(c.Key("hackTabs")).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
		for i, label := range pageLabels() {
			b := ui.ButtonBase(c.Key("tab-"+label)).Label(label).Padding(0, gapTight).Height(fontBody + gapTight).
				Cursor(ui.CursorPointer)
			ink := t.TextMuted
			switch {
			case d.page == i:
				b.Background(t.Accent)
				ink = t.AccentText
			case b.Hovered():
				ink = t.Accent
			}
			b.OnClick(func() { d.page = i })
			b.Children(func() {
				ui.Text(c, label).FontSize(fontBody).FontWeight(weightSemibold).TextColor(ink).NoWrap()
			})
		}
	})
	ui.Box(c).Grow(1)
	d.hackStatus(c, t)
}

// hackStatus is whether Hop carries the traffic, in a box lit in the state's
// colour, with the port it listens on beside it while the core runs.
func (d *Dashboard) hackStatus(c *ui.Context, t *ui.Theme) {
	color := d.statusColor(t)
	box := ui.Row(c.Key("headerStatus")).Gap(gapSmall).AlignItems(ui.Center).Height(switchHeight).
		Padding(0, hackStatusPad).Border(hairlineWidth, color).Background(color.Alpha(neutralFillAlpha))
	if d.proxying() || d.status == statusFailed {
		box.Shadow(0, 0, hackLightBlur, 0, color.Alpha(hackLight))
	}
	box.Children(func() {
		ui.Box(c).Width(hackStatusSquare).Height(hackStatusSquare).Background(color)
		ui.Text(c, d.statusText()).FontSize(fontBody).FontWeight(weightBold).TextColor(color).NoWrap()
		d.headerPort(c, t)
	})
}

// hackHero is the exit: its words on the left, the map on the right.
func (d *Dashboard) hackHero(c *ui.Context, t *ui.Theme) {
	hackBox(c, t, "hackHero", locales.HackExitTitle(), nil, func() {
		ui.Row(c.Key("heroBody")).Grow(1).AlignItems(ui.Stretch).Children(func() {
			d.hackExit(c, t)
			ui.Box(c).Width(hairlineWidth).Background(t.Border)
			d.hackMap(c, t)
		})
	}).Height(hackHeroHeight).Padding(0)
}

// hackExit is the exit in words: where traffic leaves, how fast it answers,
// the two speeds, its address and facts, and the path to it.
func (d *Dashboard) hackExit(c *ui.Context, t *ui.Theme) {
	route := d.view.Route
	ui.Column(c.Key("hackExit")).Width(hackHeroText).Gap(gapTight).Padding(gapLarge).Children(func() {
		captionText(c, t, locales.HackExitPrompt()).NoWrap()
		if len(route) == 0 {
			mutedText(c, t, locales.HeroEmpty())
			return
		}
		hop := route[len(route)-1]
		ui.Row(c.Key("exitName")).Gap(gapSmall).AlignItems(ui.End).Children(func() {
			ui.Text(c, heroExitName(hop)).FontSize(hackExitSize(heroExitName(hop))).FontWeight(weightBold).MaxLines(1).Shrink(1)
			ui.Text(c, delayText(hop.Node.Delay)).FontSize(fontEmphasis).FontWeight(weightBold).
				TextColor(delayColor(t, hop.Node.Delay)).NoWrap()
		})
		hackBar(c, t, hackSignal(hop.Node.Delay, hackSignalCells), hackSignalCells, delayColor(t, hop.Node.Delay))
		ui.Row(c.Key("speeds")).Gap(gapLarge).Children(func() {
			hackSpeed(c, t, "↓ "+locales.ColumnDownload(), formatRate(d.last.Down), t.Accent)
			hackSpeed(c, t, "↑ "+locales.ColumnUpload(), formatRate(d.last.Up), uploadColor(t))
		})
		mutedText(c, t, d.hackFacts(hop)).MaxLines(1)
		ui.Box(c).Grow(1)
		hackPath(c, t, route)
	})
}

// hackExitSize is the size of the exit's name: the stats' large size while it
// fits the column, the hero's otherwise.
func hackExitSize(name string) float32 {
	if hackTextWidth(name, fontStat) > hackHeroText-hackExitRoom {
		return fontExit
	}
	return fontStat
}

// hackSpeed is one speed under a rail of its colour: what it is, the number
// and its unit.
func hackSpeed(c *ui.Context, t *ui.Theme, label string, r readout, color ui.Color) {
	ui.Row(c.Key(label)).Grow(1).Basis(0).Gap(gapSmall).AlignItems(ui.Stretch).Children(func() {
		ui.Box(c).Width(hackRail).Background(color)
		ui.Column(c).Children(func() {
			captionText(c, t, label).NoWrap()
			ui.Text(c, r.value).FontSize(fontExit).FontWeight(weightBold).TextColor(color).FontFeatures("tnum").
				ContentTransition(ui.ContentTransition{Numeric: true})
			captionText(c, t, r.unit)
		})
	})
}

// hackFacts is the exit's address and facts on one line, set apart by slashes.
func (d *Dashboard) hackFacts(hop model.RouteHop) string {
	var parts []string
	if d.exitIP.status == exitIPReady {
		parts = append(parts, d.exitIP.ip.Address.String())
	}
	if !hop.Group && !hop.Node.PlanNote && hop.Node.Via == "" {
		parts = append(parts, nodeLabels(hop.Node)...)
		if hop.Node.Multiplier != "" {
			parts = append(parts, hop.Node.Multiplier)
		}
	}
	return strings.Join(parts, " / ")
}

// hackPath is the way from this machine to the exit, the exit lit.
func hackPath(c *ui.Context, t *ui.Theme, route []model.RouteHop) {
	ui.Row(c.Key("path")).Gap(gapTight).AlignItems(ui.Center).Clip().Children(func() {
		captionText(c, t, locales.HackLocal()).NoWrap()
		for i, hop := range route {
			name := shownName(hop.Name)
			if i == len(route)-1 {
				name = heroExitName(hop)
			}
			ui.Text(c.Key("arrow-"+hop.Name), "→").FontSize(fontSecondary).TextColor(t.Accent)
			step := ui.Text(c.Key("step-"+hop.Name), name).FontSize(fontSecondary).NoWrap()
			if i == len(route)-1 {
				step.TextColor(t.Accent).FontWeight(weightSemibold)
			}
		}
	})
}

// hackSignal is how many of cells a latency fills: all of them at no delay,
// none at hackSignalSpan or past it, and none before a test or after a
// failed one.
func hackSignal(delay model.Delay, cells int) int {
	if !delay.Measured || delay.Millis <= 0 {
		return 0
	}
	filled := int(float32(cells)*(1-float32(delay.Millis)/hackSignalSpan) + 0.5)
	return max(1, min(cells, filled))
}

// hackBar is a bar of cells as a terminal draws one: filled cells solid in
// color, the rest a fine stipple, as the light shade block shows. It is
// drawn rather than typed, so it looks the same in any face.
func hackBar(c *ui.Context, t *ui.Theme, filled int, cells int, color ui.Color) {
	stipple := t.Border.Mix(t.Accent, hackStippleTint)
	ui.Box(c.Key("bar")).Width(float32(cells) * hackBarCell).Height(hackBarHeight).Draw(func(p *ui.Painter, r ui.Rect) {
		solid := float32(filled) * hackBarCell
		p.Fill(ui.Rect{X: r.X, Y: r.Y, W: solid, H: r.H}, color, 0)
		rest := ui.Rect{X: r.X + solid, Y: r.Y, W: r.W - solid, H: r.H}
		if rest.W > 0 {
			p.Image(hackStippleBitmap(rest.W, rest.H, p.Scale(), stipple), rest, ui.FillBox)
		}
	})
}

// hackStippleKey names a baked stipple: its size in pixels and its colour.
type hackStippleKey struct {
	w, h  int
	color ui.Color
}

// hackStipples keeps the stipples drawn so far; a home has a handful of bar
// lengths.
var hackStipples = struct {
	sync.Mutex
	bitmaps map[hackStippleKey]*ui.Bitmap
}{bitmaps: map[hackStippleKey]*ui.Bitmap{}}

// hackStippleBitmap is a w by h point stipple at scale: one dot in every
// other pixel, each row shifted by one, like a light shade block.
func hackStippleBitmap(w float32, h float32, scale float32, ink ui.Color) *ui.Bitmap {
	key := hackStippleKey{w: max(1, int(w*scale)), h: max(1, int(h*scale)), color: ink}
	hackStipples.Lock()
	defer hackStipples.Unlock()
	if b, ok := hackStipples.bitmaps[key]; ok {
		return b
	}
	img := image.NewRGBA(image.Rect(0, 0, key.w, key.h))
	step := max(2, int(hackStippleStep*scale))
	dot := color.RGBA{R: ink.R, G: ink.G, B: ink.B, A: 255}
	for y := 0; y < key.h; y += step {
		for x := (y / step % 2) * step / 2; x < key.w; x += step {
			for dy := range max(1, step/2) {
				for dx := range max(1, step/2) {
					img.SetRGBA(x+dx, y+dy, dot)
				}
			}
		}
	}
	b := ui.NewBitmap(img)
	hackStipples.bitmaps[key] = b
	return b
}

// hackCaptureBox is how traffic reaches Hop and how it leaves: the system
// proxy and TUN as switches that light up while on, the outbound mode, then
// the saved subscriptions with the one in use open.
func (d *Dashboard) hackCaptureBox(c *ui.Context, t *ui.Theme) ui.Element {
	return hackBox(c, t, "hackCapture", locales.HackCaptureTitle(), nil, func() {
		proxy, proxyTip := d.systemProxySpec()
		hackToggle(c, t, proxy, proxyTip, func() { d.beginSystemProxy(!d.sysProxy.on) })
		tun, tunTip := d.tunSpec()
		hackToggle(c, t, tun, tunTip, func() { d.beginTun(!d.tun.on) })
		captionText(c, t, locales.OutboundMode()).NoWrap()
		d.hackModes(c, t)
		hackDashedLine(c, t)
		d.hackSubscriptions(c, t)
	})
}

// hackToggle is a capture switch as a whole row: inverted and lit while on,
// a dashed outline while off, its state at the right end.
func hackToggle(c *ui.Context, t *ui.Theme, spec pillSpec, tip string, onClick func()) {
	b := ui.ButtonBase(c.Key(spec.label)).Label(spec.label).Tooltip(tip).Height(hackToggleHeight).
		Padding(0, gapMedium).Gap(gapMedium).AlignItems(ui.Center).Disabled(spec.disabled)
	ink, cell := t.TextMuted, t.Border
	if spec.on {
		ink, cell = t.AccentText, t.AccentText
		b.Background(t.Accent).Border(hairlineWidth, t.Accent).Shadow(0, 0, hackLightBlur, 0, t.Accent.Alpha(hackLight))
	} else {
		line := t.Border
		if b.Hovered() && !spec.disabled && !spec.busy {
			line, ink = t.Accent, t.Accent
		}
		b.Border(hairlineWidth, line).BorderStyle(ui.BorderDashed)
	}
	if spec.disabled {
		b.Opacity(disabledOpacity)
	}
	if !spec.disabled && !spec.busy {
		b.Cursor(ui.CursorPointer)
	}
	b.OnClick(func() {
		if !spec.busy {
			onClick()
		}
	})
	b.Children(func() {
		ui.Text(c, spec.label).FontSize(fontEmphasis).FontWeight(weightBold).TextColor(ink).NoWrap()
		ui.Box(c).Grow(1)
		if spec.busy {
			spinner(c, hackCellHeight, ink)
			return
		}
		ui.Row(c.Key("cells")).Gap(hairlineWidth * 2).Children(func() {
			for i := range hackCells {
				ui.Box(c.Key("cell-" + string(rune('0'+i)))).Width(hackCellWidth).Height(hackCellHeight).Background(cell)
			}
		})
		state := locales.HackOff()
		if spec.on {
			state = locales.HackOn()
		}
		ui.Text(c, state).FontSize(fontSecondary).FontWeight(weightBold).TextColor(ink).NoWrap()
	})
}

// hackModes is the outbound mode as three cells in one frame, the chosen one
// inverted and pointed at.
func (d *Dashboard) hackModes(c *ui.Context, t *ui.Theme) {
	locked := d.modeLocked()
	row := ui.Row(c.Key("hackModes")).Border(hairlineWidth, t.Border).Disabled(locked)
	if locked {
		row.Opacity(disabledOpacity)
	}
	row.Children(func() {
		for i, label := range modeLabels() {
			chosen := d.modeIndex == i
			b := ui.ButtonBase(c.Key("mode-" + label)).Label(label).Tooltip(modeHints()[i]).Grow(1).Basis(0).MinWidth(0).
				Height(hackToggleHeight - gapTight).Justify(ui.Center).AlignItems(ui.Center).Cursor(ui.CursorPointer)
			ink := t.TextMuted
			switch {
			case chosen:
				b.Background(t.Accent).Shadow(0, 0, hackLightBlur, 0, t.Accent.Alpha(hackLight))
				ink = t.AccentText
			case b.Hovered():
				ink = t.Accent
			}
			b.OnClick(func() { d.chooseMode(i) })
			b.Children(func() {
				text := label
				if chosen {
					text = "> " + label
				}
				ui.Text(c, text).FontSize(fontBody).FontWeight(weightSemibold).TextColor(ink).NoWrap()
			})
		}
	})
}

// hackDashedLine separates parts of a box.
func hackDashedLine(c *ui.Context, t *ui.Theme) {
	ui.Box(c).Height(0).BorderWidth(hairlineWidth, 0, 0, 0).BorderColor(t.Border).BorderStyle(ui.BorderDashed).
		Margin(gapTight, 0)
}

// hackSubscriptions is the saved subscriptions as choices: the one in use
// open with what its account has left, the others one line each, choosing one
// uses it. The actions on the one in use sit at the box's foot.
func (d *Dashboard) hackSubscriptions(c *ui.Context, t *ui.Theme) {
	s := &d.subscription
	if !s.loaded {
		return
	}
	active, ok := s.active()
	if !ok {
		d.noSubscription(c, t)
		return
	}
	now := time.Now()
	ui.Column(c.Key("hackSubscriptions")).Gap(gapLine).Children(func() {
		for _, item := range s.view.Items {
			d.hackSubscriptionRow(c, t, item, item.ID == active.ID, now)
		}
	})
	ui.Box(c).Grow(1)
	ui.Row(c.Key("hackSubActions")).Wrap().Gap(gapMedium).GapY(gapTight).AlignItems(ui.Center).Children(func() {
		if active.Source == model.SourceRemote {
			label := locales.Refresh()
			if s.busy(active.ID) {
				label = locales.Refreshing()
			}
			hackTextButton(c, t, label, d.controlsLocked() || s.busy(active.ID), func() { d.refreshSubscription(active.ID) })
		}
		hackTextButton(c, t, locales.SubManage(), false, func() { s.managing = true })
	})
}

// hackSubscriptionRow is one saved subscription: a radio mark and its name;
// the one in use carries Hop's rules tag and opens under a rail to its usage.
func (d *Dashboard) hackSubscriptionRow(c *ui.Context, t *ui.Theme, item model.SubscriptionSummary, inUse bool, now time.Time) {
	s := &d.subscription
	ui.Column(c.Key("sub-" + item.ID)).Gap(gapLine).Children(func() {
		row := ui.Row(c.Key("line")).Gap(gapSmall).AlignItems(ui.Center)
		mark, ink := "( )", t.TextMuted
		if inUse {
			mark, ink = "(•)", t.Text
		} else if !d.controlsLocked() {
			row.Cursor(ui.CursorPointer).OnClick(func() { d.useSubscription(item.ID) })
			if row.Hovered() {
				ink = t.Accent
			}
		}
		row.Children(func() {
			markInk := t.TextMuted
			if inUse {
				markInk = t.Accent
			}
			ui.Text(c, mark).FontSize(fontBody).TextColor(markInk).NoWrap()
			ui.Text(c, pickerLabel(s.view.Items, item.ID)).FontSize(fontBody).FontWeight(weightSemibold).TextColor(ink).
				MaxLines(1).Shrink(1)
			if inUse && item.Rules == model.RulesHop {
				ui.Box(c.Key("rules")).Padding(0, gapLine).Background(t.Accent).Children(func() {
					ui.Text(c, locales.SubRuleSmart()).FontSize(fontCaption).FontWeight(weightSemibold).TextColor(t.AccentText).NoWrap()
				})
			}
			if !inUse {
				ui.Box(c).Grow(1)
				captionText(c, t, hackUsageShort(item)).NoWrap()
			}
		})
		if !inUse {
			return
		}
		ui.Row(c.Key("open")).Gap(gapMedium).AlignItems(ui.Stretch).Padding(0, 0, 0, gapTight).Children(func() {
			ui.Box(c).Width(hairlineWidth).Background(t.Border)
			ui.Column(c).Gap(gapLine).MinWidth(0).Children(func() {
				usage := item.Usage
				if item.Source != model.SourceRemote || !item.HasUsage {
					usage = service.MonthlyProxyForSource(d.stats, item.ID)
				}
				used := usage.Upload + usage.Download
				filled := 0
				if usage.Total > 0 {
					filled = int(float32(hackUsageCells)*min(1, float32(used)/float32(usage.Total)) + 0.5)
					if used > 0 {
						filled = max(1, filled)
					}
				}
				if usage.Total > 0 {
					hackBar(c, t, filled, hackUsageCells, usageColor(t, float32(used)/float32(usage.Total)))
				}
				captionText(c, t, hackUsageLine(usage, now)).MaxLines(1)
				if item.LastError != "" {
					warningLine(c, t, item.LastError)
				}
			})
		})
	})
}

// hackUsageLine is what an account used of its total and how long it has
// left, on one line.
func hackUsageLine(usage model.Usage, now time.Time) string {
	used := formatBytes(usage.Upload + usage.Download)
	line := used.value + " " + used.unit
	if usage.Total > 0 {
		total := formatBytes(usage.Total)
		line += " / " + total.value + " " + total.unit
	}
	if !usage.Expire.IsZero() {
		line += " · " + remainingText(usage.Expire.Sub(now))
	}
	return line
}

// hackUsageShort is what a subscription not in use shows at its line's end:
// its account's use and total, or that it is a local profile.
func hackUsageShort(item model.SubscriptionSummary) string {
	if item.Source != model.SourceRemote {
		return service.LocalProfileName()
	}
	if !item.HasUsage || item.Usage.Total <= 0 {
		return ""
	}
	used, total := formatBytes(item.Usage.Upload+item.Usage.Download), formatBytes(item.Usage.Total)
	return used.value + " / " + total.value + " " + total.unit
}

// hackTextButton is an action in brackets, as a terminal shows one.
func hackTextButton(c *ui.Context, t *ui.Theme, label string, disabled bool, onClick func()) {
	b := ui.ButtonBase(c.Key("action-"+label)).Label(label).Padding(0, gapLine).Disabled(disabled)
	ink := t.Accent
	if disabled {
		ink = t.TextMuted
	} else {
		b.Cursor(ui.CursorPointer)
		if b.Hovered() {
			b.Background(t.Accent.Alpha(neutralFillAlpha * 2))
		}
	}
	b.OnClick(onClick)
	b.Children(func() {
		ui.Text(c, "[ "+label+" ]").FontSize(fontBody).FontWeight(weightSemibold).TextColor(ink).NoWrap()
	})
}

// hackRoutesBox is every group gathered under where it sends traffic now, as
// a tree: the exit with its latency, then each group that leads there and
// through which group, when another.
func (d *Dashboard) hackRoutesBox(c *ui.Context, t *ui.Theme) ui.Element {
	hint := func() { captionText(c, t, locales.HackRoutesHint()).NoWrap() }
	return hackBox(c, t, "hackRoutes", locales.HackRoutesTitle(), hint, func() {
		ui.Scroll(c.Key("hackRoutesScroll")).Grow(1).Basis(0).MinHeight(0).Disabled(d.controlsLocked()).Children(func() {
			ui.Column(c).Gap(gapSmall).Children(func() {
				for _, branch := range service.ExitBranches(d.view.Groups) {
					d.hackBranch(c, t, branch)
				}
			})
		})
	})
}

// hackBranch is one exit of the tree and the groups under it.
func (d *Dashboard) hackBranch(c *ui.Context, t *ui.Theme, branch model.ExitBranch) {
	ui.Column(c.Key("branch-" + branch.Key)).Gap(gapLine).Children(func() {
		name, delay := shownName(branch.Key), model.Delay{}
		proxy := branch.Exit.Protocol != ""
		if branch.Exit.Name != "" {
			name, delay = hackExitLabel(branch.Exit), branch.Exit.Delay
		}
		color := delayColor(t, delay)
		ui.Row(c.Key("exit")).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
			ui.Text(c, name).FontSize(fontEmphasis).FontWeight(weightBold).MaxLines(1).Shrink(1)
			if proxy {
				hackBar(c, t, hackSignal(delay, hackBranchCells), hackBranchCells, color)
			}
			ui.Box(c).Grow(1)
			if proxy {
				ui.Text(c, delayText(delay)).FontSize(fontBody).FontWeight(weightBold).TextColor(color).FontFeatures("tnum").NoWrap()
			}
		})
		for i, g := range branch.Groups {
			d.hackBranchGroup(c, t, branch, g, i == len(branch.Groups)-1)
		}
	})
}

// hackBranchGroup is one group under its exit: the tree's line, its name, and
// the group it goes through when it does not pick the exit itself.
func (d *Dashboard) hackBranchGroup(c *ui.Context, t *ui.Theme, branch model.ExitBranch, g model.GroupView, last bool) {
	row := ui.Row(c.Key("group-"+g.Name)).Gap(gapSmall).AlignItems(ui.Center).Padding(0, gapTight).Cursor(ui.CursorPointer)
	nameInk, viaInk := t.Accent, t.TextMuted
	if row.Hovered() {
		row.Background(t.Accent.Alpha(neutralFillAlpha * 2))
	}
	row.OnClick(func() { d.activeGroup, d.hackPickerOpen = g.Name, true })
	row.Children(func() {
		line := "├──"
		if last {
			line = "└──"
		}
		ui.Text(c, line).FontSize(fontBody).TextColor(t.Border).NoWrap()
		ui.Text(c, shownName(g.Label)).FontSize(fontBody).FontWeight(weightSemibold).TextColor(nameInk).NoWrap()
		if g.Selected != "" && g.Selected != g.Exit.Name && g.Selected != branch.Key {
			ui.Text(c, locales.RouteVia(shownName(g.Selected))).FontSize(fontSecondary).TextColor(viaInk).MaxLines(1).Shrink(1)
		}
	})
}

// hackExitLabel names an exit of the tree: its flag, region and title.
func hackExitLabel(node model.NodeView) string {
	if node.PlanNote {
		return locales.PlanInfoNode()
	}
	if name := nodeDisplay(node); name != "" {
		return name
	}
	return shownName(node.Name)
}

// hackServicesBox is every service of the check, one line each: its name, a
// dotted leader, the region it saw and whether it works. Clicking it opens the
// dialog that has the check's buttons.
func (d *Dashboard) hackServicesBox(c *ui.Context, t *ui.Theme) ui.Element {
	s := &d.unlock.services
	text, _, ink := d.servicesSummary(t)
	aside := func() { ui.Text(c, text).FontSize(fontSecondary).FontWeight(weightSemibold).TextColor(ink).NoWrap() }
	return hackBox(c, t, "hackServices", locales.ServicesTitle(), aside, func() {
		ui.Column(c.Key("hackServiceRows")).Grow(1).Justify(ui.SpaceBetween).Cursor(ui.CursorPointer).
			OnClick(func() { s.open = true }).Children(func() {
			for _, svc := range model.UnlockServices {
				r, checked := s.results[svc]
				hackServiceRow(c, t, svc, r, checked)
			}
		})
	})
}

// hackServiceRow is one service: its name, a dotted leader to the region it
// was placed in, and its state.
func hackServiceRow(c *ui.Context, t *ui.Theme, svc model.UnlockService, r model.UnlockResult, checked bool) {
	row := ui.Row(c.Key(string(svc))).Gap(gapTight).AlignItems(ui.Center)
	if checked {
		row.Tooltip(serviceTileTip(svc, r))
	}
	row.Children(func() {
		ui.Text(c, serviceNames[svc]).FontSize(fontBody).NoWrap()
		ui.Box(c.Key("leader")).Grow(1).Basis(0).MinWidth(0).Clip().Children(func() {
			ui.Text(c, strings.Repeat(".", hackLeaderDots)).FontSize(fontSecondary).TextColor(t.Border).NoWrap()
		})
		state, color := "—", t.TextMuted
		if checked {
			if r.Region != "" {
				captionText(c, t, r.Region).NoWrap()
			}
			state, color = hackServiceState(r), serviceStateColor(t, r)
			if works(r) {
				color = t.Accent
			}
		}
		ui.Text(c, state).FontSize(fontBody).FontWeight(weightBold).TextColor(color).NoWrap()
	})
}

// hackLeaderDots is more dots than any leader shows; the line cuts them.
const hackLeaderDots = 64

// hackServiceState is a result in a terminal's word: OK for a service that
// works, its state in words otherwise.
func hackServiceState(r model.UnlockResult) string {
	if r.State == model.UnlockAvailable {
		return "OK"
	}
	return serviceState(r)
}
