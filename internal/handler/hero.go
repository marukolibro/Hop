package handler

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
	"hop/locales"
)

const (
	heroGaugeSize float32 = 100
	heroRadius    float32 = 16
	heroPadY      float32 = 20
	heroPadX      float32 = 22
	heroGap       float32 = 18
	// heroLiveBasis is the width the speeds, the mode and the plot ask for
	// before the card wraps them under the exit; heroPlotMinPane is the
	// narrowest pane that still leaves the plot a useful width beside the
	// exit.
	heroLiveBasis   float32 = 300
	heroPlotMinPane float32 = 700
	heroPlotHeight  float32 = 64
	heroModeSegment float32 = 46
	heroDelayHeight float32 = 20
	// heroDelayFill washes the latency pill in the accent.
	heroDelayFill float32 = 0.14
)

// routeBar is the nodes page's hero: one card tinted with the theme's
// accent, with the ring gauge and the exit on the left (its name and latency,
// the groups it passes, its public address and its facts) and on the right
// the two speeds with the outbound mode, over the last two minutes of
// traffic. The gauge's arcs ease to the traffic and then rest. The right part
// wraps under the exit in a narrow window.
func (d *Dashboard) routeBar(c *ui.Context, t *ui.Theme) {
	route := d.view.Route
	width, _ := c.Size()
	pane := width - sidebarWidthFor(width) - hairlineWidth - 2*pagePadding
	ui.Row(c).Key("hero").Wrap().Gap(pagePadding).GapY(gapLarge).AlignItems(ui.Center).
		Padding(heroPadY, heroPadX).Radius(heroRadius).LinearGradient(heroFace(t, glassPanel(t))).
		Border(hairlineWidth, glassMaterialLine(t)).Shadow(0, 3, 12, 0, glassCardShadow(t)).Children(func() {
		ui.Row(c).Key("routeTop").Gap(heroGap).AlignItems(ui.Center).Children(func() {
			if len(route) > 0 {
				routeGauge(c, t, route[len(route)-1], heroGaugeSize, d.last)
			}
			d.heroExit(c, t, route)
		})
		ui.Column(c).Key("live").Grow(1).Basis(heroLiveBasis).MinWidth(0).Gap(gapMedium).Children(func() {
			d.heroReadout(c, t)
			if pane >= heroPlotMinPane {
				d.sparkline(c, t, heroPlotHeight)
			}
		})
	})
}

// heroExit names the exit, the groups it passes, its public address and its
// facts, over the line that opens the services dialog.
func (d *Dashboard) heroExit(c *ui.Context, t *ui.Theme, route []model.RouteHop) {
	exit := ui.Column(c).Key("exit").Gap(gapTight).Width(exitWidth)
	exit.Children(func() {
		if len(route) == 0 {
			mutedText(c, t, locales.HeroEmpty())
			return
		}
		hop := route[len(route)-1]
		ui.Row(c).Key("routeTitle").Gap(gapSmall).AlignItems(ui.Center).Children(func() {
			// The row stays as wide as its content, so the delay follows the
			// name; a long name shrinks and truncates instead of pushing it
			// to the column's edge.
			ui.Text(c, heroExitName(hop)).FontSize(fontExit).FontWeight(weightSemibold).LetterSpacing(-0.5).
				MaxLines(1).Shrink(1)
			heroDelay(c, t, hop.Node.Delay)
		})
		if via := heroVia(route, hop); via != "" {
			mutedText(c, t, via).MaxLines(1)
		}
		d.exitIPLine(c, t)
		heroChips(c, t, hop)
		d.servicesEntry(c, t)
	})
}

// heroDelay is the exit's latency on a pill washed in the accent.
func heroDelay(c *ui.Context, t *ui.Theme, delay model.Delay) {
	ui.Box(c).Key("delay").Height(heroDelayHeight).Padding(0, gapSmall).Radius(heroDelayHeight / 2).Center().
		Background(t.Accent.Alpha(heroDelayFill)).Children(func() {
		ui.Text(c, delayText(delay)).FontSize(fontSecondary).FontWeight(weightSemibold).TextColor(delayColor(t, delay)).
			FontFeatures("tnum").NoWrap().Translate(0, -opticalLift).ContentTransition(ui.ContentTransition{Numeric: true})
	})
}

// heroExitName is the exit's name, with the flag before it when it has one.
func heroExitName(hop model.RouteHop) string {
	node := hop.Node
	switch {
	case hop.Group:
		return hop.Name
	case node.PlanNote:
		return locales.PlanInfoNode()
	case node.Via != "":
		return locales.RouteDirect()
	}
	name := strings.TrimSpace(node.Region + " " + node.Title)
	if name == "" {
		name = node.Name
	}
	return strings.TrimSpace(shownFlag(node.Flag) + " " + name)
}

// heroVia is the line under the exit's name: the groups it passes, or that
// it goes out directly; "" when the route says neither.
func heroVia(route []model.RouteHop, hop model.RouteHop) string {
	switch {
	case hop.Group:
		return routeVia(route)
	case hop.Node.PlanNote, hop.Node.Via != "":
		return locales.HeroDirectNote()
	default:
		return routeVia(route)
	}
}

// heroChips are the exit's facts as small labels: its protocol, UDP, the
// multiplier that costs something, then what its name gives.
func heroChips(c *ui.Context, t *ui.Theme, hop model.RouteHop) {
	node := hop.Node
	if hop.Group || node.PlanNote || node.Via != "" {
		return
	}
	labels := nodeLabels(node)
	if len(labels) == 0 && node.Multiplier == "" {
		return
	}
	ui.Row(c).Key("routeFacts").Gap(gapTight).Wrap().Children(func() {
		for _, label := range labels {
			nodeChip(c, t, label)
		}
		if node.Multiplier != "" {
			nodeChip(c, t, node.Multiplier)
		}
	})
}

// heroReadout is the download and upload speeds with the outbound mode
// beside them.
func (d *Dashboard) heroReadout(c *ui.Context, t *ui.Theme) {
	ui.Row(c).Key("readout").Wrap().Gap(gapLarge).GapY(gapSmall).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Key("speeds").Gap(gapLarge).AlignItems(ui.Center).Children(func() {
			heroSpeedLine(c, t, "↓", formatRate(d.last.Down), accentInk(t))
			heroSpeedLine(c, t, "↑", formatRate(d.last.Up), uploadColor(t))
		})
		ui.Box(c).Grow(1)
		d.modeSwitch(c, pageSwitchStyle(t, heroModeSegment))
	})
}

// modeLocked says the outbound mode cannot change yet: the core has not
// reported its mode, or a subscription mutation or core change is running, so
// an apply could carry the mode the user just left back in.
func (d *Dashboard) modeLocked() bool {
	return d.view.Mode == "" || d.subscription.mutations > 0 || d.core.busy
}

// modeSwitch chooses the outbound mode as one sliding switch.
func (d *Dashboard) modeSwitch(c *ui.Context, style switchStyle) {
	disabled := d.modeLocked()
	mode := ui.Box(c).Key("mode").Disabled(disabled).Tooltip(modeHints()[d.modeIndex])
	if disabled {
		mode.Opacity(disabledOpacity)
	}
	mode.Children(func() {
		slidingSwitch(c, locales.OutboundMode(), &d.modeIndex, modeLabels(), style, func(i int) {
			d.setMode(service.Modes[i])
		})
	})
}

func heroSpeedLine(c *ui.Context, t *ui.Theme, arrow string, r readout, color ui.Color) {
	ui.Row(c).Key(arrow).Gap(gapTight).AlignItems(ui.End).Children(func() {
		ui.Text(c, arrow).FontSize(fontSecondary).FontWeight(weightMedium).TextColor(color)
		ui.Text(c, r.value).FontSize(fontHeadline).FontWeight(weightMedium).LetterSpacing(-0.4).
			FontFeatures("tnum").ContentTransition(ui.ContentTransition{Numeric: true})
		captionText(c, t, r.unit)
	})
}

// coreStatus is the core's state: a dot and a few words.
func (d *Dashboard) coreStatus(c *ui.Context, t *ui.Theme) {
	dotColor, title := t.Success, locales.CoreRunning()
	switch d.status {
	case statusFailed:
		dotColor, title = t.Danger, locales.CoreNotConnected()
	case statusConnected:
	default:
		dotColor, title = t.TextMuted, locales.CoreConnecting()
	}
	ui.Row(c).Key("coreStatus").Gap(gapTight).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Width(5).Height(5).Radius(2.5).Background(dotColor)
		captionText(c, t, title).NoWrap()
	})
}
