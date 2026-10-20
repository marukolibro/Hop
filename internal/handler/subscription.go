package handler

import (
	"context"
	"fmt"
	neturl "net/url"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
)

const (
	usageBarWidth  float32 = 160
	usageBarHeight float32 = 6
	// Usage turns amber and then red as the account nears its limit, and
	// the expiry date turns amber in its last week.
	usageWarning   = 0.8
	usageCritical  = 0.95
	expiryWarning  = 7 * 24 * time.Hour
	dayLength      = 24 * time.Hour
	usageDateStyle = "2006-01-02"

	// activeCardTint is Accent mixed into Surface.
	// cardDividerAlpha softens the metrics divider.
	activeCardTint   float32 = 0.05
	cardDividerAlpha float32 = 0.65
)

// editorIntent distinguishes adding, renaming and editing a URL.
type editorIntent int

const (
	editorAdd editorIntent = iota
	editorRename
	editorEditURL
)

// controlsLocked says whether a change that touches the core must wait: a
// core change, a subscription mutation, a mode change or a TUN swap runs.
func (d *Dashboard) controlsLocked() bool {
	return d.modeBusy || d.subscription.mutations > 0 || d.core.busy || d.tun.busy
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
		e.original, e.sourceLoaded = input, true
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
		return "请输入订阅地址。"
	}
	parsed, err := neturl.Parse(url)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "订阅地址需要以 http:// 或 https:// 开头。"
	}
	return ""
}

// openEditorDialog resets the editor for id, "" to add, and shows it, returning
// the open's seq, which drops replies from older opens.
func (d *Dashboard) openEditorDialog(intent editorIntent, id string) int {
	s := &d.subscription
	s.editor = subscriptionEditor{
		seq: s.editor.seq + 1, editingID: id, intent: intent,
		// Adding has nothing to wait for; editing fills the address from Source
		// in the background.
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
	if e.busy || !e.sourceLoaded || d.controlsLocked() {
		return
	}
	if problem := subscriptionURLProblem(strings.TrimSpace(e.url)); problem != "" {
		e.err = problem
		return
	}
	input := model.SubscriptionInput{Name: strings.TrimSpace(e.name), URL: strings.TrimSpace(e.url)}
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
	// A new address on the entry in use revalidates and reapplies before it
	// commits; a rename alone downloads and reloads nothing.
	touchesRuntime := id == s.view.ActiveID && input.URL != e.original.URL
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
			dlg.err = "请选择替代订阅。"
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

// subscriptionBar sits under the route: the subscription in use with a picker
// that switches among the saved ones offline, a refresh of it, and the door to
// managing every subscription.
func (d *Dashboard) subscriptionBar(c *ui.Context, t *ui.Theme) {
	s := &d.subscription
	if !s.loaded {
		return
	}
	active, ok := s.active()
	if !ok {
		d.noSubscriptionBar(c, t)
		return
	}
	now := time.Now()
	busy := s.busy(active.ID)
	ui.Column(c).Gap(gapMedium).Padding(0, pagePadding, gapLarge, pagePadding).Children(func() {
		ui.Column(c).Gap(gapSmall).Padding(panelPadY, panelPadX).Radius(radiusPanel).
			Background(t.Surface).Border(1, t.Border).Children(func() {
			ui.Row(c).Gap(gapMedium).AlignItems(ui.Center).Children(func() {
				mutedText(c, t, "当前订阅").NoWrap()
				d.activeSelect(c, t)
				ui.Box(c).Grow(1)
				mutedText(c, t, updatedText(active.UpdatedAt, now)).NoWrap()
				label := "刷新订阅"
				if busy {
					label = "刷新中"
				}
				button(c, buttonSpec{label: label, icon: iconRefresh, style: softButton(t),
					disabled: d.controlsLocked(), busy: busy}, func() {
					d.refreshSubscription(active.ID)
				})
				button(c, buttonSpec{label: "管理订阅", icon: iconList, style: softButton(t)}, func() { s.managing = true })
			})
			switch {
			case active.HasUsage:
				usageLine(c, t, active.Usage, now)
			case len(d.view.Plan) > 0:
				// Without the usage header, show what the provider wrote into
				// the proxy list instead.
				ui.Row(c).Gap(gapLarge).AlignItems(ui.Center).Children(func() {
					for _, note := range d.view.Plan {
						label, value := splitPlanNote(note)
						planNote(c, t, label, value)
					}
				})
			}
			if active.LastError != "" {
				warningLine(c, t, active.LastError)
			}
		})
	})
}

// noSubscriptionBar is what the nodes page shows before any subscription
// exists: adding the first one uses it at once.
func (d *Dashboard) noSubscriptionBar(c *ui.Context, t *ui.Theme) {
	s := &d.subscription
	ui.Row(c).Gap(gapMedium).AlignItems(ui.Center).Padding(panelPadY, panelPadX).
		Margin(0, pagePadding, gapLarge, pagePadding).
		Radius(radiusPanel).Background(t.Surface).Border(1, t.Border).Children(func() {
		ui.Icon(c, iconLink).Width(iconLead).Height(iconLead).TextColor(t.TextMuted)
		mutedText(c, t, "还没有使用中的订阅").Grow(1)
		button(c, buttonSpec{label: "添加并使用", icon: iconPlus, style: primaryButton(t),
			disabled: d.controlsLocked()}, func() {
			d.openEditor(editorAdd, "")
		})
		button(c, buttonSpec{label: "管理订阅", style: softButton(t)}, func() { s.managing = true })
	})
}

// activeSelect switches the subscription in use offline: the choice applies from
// what is saved, without downloading anything.
func (d *Dashboard) activeSelect(c *ui.Context, t *ui.Theme) {
	s := &d.subscription
	sel := ui.SelectBase(c, &s.activeChoice)
	b := sel.Trigger
	b.Label("当前订阅").Height(switchHeight).MinWidth(selectMinWidth).
		Padding(0, gapSmall, 0, pickerPadX).Radius(radiusControl).Border(1, t.Border).Gap(gapSmall).Justify(ui.SpaceBetween).
		Cursor(ui.CursorPointer).Disabled(d.controlsLocked())
	fill := t.Surface
	if b.Hovered() || sel.Open() {
		fill = t.SurfaceHover
	}
	b.Background(b.AnimateColor("fill", fill, ui.Smooth))
	b.Children(func() {
		ui.Text(c, pickerLabel(s.view.Items, s.activeChoice)).FontSize(fontBody).FontWeight(weightMedium).
			MaxLines(1).Grow(1).Basis(0).Translate(0, -opticalLift)
		ui.Icon(c, iconChevron).Width(iconInline).Height(iconInline).TextColor(t.TextMuted)
	})
	sel.Popup(func(panel ui.Element) {
		panel.Margin(gapTight, 0, 0, 0).Padding(pickerPopupInset).Radius(radiusControl).
			Background(t.Surface).Border(1, t.Border).Shadow(0, 6, 20, 0, popupShadow)
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

// managePage is the subscription management subpage of the nodes page: the saved
// collection as one card per subscription.
func (d *Dashboard) managePage(c *ui.Context, t *ui.Theme) {
	s := &d.subscription
	if !s.loaded {
		return
	}
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		toolbar(c, func() {
			button(c, buttonSpec{label: "返回节点", icon: iconBack, style: softButton(t)}, func() { s.managing = false })
			ui.Box(c).Grow(1)
			label := "全部刷新"
			if s.listBusy {
				label = "刷新中"
			}
			button(c, buttonSpec{label: label, icon: iconRefresh, style: softButton(t),
				disabled: s.mutations > 0 || len(s.view.Items) == 0 || d.modeBusy, busy: s.listBusy}, d.refreshAllSubscriptions)
			button(c, buttonSpec{label: "添加订阅", icon: iconPlus, style: primaryButton(t),
				disabled: d.controlsLocked()}, func() {
				d.openEditor(editorAdd, "")
			})
		})
		ui.Row(c).Gap(gapSmall).AlignItems(ui.End).Padding(0, pagePadding, gapTight, pagePadding).Children(func() {
			ui.Text(c, "订阅").FontSize(fontHeadline).FontWeight(weightSemibold)
			mutedText(c, t, fmt.Sprintf("%d 个", len(s.view.Items))).FontFeatures("tnum")
		})
		ui.Text(c, "独立保存，按需切换。添加订阅不会改变当前线路。").
			FontSize(fontSecondary).TextColor(t.TextMuted).Padding(0, pagePadding, gapLarge, pagePadding)
		ui.Scroll(c).Grow(1).Basis(0).MinHeight(0).Children(func() {
			ui.Column(c).Gap(gapMedium).Padding(scrollTrim, pagePadding, scrollEnd, pagePadding).Children(func() {
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
		ui.Text(c, "还没有订阅").FontSize(fontEmphasis).FontWeight(weightSemibold)
		ui.Text(c, "添加一个订阅，开始选择线路。").FontSize(fontSecondary).TextColor(t.TextMuted)
		button(c, buttonSpec{label: "添加并使用", icon: iconPlus, style: primaryButton(t),
			disabled: d.controlsLocked()}, func() {
			d.openEditor(editorAdd, "")
		})
	})
}

// subscriptionCard is one saved subscription: where it comes from, what the
// account holds, when it last refreshed, and what to do with it. The card in use
// differs by a quiet accent tint alone.
func (d *Dashboard) subscriptionCard(c *ui.Context, t *ui.Theme, item model.SubscriptionSummary) {
	s := &d.subscription
	isActive := item.ID == s.view.ActiveID
	busy := s.busy(item.ID)
	locked := d.controlsLocked()
	card := ui.Column(c).Key("sub:"+item.ID).Gap(gapMedium).Padding(gapLarge, gapSection).Radius(radiusPanel)
	switch {
	case isActive:
		// The accent mixed into an opaque Surface, the same border as the rest.
		card.Background(t.Surface.Mix(t.Accent, activeCardTint)).Border(1, t.Border)
	case card.Hovered():
		card.Background(t.SurfaceHover).Border(1, t.Border)
	default:
		card.Background(t.Surface).Border(1, t.Border)
	}
	card.Children(func() {
		ui.Row(c).Gap(gapMedium).AlignItems(ui.Center).Children(func() {
			cardSource(c, t, isActive)
			ui.Column(c).Grow(1).Basis(0).Gap(gapLine).Children(func() {
				ui.Row(c).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
					ui.Text(c, item.Name).FontSize(fontEmphasis).FontWeight(weightSemibold).MaxLines(1)
					if isActive {
						activeBadge(c, t)
					}
				})
				ui.Text(c, item.Host).FontSize(fontCaption).TextColor(t.TextMuted).MaxLines(1)
			})
			if !isActive {
				button(c, buttonSpec{label: "使用", style: softButton(t), disabled: locked}, func() {
					d.useSubscription(item.ID)
				})
			}
			label := "刷新"
			if busy {
				label = "刷新中"
			}
			button(c, buttonSpec{label: label, icon: iconRefresh, style: softButton(t), disabled: locked, busy: busy}, func() {
				d.refreshSubscription(item.ID)
			})
			d.cardMenu(c, t, item, locked)
		})
		d.cardMetrics(c, t, item)
		if item.LastError != "" {
			warningLine(c, t, item.LastError)
		}
	})
}

// cardSource is the source tile: a globe on a soft square, in the accent when
// the subscription is the one in use.
func cardSource(c *ui.Context, t *ui.Theme, isActive bool) {
	tile := ui.Box(c).Key("来源").Width(cardIconTile).Height(cardIconTile).Radius(radiusControl).Center()
	ink, wash := t.TextMuted, t.Text.Alpha(0.08)
	if isActive {
		ink, wash = t.Accent, t.Accent.Alpha(0.09)
	}
	tile.Background(wash)
	tile.Children(func() {
		ui.Icon(c, iconGlobe).Width(cardIconGlyph).Height(cardIconGlyph).TextColor(ink)
	})
}

// activeBadge marks the subscription in use.
func activeBadge(c *ui.Context, t *ui.Theme) {
	ui.Row(c).Key("使用中").Gap(gapLine).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, iconCheck).Width(iconInline).Height(iconInline).TextColor(t.Success)
		ui.Text(c, "使用中").FontSize(fontCaption).FontWeight(weightMedium).TextColor(t.Success).NoWrap()
	})
}

// cardMenu holds the actions that fit one card poorly: rename, edit the address,
// delete.
func (d *Dashboard) cardMenu(c *ui.Context, t *ui.Theme, item model.SubscriptionSummary, disabled bool) {
	id := item.ID
	iconMenuButton(c, item.Name+"的更多操作", iconMore, softButton(t), disabled, func(m *ui.Menu) {
		if m.Item("重命名").Disabled(disabled).Chosen() {
			d.openEditor(editorRename, id)
		}
		if m.Item("修改地址").Disabled(disabled).Chosen() {
			d.openEditor(editorEditURL, id)
		}
		m.Separator()
		if m.Item("删除订阅").Disabled(disabled).Chosen() {
			d.openDelete(id)
		}
	})
}

// cardMetrics shows what the provider reports: the account's use, its term, and
// when the subscription last refreshed successfully. Metrics the provider omit
// are left out rather than shown empty.
func (d *Dashboard) cardMetrics(c *ui.Context, t *ui.Theme, item model.SubscriptionSummary) {
	hasExpiry := item.HasUsage && !item.Usage.Expire.IsZero()
	ui.Column(c).Key("指标").Gap(gapMedium).Children(func() {
		ui.Box(c).Height(1).Background(t.Border.Alpha(cardDividerAlpha))
		ui.Row(c).Gap(gapSection).AlignItems(ui.Start).Children(func() {
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
	ui.Column(c).Key("已用流量").Grow(1.4).Basis(0).Gap(gapTight).Children(func() {
		metricLabel(c, t, "已用流量")
		ui.Row(c).Gap(gapTight).AlignItems(ui.End).Children(func() {
			usedReadout := formatBytes(used)
			ui.Text(c, usedReadout.value+" "+usedReadout.unit).FontSize(fontSecondary).FontWeight(weightSemibold).
				FontFeatures("tnum").NoWrap()
			if usage.Total > 0 {
				totalReadout := formatBytes(usage.Total)
				ui.Text(c, "/ "+totalReadout.value+" "+totalReadout.unit).FontSize(fontCaption).TextColor(t.TextMuted).NoWrap()
				ratio := min(1, float32(used)/float32(usage.Total))
				ui.Text(c, fmt.Sprintf("%.0f%%", ratio*100)).FontSize(fontCaption).TextColor(usageColor(t, ratio)).
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
	ui.Column(c).Key("到期时间").Grow(1).Basis(0).Gap(gapTight).Children(func() {
		metricLabel(c, t, "到期时间")
		ui.Row(c).Gap(gapTight).AlignItems(ui.End).Children(func() {
			ui.Text(c, expire.Format(usageDateStyle)).FontSize(fontSecondary).FontWeight(weightSemibold).
				FontFeatures("tnum").NoWrap()
			ui.Text(c, remainingText(left)).FontSize(fontCaption).TextColor(color).NoWrap()
		})
	})
}

func updatedMetric(c *ui.Context, t *ui.Theme, at time.Time) {
	ui.Column(c).Key("最近成功更新").Grow(0.8).Basis(0).Gap(gapTight).Children(func() {
		metricLabel(c, t, "最近成功更新")
		ui.Text(c, updatedText(at, time.Now())).FontSize(fontSecondary).FontWeight(weightSemibold).NoWrap()
	})
}

// metricLabel heads one metric of a card.
func metricLabel(c *ui.Context, t *ui.Theme, text string) {
	ui.Text(c, text).FontSize(fontCaption).TextColor(t.TextMuted)
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
	ui.DialogBase(c, &s.editorOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(dialogScrim)
		panel.Width(dialogWidth).Padding(pagePadding).Gap(gapLarge).Radius(radiusPanel).
			Background(t.Surface).Border(1, t.Border).Shadow(0, 10, 30, 0, dialogShadow)
		panel.Children(func() { d.editorForm(c, t) })
	})
}

// editorForm is the add/edit dialog's body: an optional name and the
// subscription address, filled from what is stored when editing.
func (d *Dashboard) editorForm(c *ui.Context, t *ui.Theme) {
	s := &d.subscription
	e := &s.editor
	first := len(s.view.Items) == 0
	title, description, submit := "添加订阅", "保存一个新订阅，不改变当前正在使用的线路。", "添加"
	switch e.intent {
	case editorAdd:
		if first {
			description, submit = "添加第一个订阅，并将它设为当前使用。", "添加并使用"
		}
	case editorRename:
		title, description, submit = "重命名", "名称可以自由修改。地址保持不变。", "保存修改"
	case editorEditURL:
		title, description, submit = "编辑订阅", "名称可以自由修改。新地址校验成功后才替换旧内容。", "保存修改"
	}
	hint := "名称留空时使用来源域名。"
	urlPlaceholder := "https://provider.example.com/subscribe"
	if !e.sourceLoaded {
		hint = "正在读取已保存的订阅地址…"
		urlPlaceholder = "正在读取订阅信息…"
	}
	ui.Column(c).Gap(gapSection).Children(func() {
		ui.Column(c).Gap(gapSmall).Children(func() {
			ui.Row(c).Gap(gapMedium).AlignItems(ui.Center).Children(func() {
				ui.Text(c, title).FontSize(fontHeadline).FontWeight(weightSemibold).Grow(1)
				iconButton(c, "关闭", iconClose, softButton(t), func() { s.editorOpen = false })
			})
			mutedText(c, t, description)
		})
		ui.Column(c).Gap(gapSmall).Children(func() {
			fieldLabel(c, t, "订阅名称", "选填")
			ui.TextInput(c, &e.name).Label("订阅名称").Placeholder("例如：备用线路").Disabled(e.busy)
			fieldLabel(c, t, "订阅地址", "")
			ui.TextInput(c, &e.url).Label("订阅地址").Placeholder(urlPlaceholder).Disabled(e.busy || !e.sourceLoaded).OnSubmit(d.submitEditor)
			ui.Text(c, hint).FontSize(fontCaption).TextColor(t.TextMuted)
			if e.err != "" {
				ui.Text(c, e.err).FontSize(fontSecondary).TextColor(t.Danger)
			}
		})
		ui.Row(c).Gap(gapSmall).Justify(ui.End).Children(func() {
			button(c, buttonSpec{label: "取消", style: softButton(t), disabled: e.busy}, func() { s.editorOpen = false })
			button(c, buttonSpec{label: submit, style: primaryButton(t), busy: e.busy,
				disabled: e.busy || !e.sourceLoaded || strings.TrimSpace(e.url) == ""}, d.submitEditor)
		})
	})
}

// deleteDialog confirms deleting a subscription, asking for its successor when
// the one in use goes and others remain.
func (d *Dashboard) deleteDialog(c *ui.Context, t *ui.Theme) {
	s := &d.subscription
	ui.DialogBase(c, &s.deleteOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(dialogScrim)
		panel.Width(dialogWidth).Padding(pagePadding).Gap(gapLarge).Radius(radiusPanel).
			Background(t.Surface).Border(1, t.Border).Shadow(0, 10, 30, 0, dialogShadow)
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
	description := "确定删除「" + dlg.name + "」？不会影响当前正在使用的订阅。"
	submit := "删除"
	switch {
	case needReplacement:
		description = "「" + dlg.name + "」正在使用。请选择替代订阅，切换成功后再删除。"
		submit = "切换并删除"
	case lastOne:
		description = "「" + dlg.name + "」是最后一个订阅。删除后将回到未配置状态，不再通过此订阅代理。"
		submit = "停用并删除"
	}
	ui.Column(c).Gap(gapSection).Children(func() {
		ui.Column(c).Gap(gapSmall).Children(func() {
			ui.Row(c).Gap(gapMedium).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconTrash).Width(iconTitle).Height(iconTitle).TextColor(t.Danger)
				ui.Text(c, "删除订阅").FontSize(fontHeadline).FontWeight(weightSemibold).Grow(1)
				iconButton(c, "关闭", iconClose, softButton(t), func() { s.deleteOpen = false })
			})
			mutedText(c, t, description)
		})
		if needReplacement {
			ui.Column(c).Key("替代").Gap(gapSmall).Children(func() {
				fieldLabel(c, t, "切换到另一个订阅", "")
				subscriptionPicker(c, t, others, &dlg.replacement)
				ui.Text(c, "切换成功后再删除，不会自动替你选择线路。").FontSize(fontCaption).TextColor(t.TextMuted)
			})
		}
		if dlg.err != "" {
			ui.Text(c, dlg.err).FontSize(fontSecondary).TextColor(t.Danger)
		}
		ui.Row(c).Gap(gapSmall).Justify(ui.End).Children(func() {
			button(c, buttonSpec{label: "取消", style: softButton(t), disabled: dlg.busy}, func() { s.deleteOpen = false })
			button(c, buttonSpec{label: submit, style: dangerButton(t), busy: dlg.busy,
				disabled: dlg.busy || (needReplacement && dlg.replacement == "")}, d.submitDelete)
		})
	})
}

// fieldLabel names a form field, its optional note in the muted colour.
func fieldLabel(c *ui.Context, t *ui.Theme, text string, note string) {
	ui.Row(c).Key(text).Gap(gapTight).AlignItems(ui.End).Children(func() {
		ui.Text(c, text).FontSize(fontSecondary).FontWeight(weightSemibold)
		if note != "" {
			ui.Text(c, note).FontSize(fontCaption).TextColor(t.TextMuted)
		}
	})
}

// subscriptionPicker chooses among items into chosen; the delete dialog uses it
// to name the successor of the subscription it removes.
func subscriptionPicker(c *ui.Context, t *ui.Theme, items []model.SubscriptionSummary, chosen *string) {
	sel := ui.SelectBase(c, chosen)
	b := sel.Trigger
	b.Label("选择订阅").Height(switchHeight).Grow(1).Padding(0, gapSmall, 0, pickerPadX).
		Radius(radiusControl).Background(t.Surface).Border(1, t.Border).Gap(gapSmall).
		Justify(ui.SpaceBetween).Cursor(ui.CursorPointer)
	b.Children(func() {
		if label := pickerLabel(items, *chosen); label != "" {
			ui.Text(c, label).FontSize(fontBody).FontWeight(weightMedium).MaxLines(1).
				Grow(1).Basis(0).Translate(0, -opticalLift)
		} else {
			ui.Text(c, "请选择订阅").FontSize(fontBody).TextColor(t.TextMuted).
				Grow(1).Basis(0).NoWrap().Translate(0, -opticalLift)
		}
		ui.Icon(c, iconChevron).Width(iconInline).Height(iconInline).TextColor(t.TextMuted)
	})
	sel.Popup(func(panel ui.Element) {
		panel.Margin(gapTight, 0, 0, 0).Padding(pickerPopupInset).Radius(radiusControl).
			Background(t.Surface).Border(1, t.Border).Shadow(0, 6, 20, 0, popupShadow)
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

// usageLine shows how much traffic the account used of its total, as a bar
// that warms as the limit nears, and when the account ends.
func usageLine(c *ui.Context, t *ui.Theme, usage model.Usage, now time.Time) {
	used := usage.Upload + usage.Download
	ui.Row(c).Gap(gapLarge).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
			mutedText(c, t, "已用")
			usedText := formatBytes(used)
			value := usedText.value + " " + usedText.unit
			if usage.Total > 0 {
				total := formatBytes(usage.Total)
				value += " / " + total.value + " " + total.unit
			}
			ui.Text(c, value).FontSize(fontSecondary).FontWeight(weightSemibold).FontFeatures("tnum").NoWrap()
			if usage.Total > 0 {
				ratio := min(1, float32(used)/float32(usage.Total))
				usageBar(c, t, ratio)
				ui.Text(c, fmt.Sprintf("%.0f%%", ratio*100)).FontSize(fontSecondary).TextColor(usageColor(t, ratio)).FontFeatures("tnum").NoWrap()
			}
		})
		if !usage.Expire.IsZero() {
			ui.Row(c).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
				mutedText(c, t, "到期")
				ui.Text(c, usage.Expire.Format(usageDateStyle)).FontSize(fontSecondary).FontWeight(weightSemibold).FontFeatures("tnum").NoWrap()
				left := usage.Expire.Sub(now)
				color := t.TextMuted
				if left < expiryWarning {
					color = t.Warning
				}
				ui.Text(c, remainingText(left)).FontSize(fontSecondary).TextColor(color).NoWrap()
			})
		}
	})
}

// usageBar fills to ratio, springing to a new value after a refresh.
func usageBar(c *ui.Context, t *ui.Theme, ratio float32) {
	track := ui.Box(c).Width(usageBarWidth).Height(usageBarHeight).Radius(usageBarHeight / 2).Background(t.Text.Alpha(softFillAlpha))
	width := track.AnimateTo("used", max(usageBarHeight, ratio*usageBarWidth), ui.Snappy)
	fill := usageColor(t, ratio)
	track.Children(func() {
		ui.Box(c).Width(width).Height(usageBarHeight).Radius(usageBarHeight / 2).Background(fill)
	})
}

func usageColor(t *ui.Theme, ratio float32) ui.Color {
	switch {
	case ratio >= usageCritical:
		return t.Danger
	case ratio >= usageWarning:
		return t.Warning
	default:
		return t.Accent
	}
}

// updatedText says how long ago the subscription was downloaded.
func updatedText(at time.Time, now time.Time) string {
	if at.IsZero() {
		return "更新时间未知,刷新一次即可显示"
	}
	ago := now.Sub(at)
	switch {
	case ago < time.Minute:
		return "刚刚更新"
	case ago < time.Hour:
		return fmt.Sprintf("%d 分钟前更新", int(ago.Minutes()))
	case ago < dayLength:
		return fmt.Sprintf("%d 小时前更新", int(ago.Hours()))
	default:
		return fmt.Sprintf("%d 天前更新", int(ago/dayLength))
	}
}

// remainingText says how long the account has left.
func remainingText(left time.Duration) string {
	switch {
	case left <= 0:
		return "已到期"
	case left < dayLength:
		return "今天到期"
	default:
		return fmt.Sprintf("还剩 %d 天", int(left/dayLength))
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
