package handler

import (
	"log/slog"
	"time"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
	"hop/locales"
)

const (
	// failureToastLife is how long a failure shows before it goes; it stays
	// while the pointer rests on it and, with its details open, until closed.
	failureToastLife = 8 * time.Second
	// failureToastUntilClosed is MyGo's toast timeout that never ends.
	failureToastUntilClosed time.Duration = -1
	// toastWidth is a toast's width in a window wide enough for it.
	toastWidth float32 = 440
	// failureBadgeSize is the round tint behind a failure's mark.
	failureBadgeSize  float32 = 28
	failureBadgeAlpha float32 = 0.12
	// failureDetailLines bounds the details shown; Copy takes them whole.
	failureDetailLines         = 8
	toastShift         float32 = 8
	toastFade                  = 180 * time.Millisecond
)

// failureToastID names the one failure toast: a new failure takes the place
// of the last, so one cause that fails several steps shows once, not as a
// stack of cards.
const failureToastID = "failure"

// failure is something that went wrong, as its toast says it.
type failure struct {
	title  string
	reason string
	detail string
	// expanded shows the detail under the reason; copied says Copy ran.
	expanded bool
	copied   bool
}

// failuresState is the last failure and how far each window has shown the
// failures: seq counts them, 0 while none happened.
type failuresState struct {
	last       failure
	seq        int
	windowSeen int
	flyoutSeen int
}

// fail shows what went wrong without touching the connection status.
func (d *Dashboard) fail(a action, err error) {
	slog.Error("dashboard action failed", "action", a.id, "error", err)
	reason := service.FailureReasonOf(err)
	d.failures.seq++
	d.failures.last = failure{title: a.title(), reason: failureReasonText(reason), detail: reason.Detail}
}

// failConnection is fail for errors that mean the core cannot be reached.
func (d *Dashboard) failConnection(a action, err error) {
	d.fail(a, err)
	d.status = statusFailed
}

// failureReasonText says why an action failed, or nothing when its error
// says nothing the user can act on.
func failureReasonText(reason model.FailureReason) string {
	switch reason.Kind {
	case model.FailureExplained:
		return reason.Text
	case model.FailureCoreDown:
		return locales.FailureReasonCoreDown()
	case model.FailureTimeout:
		return locales.FailureReasonTimeout()
	case model.FailureProviderRefused:
		return locales.FailureReasonProviderRefused(reason.Status)
	case model.FailureProviderDown:
		return locales.FailureReasonProviderDown(reason.Status)
	case model.FailureHostNotFound:
		return locales.FailureReasonHostNotFound(reason.Host)
	case model.FailureNetwork:
		return locales.FailureReasonNetwork()
	default:
		return ""
	}
}

// showFailures shows the last failure when the window has not shown it since
// seen, and returns how far it has shown the failures now. Failures that came
// and went in between are in the log only.
func (d *Dashboard) showFailures(c *ui.Context, seen int) int {
	if d.failures.seq > seen {
		c.AddToast(failureToast(d.failures.last, failureToastLife))
	}
	return d.failures.seq
}

// failureToast is the toast of failure f, shown for life.
func failureToast(f failure, life time.Duration) ui.Toast {
	return ui.Toast{ID: failureToastID, Title: f.title, Description: f.reason, Type: "error", Timeout: life}
}

// toasts builds the window's toasts in Hop's look, at its bottom: a failure
// as its card, any other toast as one line on an opaque card. padding keeps
// them off the window's edges.
func (d *Dashboard) toasts(c *ui.Context, t *ui.Theme, padding float32) {
	ui.ToastViewportBase(c, func(viewport ui.Element, toasts []ui.Toast) {
		viewport.Padding(padding).Gap(gapSmall)
		for _, toast := range toasts {
			// The row centres the card and lets it shrink in a narrow window.
			ui.Row(c.Key(toast.ID)).Justify(ui.Center).Transition(toastTransition()).Children(func() {
				if toast.ID == failureToastID {
					d.failureCard(c, t, toast, d.failures.last)
					return
				}
				plainToastCard(c, t, toast)
			})
		}
	})
}

// toastTransition rises a toast in and lets it sink away.
func toastTransition() ui.ElementTransition {
	return ui.ElementTransition{
		Duration: toastFade,
		Enter:    &ui.Motion{Opacity: 0, Y: toastShift},
		Exit:     &ui.Motion{Opacity: 0, Y: toastShift},
	}
}

// toastCard is the opaque card every toast sits on.
func toastCard(t *ui.Theme, root ui.Element) ui.Element {
	return root.Width(toastWidth).Shrink(1).MinWidth(0).Radius(radiusPanel).
		Background(overlaySurface(t)).Border(hairlineWidth, t.Border).Shadow(0, 10, 30, 0, dialogShadow)
}

// plainToastCard is a toast that only says something happened, as Copied.
func plainToastCard(c *ui.Context, t *ui.Theme, toast ui.Toast) {
	parts := ui.ToastBase(c, toast)
	toastCard(t, parts.Root.Row()).Padding(10, 14).Children(func() {
		ui.Text(c, toast.Title).FontSize(fontBody).FontWeight(weightMedium).TextColor(t.Text).MaxLines(2)
	})
}

// failureCard is a failure's toast: what failed, why in the user's words
// when the error says, and the error itself under Details, to copy for a
// report.
func (d *Dashboard) failureCard(c *ui.Context, t *ui.Theme, toast ui.Toast, f failure) {
	parts := ui.ToastBase(c, toast)
	indent := failureBadgeSize + gapMedium
	toastCard(t, parts.Root.Column()).Padding(gapMedium).Gap(gapSmall).Children(func() {
		ui.Row(c).Gap(gapMedium).AlignItems(ui.Start).Children(func() {
			ui.Row(c).Width(failureBadgeSize).Height(failureBadgeSize).Radius(failureBadgeSize / 2).Shrink(0).
				Background(dangerFill(t).Alpha(failureBadgeAlpha)).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconWarn).Width(buttonIconSize).Height(buttonIconSize).TextColor(dangerFill(t))
			})
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(gapLine).Children(func() {
				ui.Text(c, f.title).FontSize(fontBody).FontWeight(weightSemibold).TextColor(t.Text)
				if f.reason != "" {
					mutedText(c, t, f.reason)
				}
			})
			parts.CloseButton().Label(locales.DismissNotice()).Width(24).Height(24).Radius(12).Shrink(0).
				Justify(ui.Center).AlignItems(ui.Center).Cursor(ui.CursorPointer).Children(func() {
				ui.Icon(c, iconClose).Width(12).Height(12).TextColor(t.TextMuted)
			})
		})
		if f.detail == "" {
			return
		}
		if f.expanded {
			ui.Row(c).Padding(0, 0, 0, indent).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Padding(gapSmall, 10).Radius(radiusControl).
					Background(t.Text.Alpha(softFillAlpha)).Children(func() {
					ui.Text(c, f.detail).FontSize(fontCaption).TextColor(t.TextMuted).
						MaxLines(failureDetailLines).Selectable()
				})
			})
		}
		ui.Row(c).Padding(0, 0, 0, indent).Gap(gapLarge).AlignItems(ui.Center).Children(func() {
			label := locales.FailureDetails()
			if f.expanded {
				label = locales.FailureHideDetails()
			}
			textButton(c, t, label, nil, false, false, func() { d.toggleFailureDetail(c) })
			copyLabel := locales.FailureCopyDetails()
			if f.copied {
				copyLabel = locales.Copied()
			}
			textButton(c, t, copyLabel, iconCopy, false, false, func() { d.copyFailureDetail(c) })
		})
	})
}

// toggleFailureDetail opens or closes a failure's details. While they are
// open the toast stays until closed; closed again, it has its time anew.
func (d *Dashboard) toggleFailureDetail(c *ui.Context) {
	d.failures.last.expanded = !d.failures.last.expanded
	life := failureToastLife
	if d.failures.last.expanded {
		life = failureToastUntilClosed
	}
	c.AddToast(failureToast(d.failures.last, life))
}

// copyFailureDetail puts a failure's details on the clipboard, for a report.
func (d *Dashboard) copyFailureDetail(c *ui.Context) {
	c.WriteClipboard(d.failures.last.detail)
	d.failures.last.copied = true
}
