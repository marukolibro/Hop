package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	neturl "net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
	"hop/locales"
)

const (
	// Usage turns amber and then red as the account nears its limit, and
	// the expiry date turns amber in its last week.
	usageWarning   = 0.8
	usageCritical  = 0.95
	expiryWarning  = 7 * 24 * time.Hour
	dayLength      = 24 * time.Hour
	usageDateStyle = "2006-01-02"
)

// editorIntent distinguishes adding, renaming and editing a profile.
type editorIntent int

const (
	editorAdd editorIntent = iota
	editorRename
	editorEdit
)

// controlsLocked says whether a change that touches the core must wait: a
// core change, a subscription mutation, a mode change, a TUN swap, a rules or
// geodata update, an app update, or the automatic check runs.
func (d *Dashboard) controlsLocked() bool {
	return d.modeBusy || d.subscription.mutations > 0 || d.core.busy || d.tun.busy ||
		d.geodata.busy || d.rules.busy || d.lan.busy || d.port.busy || d.appUpdate.updating || d.autoBusy
}

// busy reports whether an operation runs on the subscription id.
func (s *subscriptionsState) busy(id string) bool {
	_, running := s.busyIDs[id]
	return running
}

// active returns the subscription in use.
func (s *subscriptionsState) active() (model.SubscriptionSummary, bool) {
	for _, item := range s.view.Items {
		if item.ID == s.view.ActiveID {
			return item, true
		}
	}
	return model.SubscriptionSummary{}, false
}

// subOp is one subscription mutation: what runs, under which view epoch it
// reloads, whether the node view is rebuilt, and how the outcome is reported.
type subOp struct {
	a action
	// id spins one card; list spins 全部刷新 instead; "" spins only the dialog that
	// started it.
	id   string
	list bool
	// gen is the runtime epoch taken on the UI thread at the click; its reloads
	// publish only while the epoch is current.
	gen int
	// reloadAfter rebuilds the node view when the op succeeds; reloadOnError
	// also when it fails, for ops that may have left the runtime mid-way.
	reloadAfter   bool
	reloadOnError bool
	run           func(ctx context.Context) error
	// finish reports the outcome on the UI thread, after the reloads.
	finish func(err error)
}

// beginSubOp marks the mutation busy at once on the UI thread and runs it in the
// background; while any mutation runs, every starter refuses clicks, so one runs
// at a time.
func (d *Dashboard) beginSubOp(op subOp) {
	s := &d.subscription
	if d.controlsLocked() {
		return
	}
	op.gen = d.viewGen
	if op.reloadAfter || op.reloadOnError {
		op.gen = d.nextViewGen()
	}
	s.mutations++
	switch {
	case op.list:
		s.listBusy = true
	case op.id != "":
		s.busyIDs[op.id] = struct{}{}
	}
	go d.runSubOp(d.opCtx, d.opWin, op)
}

// runSubOp releases controls after applying and reading back the profile;
// latency probes continue independently.
func (d *Dashboard) runSubOp(ctx context.Context, win Updater, op subOp) {
	err := op.run(ctx)
	d.loadSubscriptions(ctx, win)
	if (err == nil && op.reloadAfter) || (err != nil && op.reloadOnError) {
		d.startupTest(ctx, win, op.gen)
	}
	win.Update(func() {
		s := &d.subscription
		s.mutations--
		switch {
		case op.list:
			s.listBusy = false
		case op.id != "":
			delete(s.busyIDs, op.id)
		}
		op.finish(err)
	})
}

// loadSubscriptions reads the saved collection and the entry in use, keeping the
// bar's picker on the entry the service reports.
func (d *Dashboard) loadSubscriptions(ctx context.Context, win Updater) {
	view, err := d.services.Subscriptions.List(ctx)
	win.Update(func() {
		d.subscription.loaded = true
		if err != nil {
			d.fail(actionReadSubscription, err)
			return
		}
		d.subscription.view = view
		d.subscription.activeChoice = view.ActiveID
	})
}

// loadSource fills the editor with the stored input of id in the background; a
// reply from an older open is dropped by its seq. Read only when editing, never
// blocking the UI.
func (d *Dashboard) loadSource(ctx context.Context, win Updater, id string, seq int) {
	input, err := d.services.Subscriptions.Source(ctx, id)
	win.Update(func() {
		s := &d.subscription
		if !s.editorOpen || s.editor.seq != seq || s.editor.editingID != id {
			return
		}
		if err != nil {
			s.editor.err = err.Error()
			return
		}
		e := &s.editor
		index := slices.Index(service.RuleSources, input.Rules)
		if index < 0 {
			e.err = locales.SubUnknownRules(string(input.Rules))
			return
		}
		sourceIndex := slices.Index(service.ProfileSources, input.Source)
		if sourceIndex < 0 {
			e.err = locales.SubUnknownSource(string(input.Source))
			return
		}
		e.original, e.sourceLoaded, e.rulesIndex, e.sourceIndex = input, true, index, sourceIndex
		// Keep whatever the user typed while the reply was on its way.
		if e.name == "" {
			e.name = input.Name
		}
		if e.url == "" {
			e.url = input.URL
		}
	})
}

// subscriptionURLProblem words what keeps url from being saved, "" when it is a
// plain http or https address.
func subscriptionURLProblem(url string) string {
	if url == "" {
		return locales.SubEnterAddress()
	}
	parsed, err := neturl.Parse(url)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return locales.SubAddressScheme()
	}
	return ""
}

// openEditorDialog resets the editor for id, "" to add, and shows it, returning
// the open's seq, which drops replies from older opens.
func (d *Dashboard) openEditorDialog(intent editorIntent, id string) int {
	s := &d.subscription
	s.editor = subscriptionEditor{
		seq: s.editor.seq + 1, editingID: id, intent: intent,
		// Adding has nothing to wait for; editing fills the source fields from
		// Source in the background.
		sourceLoaded: id == "",
	}
	s.editorOpen = true
	return s.editor.seq
}

// beginEditorSubmit validates the form and begins Add or Edit; the busy mark
// starts here, synchronously, before any work runs.
func (d *Dashboard) beginEditorSubmit() {
	s := &d.subscription
	e := &s.editor
	if e.busy || e.fileBusy || !e.sourceLoaded || d.controlsLocked() {
		return
	}
	source := service.ProfileSources[e.sourceIndex]
	rules := service.RuleSources[e.rulesIndex]
	if e.intent == editorRename {
		// Renaming shows no rule choice: the stored one stays.
		rules = e.original.Rules
		source = e.original.Source
	}
	input := model.SubscriptionInput{Name: strings.TrimSpace(e.name), Source: source, Rules: rules}
	switch source {
	case model.SourceLocal:
		if e.content != "" && strings.TrimSpace(e.content) == "" {
			e.err = locales.SubConfigBlank()
			return
		}
		if e.editingID == "" && e.content == "" {
			e.err = locales.SubConfigMissing()
			return
		}
		if len(e.content) > service.MaxProfileSize {
			e.err = locales.SubConfigTooLarge(service.MaxProfileSize >> 20)
			return
		}
		input.Content = []byte(e.content)
	default:
		if problem := subscriptionURLProblem(strings.TrimSpace(e.url)); problem != "" {
			e.err = problem
			return
		}
		input.URL = strings.TrimSpace(e.url)
	}
	e.err, e.busy = "", true
	if e.editingID == "" {
		// The first subscription added becomes the one in use; any later one
		// leaves the current line untouched.
		first := len(s.view.Items) == 0
		d.beginSubOp(subOp{
			a: actionAddSubscription, reloadAfter: first, reloadOnError: first,
			run:    func(opCtx context.Context) error { return d.services.Subscriptions.Add(opCtx, input) },
			finish: d.finishEditorOp(e.seq, actionAddSubscription),
		})
		return
	}
	id := e.editingID
	// A new address, new content or new rules on the profile in use revalidate
	// and reapply before they commit; a rename alone replaces nothing. A local
	// profile keeps its content when the field is left empty.
	touchesRuntime := id == s.view.ActiveID &&
		(input.URL != e.original.URL || len(input.Content) > 0 || input.Rules != e.original.Rules)
	d.beginSubOp(subOp{
		a: actionEditSubscription, id: id,
		reloadAfter:   touchesRuntime,
		reloadOnError: touchesRuntime,
		run:           func(opCtx context.Context) error { return d.services.Subscriptions.Edit(opCtx, id, input) },
		finish:        d.finishEditorOp(e.seq, actionEditSubscription),
	})
}

// finishEditorOp reports the outcome of Add or Edit: into the form while the
// same open shows, as the window's notice once it does not.
func (d *Dashboard) finishEditorOp(seq int, a action) func(error) {
	return func(err error) {
		s := &d.subscription
		if s.editor.seq != seq {
			// The dialog was closed and reopened; the notice stands in.
			if err != nil {
				d.fail(a, err)
			}
			return
		}
		s.editor.busy = false
		if !s.editorOpen {
			if err != nil {
				d.fail(a, err)
			}
			return
		}
		if err != nil {
			s.editor.err = err.Error()
			return
		}
		s.editorOpen = false
	}
}

// beginDeleteSubmit begins Delete, naming the successor the user chose when
// the entry in use goes and others remain.
func (d *Dashboard) beginDeleteSubmit() {
	s := &d.subscription
	dlg := &s.deleting
	if dlg.busy || d.controlsLocked() {
		return
	}
	wasActive := dlg.id == s.view.ActiveID
	needReplacement := wasActive && len(s.view.Items) > 1
	replacement := ""
	if needReplacement {
		if dlg.replacement == "" {
			dlg.err = locales.SubChooseReplacement()
			return
		}
		replacement = dlg.replacement
	}
	dlg.err, dlg.busy = "", true
	id, seq := dlg.id, dlg.seq
	d.beginSubOp(subOp{
		a: actionDeleteSubscription, id: id,
		reloadAfter:   wasActive,
		reloadOnError: wasActive,
		run: func(opCtx context.Context) error {
			return d.services.Subscriptions.Delete(opCtx, id, replacement)
		},
		finish: func(err error) {
			s := &d.subscription
			if s.deleting.seq != seq {
				if err != nil {
					d.fail(actionDeleteSubscription, err)
				}
				return
			}
			s.deleting.busy = false
			if !s.deleteOpen {
				if err != nil {
					d.fail(actionDeleteSubscription, err)
				}
				return
			}
			if err != nil {
				s.deleting.err = err.Error()
				return
			}
			s.deleteOpen = false
		},
	})
}

// sidebarSubscription heads the sidebar: the subscription in use with a picker,
// what its account has left, and the actions that belong to it.
func (d *Dashboard) sidebarSubscription(c *ui.Context, t *ui.Theme) {
	s := &d.subscription
	if !s.loaded {
		return
	}
	card := ui.Column(c).Key("sidebarSubscription").Gap(gapMedium).Padding(gapMedium).Radius(radiusPanel).
		Background(glassPanel(t)).Border(hairlineWidth, glassMaterialLine(t)).Shadow(0, 3, 12, 0, glassCardShadow(t))
	active, ok := s.active()
	if !ok {
		card.Children(func() { d.noSubscription(c, t) })
		return
	}
	width, _ := c.Size()
	barWidth := sidebarWidthFor(width) - 4*gapMedium
	now := time.Now()
	busy := s.busy(active.ID)
	card.Children(func() {
		ui.Row(c).Wrap().Gap(gapSmall).GapY(gapTight).AlignItems(ui.Center).Children(func() {
			d.activeSelect(c, t)
			// The tag names Hop's own rule set only: a profile that keeps
			// its provider's rules has nothing extra to say here.
			if active.Rules == model.RulesHop {
				chip(c, t, locales.SubRuleSmart(), accentInk(t))
			}
		})
		switch {
		case active.Source != model.SourceRemote:
			mutedText(c, t, service.LocalProfileName()).NoWrap()
		case active.HasUsage:
			subscriptionUsage(c, t, active.Usage, now, barWidth)
		case len(d.view.Plan) > 0:
			ui.Column(c).Gap(gapTight).Children(func() {
				for _, note := range d.view.Plan {
					label, value := splitPlanNote(note)
					planNote(c, t, label, value)
				}
			})
		}
		ui.Row(c).Wrap().Gap(gapLarge).GapY(gapTight).AlignItems(ui.Center).Children(func() {
			if active.Source == model.SourceRemote {
				label := locales.Refresh()
				if busy {
					label = locales.Refreshing()
				}
				textButton(c, t, label, iconRefresh, d.controlsLocked(), busy, func() {
					d.refreshSubscription(active.ID)
				})
			}
			textButton(c, t, locales.SubManage(), iconList, false, false, func() { s.managing = true })
		})
		if active.LastError != "" {
			warningLine(c, t, active.LastError)
		}
	})
}

// noSubscription is what the card shows before any subscription exists:
// adding the first one uses it at once.
func (d *Dashboard) noSubscription(c *ui.Context, t *ui.Theme) {
	s := &d.subscription
	mutedText(c, t, locales.SubNoneInUse())
	button(c, buttonSpec{label: locales.SubAddAndUse(), icon: iconPlus, style: primaryButton(t),
		disabled: d.controlsLocked()}, func() {
		d.openEditor(editorAdd, "")
	})
	textButton(c, t, locales.SubManage(), iconList, false, false, func() { s.managing = true })
}

// subscriptionUsage is the account at a glance: used of total, a thin bar
// barWidth wide, and the days left.
func subscriptionUsage(c *ui.Context, t *ui.Theme, usage model.Usage, now time.Time, barWidth float32) {
	used := usage.Upload + usage.Download
	usedReadout := formatBytes(used)
	value := usedReadout.value + " " + usedReadout.unit
	var ratio float32
	if usage.Total > 0 {
		total := formatBytes(usage.Total)
		value += " / " + total.value + " " + total.unit
		ratio = min(1, float32(used)/float32(usage.Total))
	}
	ui.Column(c).Key("usage").Gap(gapTight).Children(func() {
		ui.Text(c, value).FontSize(fontSecondary).TextColor(t.TextMuted).FontFeatures("tnum").MaxLines(1)
		if usage.Total > 0 {
			stripUsageBar(c, t, ratio, barWidth)
		}
		if !usage.Expire.IsZero() {
			left := usage.Expire.Sub(now)
			color := t.TextMuted
			if left < expiryWarning {
				color = t.Warning
			}
			ui.Text(c, remainingText(left)).FontSize(fontSecondary).TextColor(color).MaxLines(1)
		}
	})
}

func stripUsageBar(c *ui.Context, t *ui.Theme, ratio float32, width float32) {
	const height float32 = 4
	trackFill := t.SurfacePressed
	if onGlass(t) {
		trackFill = glassFill(t, glassTrackAlpha)
	}
	track := ui.Box(c).Width(width).Height(height).Radius(height / 2).
		Background(trackFill)
	fill := track.AnimateTo("used", max(height, ratio*width), ui.Snappy)
	track.Children(func() {
		ui.Box(c).Width(fill).Height(height).Radius(height / 2).Background(usageColor(t, ratio))
	})
}

// activeSelect switches the subscription in use offline: the choice applies from
// what is saved, without downloading anything.
func (d *Dashboard) activeSelect(c *ui.Context, t *ui.Theme) {
	s := &d.subscription
	sel := ui.SelectBase(c, &s.activeChoice)
	b := sel.Trigger
	b.Label(locales.SubCurrent()).Height(switchHeight).Padding(0).Radius(0).Border(0, ui.Transparent).
		Gap(7).Justify(ui.Start).Background(ui.Transparent).
		Cursor(ui.CursorPointer).Disabled(d.controlsLocked())
	ink := t.Text
	if b.Hovered() || sel.Open() {
		ink = accentInk(t)
	}
	color := b.AnimateColor("ink", ink, ui.Smooth)
	b.Children(func() {
		ui.Icon(c, iconGlobe).Width(iconLead).Height(iconLead).TextColor(t.TextMuted)
		ui.Text(c, pickerLabel(s.view.Items, s.activeChoice)).FontSize(fontBody).FontWeight(weightMedium).
			MaxLines(1).TextColor(color).Translate(0, -opticalLift)
		ui.Icon(c, iconChevron).Width(iconInline).Height(iconInline).TextColor(t.TextMuted)
	})
	sel.Popup(func(panel ui.Element) {
		panel.Margin(gapTight, 0, 0, 0).Padding(pickerPopupInset).Radius(radiusControl).
			Background(overlaySurface(t)).Border(1, t.Border).Shadow(0, 6, 20, 0, popupShadow)
		panel.Children(func() {
			for _, item := range s.view.Items {
				option := sel.Item(item.ID)
				pickerRow(c, t, option, pickerLabel(s.view.Items, item.ID), item.ID == s.activeChoice)
			}
		})
	})
	if b.Changed() && s.activeChoice != s.view.ActiveID {
		d.useSubscription(s.activeChoice)
	}
}

// pickerRow uses quiet selection and hover fills, with vertically centered text
// and a selection mark.
func pickerRow(c *ui.Context, t *ui.Theme, option ui.Element, label string, chosen bool) {
	option.Height(pickerItemHeight).Padding(0, pickerPadX).Radius(radiusChip).Gap(gapSmall).
		AlignItems(ui.Center).TextColor(t.Text).Cursor(ui.CursorPointer)
	switch {
	case chosen:
		option.Background(t.Surface.Mix(t.Accent, softFillAlpha))
	case option.Highlighted():
		option.Background(t.SurfaceHover)
	}
	option.Children(func() {
		text := ui.Text(c, label).FontSize(fontBody).FontWeight(weightMedium).MaxLines(1).
			Grow(1).Basis(0).Translate(0, -opticalLift)
		if chosen {
			text.FontWeight(weightSemibold)
		}
		ui.Box(c).Width(iconInline).Height(iconInline).Shrink(0).Center().Children(func() {
			if chosen {
				ui.Icon(c, iconCheck).Width(iconInline).Height(iconInline).TextColor(t.Accent)
			}
		})
	})
}

// managePage is the subscription management subpage of the nodes page: the
// saved collection as spaced, frosted cards.
func (d *Dashboard) managePage(c *ui.Context, t *ui.Theme) {
	s := &d.subscription
	if !s.loaded {
		return
	}
	gutter := pageGutter(c)
	ui.Column(c).Grow(1).Basis(0).MinHeight(0).Padding(pagePadding, 0, 0, 0).Children(func() {
		toolbar(c, func() {
			button(c, buttonSpec{label: locales.SubBackToNodes(), icon: iconBack, style: mainSoftButton(t)}, func() { s.managing = false })
			ui.Box(c).Grow(1)
			label := locales.SubRefreshAll()
			if s.listBusy {
				label = locales.Refreshing()
			}
			refreshable := slices.ContainsFunc(s.view.Items, func(item model.SubscriptionSummary) bool {
				return item.Source == model.SourceRemote
			})
			button(c, buttonSpec{label: label, icon: iconRefresh, style: mainSoftButton(t),
				disabled: s.mutations > 0 || !refreshable || d.modeBusy, busy: s.listBusy}, d.refreshAllSubscriptions)
			button(c, buttonSpec{label: locales.SubAdd(), icon: iconPlus, style: primaryButton(t),
				disabled: d.controlsLocked()}, func() {
				d.openEditor(editorAdd, "")
			})
		})
		ui.Row(c).Gap(gapSmall).AlignItems(ui.End).Padding(0, gutter, gapTight, gutter).Children(func() {
			ui.Text(c, locales.Subscriptions()).FontSize(fontHeadline).FontWeight(weightSemibold)
			mutedText(c, t, locales.SubCount(len(s.view.Items))).FontFeatures("tnum")
		})
		mutedText(c, t, locales.SubPageNote()).
			Padding(0, gutter, gapLarge, gutter)
		ui.Scroll(c).Grow(1).Basis(0).MinHeight(0).Children(func() {
			ui.Column(c).Gap(gapMedium).Padding(scrollTrim, gutter, scrollEnd, gutter).Children(func() {
				if len(s.view.Items) == 0 {
					d.manageEmpty(c, t)
					return
				}
				for _, item := range s.view.Items {
					d.subscriptionCard(c, t, item)
				}
			})
		})
	})
}

// manageEmpty says what the page wants first: one subscription, used at once.
func (d *Dashboard) manageEmpty(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Key("空").Grow(1).Center().Gap(gapMedium).Padding(emptyPadY, pagePadding).Children(func() {
		ui.Icon(c, iconLink).Width(iconEmpty).Height(iconEmpty).TextColor(t.TextMuted)
		ui.Text(c, locales.SubEmptyTitle()).FontSize(fontEmphasis).FontWeight(weightSemibold)
		mutedText(c, t, locales.SubEmptyNote())
		button(c, buttonSpec{label: locales.SubAddAndUse(), icon: iconPlus, style: primaryButton(t),
			disabled: d.controlsLocked()}, func() {
			d.openEditor(editorAdd, "")
		})
	})
}

// subscriptionCard is one saved profile: where it comes from, what the account
// holds, when it last refreshed, and what to do with it. A local profile has
// no account, refresh or update time. The profile in use keeps a quiet
// accent edge and its explicit status badge over the same glass face.
func (d *Dashboard) subscriptionCard(c *ui.Context, t *ui.Theme, item model.SubscriptionSummary) {
	s := &d.subscription
	isActive := item.ID == s.view.ActiveID
	locked := d.controlsLocked()
	card := panelCard(c, t).Key("sub:"+item.ID).Gap(gapMedium).Padding(gapLarge).
		Shadow(0, 3, 12, 0, glassCardShadow(t))
	if isActive {
		card.Border(hairlineWidth, glassSelectedBorder(t))
	}
	card.Children(func() {
		ui.Row(c).Gap(gapMedium).AlignItems(ui.Center).Children(func() {
			cardSource(c, t, isActive, item.Source)
			ui.Column(c).Grow(1).Basis(0).Gap(gapLine).Children(func() {
				ui.Row(c).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
					ui.Text(c, item.Name).FontSize(fontEmphasis).FontWeight(weightSemibold).MaxLines(1).Grow(1).Basis(0)
					if isActive {
						activeBadge(c, t)
					}
					ruleChip(c, t, item.Source, item.Rules)
				})
				captionText(c, t, subscriptionOrigin(item)).MaxLines(1)
			})
			if !isActive {
				button(c, buttonSpec{label: locales.Use(), style: mainSoftButton(t), disabled: locked}, func() {
					d.useSubscription(item.ID)
				})
			}
			if item.Source == model.SourceRemote {
				busy := s.busy(item.ID)
				label := locales.Refresh()
				if busy {
					label = locales.Refreshing()
				}
				button(c, buttonSpec{label: label, icon: iconRefresh, style: mainSoftButton(t), disabled: locked, busy: busy}, func() {
					d.refreshSubscription(item.ID)
				})
			}
			d.cardMenu(c, t, item, locked)
		})
		d.cardMetrics(c, t, item)
		if item.LastError != "" {
			warningLine(c, t, item.LastError)
		}
	})
}

// subscriptionOrigin names where a profile comes from: the link's host, or the
// short local label for one stored from a file or paste.
func subscriptionOrigin(item model.SubscriptionSummary) string {
	if item.Source == model.SourceLocal {
		return service.LocalProfileName()
	}
	return item.Host
}

// cardSource is the source tile: a globe for a link, a file for a local
// config, on a soft square in the accent when the profile is the one in use.
func cardSource(c *ui.Context, t *ui.Theme, isActive bool, source model.SubscriptionSource) {
	tile := ui.Box(c).Key("来源").Width(cardIconTile).Height(cardIconTile).Radius(radiusControl).Center()
	ink, wash := t.TextMuted, t.Text.Alpha(0.08)
	if isActive {
		ink, wash = t.Accent, t.Accent.Alpha(0.09)
	}
	icon := iconGlobe
	if source == model.SourceLocal {
		icon = iconFile
	}
	tile.Background(wash)
	tile.Children(func() {
		ui.Icon(c, icon).Width(cardIconGlyph).Height(cardIconGlyph).TextColor(ink)
	})
}

// activeBadge marks the subscription in use.
func activeBadge(c *ui.Context, t *ui.Theme) {
	ui.Row(c).Key("使用中").Gap(gapLine).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, iconCheck).Width(iconInline).Height(iconInline).TextColor(t.Accent)
		ui.Text(c, locales.BadgeInUse()).FontSize(fontCaption).FontWeight(weightMedium).TextColor(accentInk(t)).NoWrap()
	})
}

// cardMenu holds the actions that fit one card poorly: rename, edit the link
// or local content, delete.
func (d *Dashboard) cardMenu(c *ui.Context, t *ui.Theme, item model.SubscriptionSummary, disabled bool) {
	id := item.ID
	editLabel := locales.SubEdit()
	if item.Source == model.SourceLocal {
		editLabel = locales.SubEditConfig()
	}
	iconMenuButton(c, locales.SubMoreActions(item.Name), iconMore, mainSoftButton(t), disabled, func(m *ui.Menu) {
		if m.Item(locales.Rename()).Disabled(disabled).Chosen() {
			d.openEditor(editorRename, id)
		}
		if m.Item(editLabel).Disabled(disabled).Chosen() {
			d.openEditor(editorEdit, id)
		}
		m.Separator()
		if m.Item(locales.SubDelete()).Disabled(disabled).Chosen() {
			d.openDelete(id)
		}
	})
}

// cardMetrics shows what the provider reports: the account's use, its term, and
// when the subscription last refreshed successfully. Metrics the provider omit
// are left out rather than shown empty. A local profile has none of them.
func (d *Dashboard) cardMetrics(c *ui.Context, t *ui.Theme, item model.SubscriptionSummary) {
	if item.Source == model.SourceLocal {
		return
	}
	hasExpiry := item.HasUsage && !item.Usage.Expire.IsZero()
	ui.Column(c).Key("指标").Gap(gapMedium).Children(func() {
		horizontalHairline(c, t)
		ui.Row(c).Gap(gapSection).GapY(gapMedium).Wrap().AlignItems(ui.Start).Children(func() {
			if item.HasUsage {
				usageMetric(c, t, item.Usage)
			}
			if hasExpiry {
				expiryMetric(c, t, item.Usage.Expire)
			}
			updatedMetric(c, t, item.UpdatedAt)
		})
	})
}

func usageMetric(c *ui.Context, t *ui.Theme, usage model.Usage) {
	used := usage.Upload + usage.Download
	ui.Column(c).Key("已用流量").Grow(1.4).Basis(0).MinWidth(cardUsageBarWidth).Gap(gapTight).Children(func() {
		metricLabel(c, t, locales.SubMetricUsed())
		ui.Row(c).Gap(gapTight).AlignItems(ui.End).Children(func() {
			usedReadout := formatBytes(used)
			ui.Text(c, usedReadout.value+" "+usedReadout.unit).FontSize(fontSecondary).FontWeight(weightSemibold).
				FontFeatures("tnum").NoWrap()
			if usage.Total > 0 {
				totalReadout := formatBytes(usage.Total)
				captionText(c, t, "/ "+totalReadout.value+" "+totalReadout.unit).NoWrap()
				ratio := min(1, float32(used)/float32(usage.Total))
				ui.Text(c, fmt.Sprintf("%.0f%%", ratio*100)).FontSize(fontCaption).TextColor(usageInk(t, ratio)).
					FontFeatures("tnum").NoWrap()
			}
		})
		if usage.Total > 0 {
			cardUsageBar(c, t, min(1, float32(used)/float32(usage.Total)))
		}
	})
}

func expiryMetric(c *ui.Context, t *ui.Theme, expire time.Time) {
	left := time.Until(expire)
	color := t.TextMuted
	if left < expiryWarning {
		color = t.Warning
	}
	ui.Column(c).Key("到期时间").Grow(1).Basis(0).MinWidth(cardExpiryMinWidth).Gap(gapTight).Children(func() {
		metricLabel(c, t, locales.SubMetricExpires())
		ui.Row(c).Gap(gapTight).AlignItems(ui.End).Children(func() {
			ui.Text(c, expire.Format(usageDateStyle)).FontSize(fontSecondary).FontWeight(weightSemibold).
				FontFeatures("tnum").NoWrap()
			ui.Text(c, remainingText(left)).FontSize(fontCaption).TextColor(color).NoWrap()
		})
	})
}

func updatedMetric(c *ui.Context, t *ui.Theme, at time.Time) {
	ui.Column(c).Key("最近成功更新").Grow(0.8).Basis(0).MinWidth(cardUpdateMinWidth).Gap(gapTight).Children(func() {
		metricLabel(c, t, locales.SubMetricUpdated())
		ui.Text(c, updatedText(at, time.Now())).FontSize(fontSecondary).FontWeight(weightSemibold).NoWrap()
	})
}

// metricLabel heads one metric of a card.
func metricLabel(c *ui.Context, t *ui.Theme, text string) {
	captionText(c, t, text)
}

// cardUsageBar fills to ratio, springing to a new value after a refresh.
func cardUsageBar(c *ui.Context, t *ui.Theme, ratio float32) {
	progressBar(c, t, ratio, usageColor(t, ratio))
}

// progressBar fills to ratio in fill, springing to each new value.
func progressBar(c *ui.Context, t *ui.Theme, ratio float32, fill ui.Color) {
	track := ui.Box(c).Width(cardUsageBarWidth).Height(cardUsageBarHeight).Radius(cardUsageBarHeight / 2).
		Background(t.Text.Alpha(softFillAlpha))
	width := track.AnimateTo("used", max(cardUsageBarHeight, ratio*cardUsageBarWidth), ui.Snappy)
	track.Children(func() {
		ui.Box(c).Width(width).Height(cardUsageBarHeight).Radius(cardUsageBarHeight / 2).Background(fill)
	})
}

// warningLine is a row's last refresh failure, kept beside the subscription's
// last good download.
func warningLine(c *ui.Context, t *ui.Theme, message string) {
	ui.Row(c).Key("刷新失败").Gap(gapTight).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, iconWarn).Width(iconInline).Height(iconInline).TextColor(t.Warning)
		ui.Text(c, message).FontSize(fontCaption).TextColor(t.Warning)
	})
}

// editorDialog is the add/edit form over a dimmed window.
func (d *Dashboard) editorDialog(c *ui.Context, t *ui.Theme) {
	s := &d.subscription
	animatedDialog(c, "subscription", &s.editorOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(dialogScrim)
		panel.Width(dialogWidth).Padding(pagePadding).Gap(gapLarge).Radius(radiusPanel).
			Background(overlaySurface(t)).Border(1, t.Border).Shadow(0, 10, 30, 0, dialogShadow)
		panel.Children(func() { d.editorForm(c, t) })
	})
}

// editorForm is the add/edit dialog's body: an optional name and the source
// fields — a subscription address, or a local file or pasted config — filled
// from what is stored when editing.
func (d *Dashboard) editorForm(c *ui.Context, t *ui.Theme) {
	s := &d.subscription
	e := &s.editor
	source := service.ProfileSources[e.sourceIndex]
	local := source == model.SourceLocal
	first := len(s.view.Items) == 0
	title, description, submit := locales.SubAdd(), locales.SubAddHint(), locales.Add()
	switch e.intent {
	case editorAdd:
		if first {
			description, submit = locales.SubAddFirstHint(), locales.SubAddAndUse()
		}
	case editorRename:
		title, description, submit = locales.Rename(), locales.SubRenameHint(), locales.SaveChanges()
	case editorEdit:
		if local {
			title, description, submit = locales.SubEditConfig(), locales.SubEditConfigHint(), locales.SaveChanges()
		} else {
			title, description, submit = locales.SubEdit(), locales.SubEditHint(), locales.SaveChanges()
		}
	}
	hint := locales.SubNameHint()
	localHint := locales.SubPasteHint()
	urlPlaceholder := "https://provider.example.com/subscribe"
	if !e.sourceLoaded {
		hint = locales.SubReadingSaved()
		urlPlaceholder = locales.SubReadingDetails()
	}
	if e.editingID != "" && local {
		localHint = locales.SubEditKeepHint()
	}
	ui.Column(c).Gap(gapSection).Children(func() {
		ui.Column(c).Gap(gapSmall).Children(func() {
			ui.Row(c).Gap(gapMedium).AlignItems(ui.Center).Children(func() {
				ui.Text(c, title).FontSize(fontHeadline).FontWeight(weightSemibold).Grow(1)
				iconButton(c, locales.Close(), iconClose, softButton(t), func() { s.editorOpen = false })
			})
			mutedText(c, t, description)
		})
		ui.Column(c).Gap(gapSmall).Children(func() {
			fieldLabel(c, t, locales.SubFieldName(), locales.Optional())
			ui.TextInput(c, &e.name).Label(locales.SubFieldName()).Placeholder(locales.SubNamePlaceholder()).
				Background(dialogField(t)).Disabled(e.busy || e.fileBusy)
			if e.intent != editorRename {
				fieldLabel(c, t, locales.SubFieldSource(), "")
				ui.Box(c).Disabled(e.busy || e.fileBusy || e.editingID != "" || !e.sourceLoaded).Children(func() {
					slidingSwitch(c, "source", &e.sourceIndex, sourceLabels(), dialogSwitchStyle(t, sourceSegmentWidth), func(i int) {
						e.err = ""
						e.rulesIndex = slices.Index(service.RuleSources, defaultRules(service.ProfileSources[i]))
					})
				})
				if e.editingID != "" {
					captionText(c, t, locales.SubSourceLocked())
				}
				if local {
					nameHint := locales.SubLocalNameHint(service.LocalProfileName())
					ui.Row(c).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
						button(c, buttonSpec{label: locales.SubChooseFile(), icon: iconFile, style: mainSoftButton(t),
							disabled: e.busy || e.fileBusy || !e.sourceLoaded, busy: e.fileBusy}, func() {
							d.chooseLocalFile(e.seq)
						})
						if e.fileName != "" {
							mutedText(c, t, locales.SubFileRead(e.fileName)).MaxLines(1)
						} else {
							mutedText(c, t, locales.SubFileTypes())
						}
					})
					fieldLabel(c, t, locales.SubFieldContent(), locales.SubContentLimit(service.MaxProfileSize>>20))
					ui.TextArea(c, &e.content).Label(locales.SubFieldContent()).
						Placeholder(locales.SubContentPlaceholder()).Height(localEditorHeight).
						Background(dialogField(t)).Disabled(e.busy || e.fileBusy || !e.sourceLoaded)
					captionText(c, t, localHint)
					captionText(c, t, nameHint)
				} else {
					fieldLabel(c, t, locales.SubFieldAddress(), "")
					ui.TextInput(c, &e.url).Label(locales.SubFieldAddress()).Placeholder(urlPlaceholder).
						Background(dialogField(t)).
						Disabled(e.busy || e.fileBusy || !e.sourceLoaded).OnSubmit(d.submitEditor)
					captionText(c, t, hint)
				}
				fieldLabel(c, t, locales.SubFieldRules(), "")
				labels, hints := ruleTexts(service.ProfileSources[e.sourceIndex])
				ui.Box(c).Disabled(e.busy || e.fileBusy || !e.sourceLoaded).Children(func() {
					slidingSwitch(c, "rules", &e.rulesIndex, labels, dialogSwitchStyle(t, ruleSegmentWidth), func(int) {})
				})
				captionText(c, t, hints[e.rulesIndex])
			}
			if e.busy {
				captionText(c, t, locales.SubApplying())
			}
			if e.err != "" {
				ui.Text(c, e.err).FontSize(fontSecondary).TextColor(t.Danger)
			}
		})
		submitDisabled := e.busy || e.fileBusy || !e.sourceLoaded
		if local {
			submitDisabled = submitDisabled || (e.editingID == "" && strings.TrimSpace(e.content) == "")
		} else {
			submitDisabled = submitDisabled || strings.TrimSpace(e.url) == ""
		}
		ui.Row(c).Gap(gapSmall).Justify(ui.End).Children(func() {
			button(c, buttonSpec{label: locales.Cancel(), style: softButton(t), disabled: e.busy}, func() { s.editorOpen = false })
			button(c, buttonSpec{label: submit, style: primaryButton(t), busy: e.busy,
				disabled: submitDisabled}, d.submitEditor)
		})
	})
}

// chooseLocalFile opens the system file picker and reads the chosen config
// into the editor. Only the content and the file's base name are kept: the
// path is shown while the dialog is open and never stored.
func (d *Dashboard) chooseLocalFile(seq int) {
	s := &d.subscription
	e := &s.editor
	if e.fileBusy || e.busy || !e.sourceLoaded {
		return
	}
	e.fileBusy, e.err = true, ""
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Title:       locales.SubFileDialogTitle(),
			ButtonLabel: locales.Choose(),
			Filters: []mygo.FileFilter{
				{Name: locales.SubFileFilterConfig(), Extensions: []string{"yaml", "yml", "txt", "conf"}},
				{Name: locales.SubFileFilterAll(), Extensions: []string{"*"}},
			},
		})
		var content []byte
		var name, problem string
		switch {
		case err != nil:
			problem = locales.SubFileDialogFailed(err.Error())
		case len(paths) > 0:
			name = filepath.Base(paths[0])
			content, err = readLocalConfig(paths[0])
			if err != nil {
				problem = err.Error()
			}
		}
		d.opWin.Update(func() {
			s := &d.subscription
			e := &s.editor
			if e.seq != seq || !s.editorOpen {
				return
			}
			e.fileBusy = false
			if problem != "" {
				e.err = problem
				return
			}
			if name == "" {
				// The picker was canceled.
				return
			}
			e.content, e.fileName = string(content), name
			if strings.TrimSpace(e.name) == "" {
				e.name = strings.TrimSuffix(name, filepath.Ext(name))
			}
		})
	}()
}

// readLocalConfig reads a config the user chose, held to the same size limit
// as a download. Errors name the file by its base name only.
func readLocalConfig(path string) (content []byte, err error) {
	name := filepath.Base(path)
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%s%w", locales.FileOpenFailed(name), fileCause(err))
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("%s%w", locales.FileCloseFailed(name), fileCause(closeErr)))
		}
	}()
	content, err = io.ReadAll(io.LimitReader(file, service.MaxProfileSize+1))
	if err != nil {
		return nil, fmt.Errorf("%s%w", locales.FileReadFailed(name), fileCause(err))
	}
	if len(content) > service.MaxProfileSize {
		return nil, errors.New(locales.FileTooLarge(name, service.MaxProfileSize>>20))
	}
	return content, nil
}

// fileCause drops the path a file error carries, so a message names the file
// by its base name only.
func fileCause(err error) error {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err
	}
	return err
}

// deleteDialog confirms deleting a subscription, asking for its successor when
// the one in use goes and others remain.
func (d *Dashboard) deleteDialog(c *ui.Context, t *ui.Theme) {
	s := &d.subscription
	animatedDialog(c, "delete", &s.deleteOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(dialogScrim)
		panel.Width(dialogWidth).Padding(pagePadding).Gap(gapLarge).Radius(radiusPanel).
			Background(overlaySurface(t)).Border(1, t.Border).Shadow(0, 10, 30, 0, dialogShadow)
		panel.Children(func() { d.deleteForm(c, t) })
	})
}

func (d *Dashboard) deleteForm(c *ui.Context, t *ui.Theme) {
	s := &d.subscription
	dlg := &s.deleting
	others := make([]model.SubscriptionSummary, 0, len(s.view.Items))
	for _, item := range s.view.Items {
		if item.ID != dlg.id {
			others = append(others, item)
		}
	}
	wasActive := dlg.id == s.view.ActiveID
	lastOne := wasActive && len(others) == 0
	needReplacement := wasActive && !lastOne
	description := locales.SubDeleteConfirm(dlg.name)
	submit := locales.Delete()
	switch {
	case needReplacement:
		description = locales.SubDeleteActive(dlg.name)
		submit = locales.SubSwitchAndDelete()
	case lastOne:
		description = locales.SubDeleteLast(dlg.name)
		submit = locales.SubStopAndDelete()
	}
	ui.Column(c).Gap(gapSection).Children(func() {
		ui.Column(c).Gap(gapSmall).Children(func() {
			ui.Row(c).Gap(gapMedium).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconTrash).Width(iconTitle).Height(iconTitle).TextColor(t.Danger)
				ui.Text(c, locales.SubDelete()).FontSize(fontHeadline).FontWeight(weightSemibold).Grow(1)
				iconButton(c, locales.Close(), iconClose, softButton(t), func() { s.deleteOpen = false })
			})
			mutedText(c, t, description)
		})
		if needReplacement {
			ui.Column(c).Key("替代").Gap(gapSmall).Children(func() {
				fieldLabel(c, t, locales.SubSwitchTo(), "")
				subscriptionPicker(c, t, others, &dlg.replacement)
				captionText(c, t, locales.SubDeleteAfterSwitch())
			})
		}
		if dlg.err != "" {
			ui.Text(c, dlg.err).FontSize(fontSecondary).TextColor(t.Danger)
		}
		ui.Row(c).Gap(gapSmall).Justify(ui.End).Children(func() {
			button(c, buttonSpec{label: locales.Cancel(), style: softButton(t), disabled: dlg.busy}, func() { s.deleteOpen = false })
			button(c, buttonSpec{label: submit, style: dangerButton(t), busy: dlg.busy,
				disabled: dlg.busy || (needReplacement && dlg.replacement == "")}, d.submitDelete)
		})
	})
}

// sourceLabels name service.ProfileSources, index for index.
func sourceLabels() []string { return []string{locales.SubSourceLink(), locales.SubSourceLocal()} }

// ruleTexts are the labels of service.RuleSources for a profile from source,
// index for index, and what each does, in the same order. A local config has
// no provider: most are a self-hosted server's node links, with no rules of
// their own.
func ruleTexts(source model.SubscriptionSource) ([]string, []string) {
	if source == model.SourceLocal {
		return []string{locales.SubRuleOwn(), locales.SubRuleSmart()},
			[]string{locales.SubRuleHintLocalOwn(), locales.SubRuleHintLocalSmart()}
	}
	return []string{locales.SubRuleProvider(), locales.SubRuleSmart()},
		[]string{locales.SubRuleHintProvider(), locales.SubRuleHintSmart()}
}

// ruleChip says whose rules a subscription runs with: the provider's in the
// muted colour, Hop's template in the accent.
func ruleChip(c *ui.Context, t *ui.Theme, source model.SubscriptionSource, rules model.RuleSource) {
	switch rules {
	case model.RulesHop:
		chip(c, t, locales.SubRuleSmart(), accentInk(t))
	default:
		labels, _ := ruleTexts(source)
		chip(c, t, labels[0], t.TextMuted)
	}
}

// defaultRules is the rule source a new profile from source starts with: a
// subscription's own rules, and Hop's for a local config, which is most
// often a self-hosted server's node links.
func defaultRules(source model.SubscriptionSource) model.RuleSource {
	if source == model.SourceLocal {
		return model.RulesHop
	}
	return model.RulesProvider
}

// ruleSegmentWidth fits the longest rule label.
const ruleSegmentWidth float32 = 104

// fieldLabel names a form field, its optional note in the muted colour.
func fieldLabel(c *ui.Context, t *ui.Theme, text string, note string) {
	ui.Row(c).Key(text).Gap(gapTight).AlignItems(ui.End).Children(func() {
		ui.Text(c, text).FontSize(fontSecondary).FontWeight(weightSemibold)
		if note != "" {
			captionText(c, t, note)
		}
	})
}

// subscriptionPicker chooses among items into chosen; the delete dialog uses it
// to name the successor of the subscription it removes.
func subscriptionPicker(c *ui.Context, t *ui.Theme, items []model.SubscriptionSummary, chosen *string) {
	sel := ui.SelectBase(c, chosen)
	b := sel.Trigger
	b.Label(locales.SubSelect()).Height(switchHeight).Grow(1).Padding(0, gapSmall, 0, pickerPadX).
		Radius(radiusControl).Background(dialogField(t)).Border(1, t.Border).Gap(gapSmall).
		Justify(ui.SpaceBetween).Cursor(ui.CursorPointer)
	b.Children(func() {
		if label := pickerLabel(items, *chosen); label != "" {
			ui.Text(c, label).FontSize(fontBody).FontWeight(weightMedium).MaxLines(1).
				Grow(1).Basis(0).Translate(0, -opticalLift)
		} else {
			ui.Text(c, locales.SubChoosePrompt()).FontSize(fontBody).TextColor(t.TextMuted).
				Grow(1).Basis(0).NoWrap().Translate(0, -opticalLift)
		}
		ui.Icon(c, iconChevron).Width(iconInline).Height(iconInline).TextColor(t.TextMuted)
	})
	sel.Popup(func(panel ui.Element) {
		panel.Margin(gapTight, 0, 0, 0).Padding(pickerPopupInset).Radius(radiusControl).
			Background(overlaySurface(t)).Border(1, t.Border).Shadow(0, 6, 20, 0, popupShadow)
		panel.Children(func() {
			for _, item := range items {
				option := sel.Item(item.ID)
				pickerRow(c, t, option, pickerLabel(items, item.ID), item.ID == *chosen)
			}
		})
	})
}

// pickerLabel names the subscription id as the pickers do; a name shared by
// more than one subscription carries its host to tell them apart.
func pickerLabel(items []model.SubscriptionSummary, id string) string {
	for _, item := range items {
		if item.ID == id {
			for _, other := range items {
				if other.ID != item.ID && other.Name == item.Name && item.Host != "" {
					return item.Name + "（" + item.Host + "）"
				}
			}
			return item.Name
		}
	}
	return ""
}

// usageInk is the usage figure as text: its tier's colour, deepened for
// contrast, or the accent ink while usage is unremarkable.
func usageInk(t *ui.Theme, ratio float32) ui.Color {
	switch {
	case ratio >= usageCritical:
		return textOn(t, t.Danger)
	case ratio >= usageWarning:
		return textOn(t, t.Warning)
	default:
		return textOn(t, accentInk(t))
	}
}

func usageColor(t *ui.Theme, ratio float32) ui.Color {
	switch {
	case ratio >= usageCritical:
		return textOn(t, t.Danger)
	case ratio >= usageWarning:
		return textOn(t, t.Warning)
	default:
		return t.Accent
	}
}

// updatedText says how long ago the subscription was downloaded.
func updatedText(at time.Time, now time.Time) string {
	if at.IsZero() {
		return locales.SubUpdatedUnknown()
	}
	ago := now.Sub(at)
	switch {
	case ago < time.Minute:
		return locales.SubUpdatedNow()
	case ago < time.Hour:
		return locales.SubUpdatedMinutes(int(ago.Minutes()))
	case ago < dayLength:
		return locales.SubUpdatedHours(int(ago.Hours()))
	default:
		return locales.SubUpdatedDays(int(ago / dayLength))
	}
}

// remainingText says how long the account has left.
func remainingText(left time.Duration) string {
	switch {
	case left <= 0:
		return locales.SubExpired()
	case left < dayLength:
		return locales.SubExpiresToday()
	default:
		return locales.SubDaysLeft(int(left / dayLength))
	}
}

func planNote(c *ui.Context, t *ui.Theme, label string, value string) {
	ui.Row(c).Key(label).Gap(5).AlignItems(ui.End).Children(func() {
		if label != "" {
			mutedText(c, t, label).NoWrap()
		}
		ui.Text(c, value).FontSize(fontSecondary).FontWeight(weightSemibold).FontFeatures("tnum").NoWrap()
	})
}

// splitPlanNote splits "剩余流量：1023.86 GB" into its label and value; a note
// without a colon is all value.
func splitPlanNote(note string) (string, string) {
	for _, sep := range []string{"：", ":"} {
		if label, value, found := strings.Cut(note, sep); found {
			return strings.TrimSpace(label), strings.TrimSpace(value)
		}
	}
	return "", note
}
