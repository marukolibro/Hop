package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
	"hop/locales"
)

// unlockState is the unlock checks: what the check over the groups found for
// each group, kept until Hop quits, and its run; and the check through the
// rules, which the services dialog shows.
type unlockState struct {
	results map[string]model.Unlock
	// running says a run goes; done and total count the groups it checks.
	running bool
	done    int
	total   int
	// cancel ends the run; seq tells a run's replies from an older one's.
	cancel context.CancelFunc
	seq    int

	services servicesCheck
}

// servicesCheck is the check of every service through the rules and its
// dialog.
type servicesCheck struct {
	open    bool
	results map[model.UnlockService]model.UnlockResult
	running bool
	cancel  context.CancelFunc
	seq     int
}

// serviceNames are the services as the check shows them: their own names,
// which stay as they are in every language.
var serviceNames = map[model.UnlockService]string{
	model.UnlockGoogle:  "Google",
	model.UnlockGitHub:  "GitHub",
	model.UnlockYouTube: "YouTube Premium",
	model.UnlockChatGPT: "ChatGPT",
	model.UnlockClaude:  "Claude",
	model.UnlockGemini:  "Gemini",
	model.UnlockNetflix: "Netflix",
	model.UnlockDisney:  "Disney+",
	model.UnlockPrime:   "Prime Video",
	model.UnlockSpotify: "Spotify",
	model.UnlockTikTok:  "TikTok",
	model.UnlockDiscord: "Discord",
}

// serviceMarks are the services' marks; a service without one shows
// serviceInitials instead.
var serviceMarks = map[model.UnlockService]*ui.SVG{
	model.UnlockGoogle:  markGoogle,
	model.UnlockGitHub:  markGitHub,
	model.UnlockYouTube: markYouTube,
	model.UnlockClaude:  markClaude,
	model.UnlockGemini:  markGemini,
	model.UnlockNetflix: markNetflix,
	model.UnlockSpotify: markSpotify,
	model.UnlockTikTok:  markTikTok,
	model.UnlockDiscord: markDiscord,
}

var serviceInitials = map[model.UnlockService]string{
	model.UnlockChatGPT: "GPT",
	model.UnlockDisney:  "D+",
	model.UnlockPrime:   "PV",
}

// works says the service can be used: wholly, or Netflix with its own
// productions.
func works(r model.UnlockResult) bool {
	return r.State == model.UnlockAvailable || r.State == model.UnlockPartial
}

// workingCount is how many of results work.
func workingCount(results []model.UnlockResult) int {
	n := 0
	for _, r := range results {
		if works(r) {
			n++
		}
	}
	return n
}

// serviceState is a result's state in words.
func serviceState(r model.UnlockResult) string {
	switch r.State {
	case model.UnlockAvailable:
		return locales.ServiceAvailable()
	case model.UnlockPartial:
		return locales.ServicePartial()
	case model.UnlockBlocked:
		return locales.ServiceBlocked()
	default:
		return locales.ServiceUnknown()
	}
}

// serviceStateColor marks a working service in the accent, a partial one as
// a warning and a refused one as an error.
func serviceStateColor(t *ui.Theme, r model.UnlockResult) ui.Color {
	switch r.State {
	case model.UnlockAvailable:
		return textOn(t, accentInk(t))
	case model.UnlockPartial:
		return textOn(t, t.Warning)
	case model.UnlockBlocked:
		return textOn(t, t.Danger)
	default:
		return t.TextMuted
	}
}

// regionFlag is the flag emoji of an ISO 3166 alpha-2 code, as Hop shows
// it; "" for anything else.
func regionFlag(code string) string {
	if len(code) != 2 || code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z' {
		return ""
	}
	return shownFlag(string([]rune{0x1F1E6 + rune(code[0]-'A'), 0x1F1E6 + rune(code[1]-'A')}))
}

// routeText is the route a service took: the first group and the proxy it
// left by, or the one hop it had; "" when the core no longer had it.
func routeText(route []string) string {
	names := make([]string, 0, 2)
	for i, hop := range route {
		if i > 0 && i < len(route)-1 {
			continue
		}
		switch hop {
		case "DIRECT":
			hop = locales.RouteDirect()
		case "REJECT":
			hop = locales.RouteReject()
		}
		names = append(names, shownName(hop))
	}
	return strings.Join(names, " → ")
}

// groupUnlockTip lists the services that work through a group, with the
// region each placed it in.
func groupUnlockTip(u model.Unlock) string {
	var lines []string
	for _, r := range u.Results {
		if !works(r) {
			continue
		}
		line := serviceNames[r.Service]
		if r.State == model.UnlockPartial {
			line = locales.UnlockNetflixPartial()
		}
		if r.Region != "" {
			line += locales.UnlockRegion(service.CountryName(r.Region))
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// toggleUnlock starts the check of every group, or ends the one running. It
// runs on the UI thread.
func (d *Dashboard) toggleUnlock() {
	if d.unlock.running {
		d.unlock.cancel()
		return
	}
	if d.services.Unlock == nil || d.opCtx == nil || d.opWin == nil || d.controlsLocked() || d.testing != "" {
		return
	}
	if d.unlock.results == nil {
		d.unlock.results = map[string]model.Unlock{}
	}
	ctx, cancel := context.WithCancel(d.opCtx)
	d.unlock.seq++
	d.unlock.running, d.unlock.done, d.unlock.total, d.unlock.cancel = true, 0, 0, cancel
	seq, win := d.unlock.seq, d.opWin
	go func() {
		defer cancel()
		err := d.services.Unlock.Detect(ctx, func(r service.UnlockReport) {
			win.Update(func() {
				if seq != d.unlock.seq {
					return
				}
				d.unlock.results[r.Group] = r.Result
				d.unlock.done, d.unlock.total = r.Done, r.Total
			})
		})
		win.Update(func() {
			if seq != d.unlock.seq {
				return
			}
			d.unlock.running, d.unlock.cancel = false, nil
			if err != nil && !errors.Is(err, context.Canceled) {
				d.fail(actionCheckUnlock, err)
			}
		})
	}()
}

// toggleServices starts the check of every service through the rules, or
// ends the one running. It runs on the UI thread.
func (d *Dashboard) toggleServices() {
	s := &d.unlock.services
	if s.running {
		s.cancel()
		return
	}
	if d.services.Unlock == nil || d.opCtx == nil || d.opWin == nil {
		return
	}
	ctx, cancel := context.WithCancel(d.opCtx)
	s.seq++
	s.running, s.cancel, s.results = true, cancel, map[model.UnlockService]model.UnlockResult{}
	seq, win := s.seq, d.opWin
	go func() {
		defer cancel()
		err := d.services.Unlock.CheckServices(ctx, func(r model.UnlockResult) {
			win.Update(func() {
				if seq == s.seq {
					s.results[r.Service] = r
				}
			})
		})
		win.Update(func() {
			if seq != s.seq {
				return
			}
			s.running, s.cancel = false, nil
			if err != nil && !errors.Is(err, context.Canceled) {
				d.fail(actionCheckUnlock, err)
			}
		})
	}()
}

// unlockControls is the check's progress and its button, beside the latency
// test's.
func (d *Dashboard) unlockControls(c *ui.Context, t *ui.Theme) {
	running := d.unlock.running
	if running && d.unlock.total > 0 {
		captionText(c, t, fmt.Sprintf("%d/%d", d.unlock.done, d.unlock.total)).FontFeatures("tnum").
			ContentTransition(ui.ContentTransition{Numeric: true})
	}
	spec := buttonSpec{label: locales.UnlockStart(), icon: iconUnlock, style: mainSoftButton(t),
		disabled: d.services.Unlock == nil || d.controlsLocked() || d.testing != ""}
	if running {
		spec.label, spec.icon, spec.disabled = locales.UnlockStop(), iconClose, false
	}
	button(c, spec, d.toggleUnlock)
}

// unlockRow is what the check found for an option that is a group, under its
// latency line. An option the check did not reach, such as a node, shows
// nothing.
func (d *Dashboard) unlockRow(c *ui.Context, t *ui.Theme, option model.NodeView) {
	result, checked := d.unlock.results[option.Name]
	if !checked || option.Via == "" {
		return
	}
	ui.Row(c).Key("unlock").Gap(gapTight).Height(chipHeight).Clip().AlignItems(ui.Center).Children(func() {
		unlockChips(c, t, result)
	})
}

// unlockChips is how many services work through a group, on a chip whose
// tooltip names them; or what stands in its place when none does.
func unlockChips(c *ui.Context, t *ui.Theme, result model.Unlock) {
	working := workingCount(result.Results)
	if working == 0 {
		captionText(c, t, unlockSummary(result)).MaxLines(1)
		return
	}
	fill, ink := t.Accent.Alpha(tileChipFill), textOn(t, accentInk(t))
	ui.Box(c).Key("unlockCount").Padding(1, 6).Radius(radiusChip).Background(fill).Tooltip(groupUnlockTip(result)).Children(func() {
		ui.Text(c, locales.UnlockWorking(working, len(result.Results))).FontSize(fontCaption).FontWeight(weightMedium).
			TextColor(ink).FontFeatures("tnum").NoWrap()
	})
}

// unlockSummary says what a group with no working service shows: that every
// check was refused, or that some could not be made.
func unlockSummary(u model.Unlock) string {
	for _, r := range u.Results {
		if r.State == model.UnlockUnknown {
			return locales.UnlockUnsure()
		}
	}
	return locales.UnlockNone()
}

// servicesSummaryText is how far the check through the rules got: how many
// services work, how many it has checked while it runs, or that it has not
// run.
func (d *Dashboard) servicesSummaryText() string {
	s := &d.unlock.services
	switch {
	case s.running:
		return locales.ServicesChecking(len(s.results), len(model.UnlockServices))
	case len(s.results) > 0:
		results := make([]model.UnlockResult, 0, len(s.results))
		for _, r := range s.results {
			results = append(results, r)
		}
		return locales.ServicesSummary(workingCount(results), len(results))
	}
	return locales.ServicesNotChecked()
}

// serviceTileTip is a checked service's tooltip: its name, its state with
// the region it placed the route in, the time it took and the route it took.
func serviceTileTip(svc model.UnlockService, r model.UnlockResult) string {
	state := serviceState(r)
	if r.Region != "" {
		state += locales.UnlockRegion(service.CountryName(r.Region))
	}
	lines := []string{serviceNames[svc], state}
	if r.Latency > 0 {
		lines = append(lines, delayText(model.Delay{Millis: int(r.Latency / time.Millisecond), Measured: true}))
	}
	if route := routeText(r.Route); route != "" {
		lines = append(lines, route)
	}
	return strings.Join(lines, "\n")
}

// servicesDialog lists every service with what the check through the rules
// found: the route it took, whether it works, the region it placed the route
// in and how long it took to answer.
func (d *Dashboard) servicesDialog(c *ui.Context, t *ui.Theme) {
	s := &d.unlock.services
	animatedDialog(c, "services", &s.open, func(backdrop, panel ui.Element) {
		backdrop.Background(dialogScrim)
		panel.Width(servicesDialogWidth).Padding(pagePadding).Gap(gapLarge).Radius(radiusPanel).
			Background(overlaySurface(t)).Border(1, t.Border).Shadow(0, 10, 30, 0, dialogShadow)
		panel.Children(func() {
			ui.Row(c).Gap(gapMedium).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(gapTight).Children(func() {
					ui.Text(c, locales.ServicesTitle()).FontSize(fontHeadline).FontWeight(weightSemibold)
					if s.running || len(s.results) > 0 {
						mutedText(c, t, d.servicesSummaryText())
					} else {
						mutedText(c, t, locales.ServicesHint())
					}
				})
				spec := buttonSpec{label: locales.ServicesCheck(), icon: iconRefresh, style: primaryButton(t),
					disabled: d.services.Unlock == nil}
				if s.running {
					spec.label, spec.icon, spec.style, spec.disabled = locales.UnlockStop(), iconClose, softButton(t), false
				}
				button(c, spec, d.toggleServices)
				iconButton(c, locales.Close(), iconClose, softButton(t), func() { s.open = false })
			})
			ui.Scroll(c).MaxHeight(servicesListHeight).Children(func() {
				ui.Column(c).Gap(gapTight).Children(func() {
					for _, svc := range model.UnlockServices {
						r, checked := s.results[svc]
						serviceRow(c, t, svc, r, checked, s.running)
					}
				})
			})
		})
	})
}

// serviceRow is one service: its mark and name, the route it took, and on
// the right the region, the latency and whether it works.
func serviceRow(c *ui.Context, t *ui.Theme, svc model.UnlockService, r model.UnlockResult, checked bool, running bool) {
	ui.Row(c).Key(string(svc)).Gap(gapMedium).Padding(gapSmall, gapSmall).Radius(radiusControl).AlignItems(ui.Center).
		Background(t.Text.Alpha(neutralFillAlpha / 2)).Children(func() {
		unlockMark(c, t, svc)
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, serviceNames[svc]).FontSize(fontBody).FontWeight(weightSemibold).MaxLines(1)
			switch {
			case checked && len(r.Route) > 0:
				captionText(c, t, routeText(r.Route)).MaxLines(1).Tooltip(strings.Join(r.Route, " → "))
			case checked:
				captionText(c, t, "—")
			case running:
				captionText(c, t, locales.ServicesWaiting())
			default:
				captionText(c, t, locales.ServicesNotChecked())
			}
		})
		if !checked {
			if running {
				spinner(c, iconInline, t.TextMuted)
			}
			return
		}
		if r.Region != "" {
			ui.Text(c, strings.TrimSpace(regionFlag(r.Region)+" "+service.CountryName(r.Region))).
				FontSize(fontSecondary).TextColor(t.TextMuted).NoWrap().Tooltip(locales.ServiceRegionTip())
		}
		delay := model.Delay{Millis: int(r.Latency / time.Millisecond), Measured: r.Latency > 0}
		if delay.Measured {
			// A page's whole answer, handshakes included, takes longer than a
			// node's latency test, so the node tiers would call most of these
			// slow: the time stays neutral and the state says whether it works.
			ui.Text(c, delayText(delay)).FontSize(fontSecondary).FontWeight(weightMedium).TextColor(t.TextMuted).
				FontFeatures("tnum").NoWrap()
		}
		ui.Text(c, serviceState(r)).FontSize(fontSecondary).FontWeight(weightSemibold).TextColor(serviceStateColor(t, r)).NoWrap()
	})
}

// unlockMark is a service's mark, or its initials, on a tile of the dialog's
// size.
func unlockMark(c *ui.Context, t *ui.Theme, svc model.UnlockService) {
	unlockMarkSized(c, t, svc, serviceMarkTile)
}

// unlockMarkSized is unlockMark on a tile of size.
func unlockMarkSized(c *ui.Context, t *ui.Theme, svc model.UnlockService, size float32) {
	ui.Box(c).Key("mark").Width(size).Height(size).Radius(radiusControl).Center().
		Background(t.Text.Alpha(neutralFillAlpha)).Children(func() {
		if mark, ok := serviceMarks[svc]; ok {
			glyph := size * serviceMarkRatio
			ui.Icon(c, mark).Width(glyph).Height(glyph).TextColor(t.Text)
			return
		}
		ui.Text(c, serviceInitials[svc]).FontSize(fontCaption).FontWeight(weightBold).TextColor(t.Text).NoWrap()
	})
}
