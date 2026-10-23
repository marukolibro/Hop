package handler

import (
	"context"
	"errors"
	"fmt"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
)

const (
	unlockStartLabel = "解锁检测"
	unlockStopLabel  = "停止检测"
	unlockNoneText   = "均不可用"
	unlockUnsureText = "未能检测"
)

// unlockState is the unlock check: what it found for each group, kept until
// Hop quits, and the run in progress.
type unlockState struct {
	results map[string]model.Unlock
	// running says a run goes; done and total count the groups it checks.
	running bool
	done    int
	total   int
	// cancel ends the run; seq tells a run's replies from an older one's.
	cancel context.CancelFunc
	seq    int
}

// unlockLabel is one service a node works with, as its tile and tooltip say.
type unlockLabel struct {
	text    string
	tooltip string
	partial bool
}

// unlockLabels lists the services that work through a node, in a fixed order;
// a refused or unchecked service is left out.
func unlockLabels(u model.Unlock) []unlockLabel {
	var labels []unlockLabel
	if u.ChatGPT.State == model.UnlockAvailable {
		labels = append(labels, unlockLabel{text: "GPT", tooltip: "ChatGPT 可用"})
	}
	if u.Netflix.State == model.UnlockAvailable || u.Netflix.State == model.UnlockPartial {
		label := unlockLabel{text: "Netflix", tooltip: "Netflix 可用" + regionNote(u.Netflix.Region)}
		if u.Netflix.State == model.UnlockPartial {
			label.tooltip = "Netflix 只能看自制剧" + regionNote(u.Netflix.Region)
			label.partial = true
		}
		labels = append(labels, label)
	}
	if u.YouTube.State == model.UnlockAvailable {
		labels = append(labels, unlockLabel{text: "YouTube", tooltip: "YouTube Premium 可用" + regionNote(u.YouTube.Region)})
	}
	return labels
}

// regionNote is the region a service placed a node in, as a tooltip ends.
func regionNote(code string) string {
	if code == "" {
		return ""
	}
	return ",地区:" + countryName(code)
}

// unlockSummary says what a group with no working service shows: that every
// check was refused, or that some could not be made.
func unlockSummary(u model.Unlock) string {
	for _, r := range []model.UnlockResult{u.ChatGPT, u.Netflix, u.YouTube} {
		if r.State == model.UnlockUnknown {
			return unlockUnsureText
		}
	}
	return unlockNoneText
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

// unlockControls is the check's progress and its button, beside the latency
// test's.
func (d *Dashboard) unlockControls(c *ui.Context, t *ui.Theme) {
	running := d.unlock.running
	if running && d.unlock.total > 0 {
		captionText(c, t, fmt.Sprintf("%d/%d", d.unlock.done, d.unlock.total)).FontFeatures("tnum").
			ContentTransition(ui.ContentTransition{Numeric: true})
	}
	spec := buttonSpec{label: unlockStartLabel, icon: iconUnlock, style: mainSoftButton(t),
		disabled: d.services.Unlock == nil || d.controlsLocked() || d.testing != ""}
	if running {
		spec.label, spec.icon, spec.disabled = unlockStopLabel, iconClose, false
	}
	button(c, spec, d.toggleUnlock)
}

// unlockRow is what the check found for an option that is a group, under its
// latency line: a chip for each service that works, in the accent colour for a
// full one and neutral for Netflix's own productions only. An option the check
// did not reach, such as a node, shows nothing.
func (d *Dashboard) unlockRow(c *ui.Context, t *ui.Theme, option model.NodeView) {
	result, checked := d.unlock.results[option.Name]
	if !checked || option.Via == "" {
		return
	}
	ui.Row(c).Key("unlock").Gap(gapTight).Height(chipHeight).Clip().AlignItems(ui.Center).Children(func() {
		unlockChips(c, t, result)
	})
}

// unlockChips are the services that work through a group, or what stands in
// their place.
func unlockChips(c *ui.Context, t *ui.Theme, result model.Unlock) {
	labels := unlockLabels(result)
	if len(labels) == 0 {
		captionText(c, t, unlockSummary(result)).MaxLines(1)
		return
	}
	for _, label := range labels {
		unlockChip(c, t, label)
	}
}

// unlockChip is one working service's label.
func unlockChip(c *ui.Context, t *ui.Theme, label unlockLabel) {
	fill, ink := t.Accent.Alpha(tileChipFill), textOn(t, accentInk(t))
	if label.partial {
		fill, ink = t.Text.Alpha(neutralFillAlpha), t.TextMuted
	}
	ui.Box(c).Key(label.text).Padding(1, 6).Radius(radiusChip).Background(fill).Tooltip(label.tooltip).Children(func() {
		ui.Text(c, label.text).FontSize(fontCaption).FontWeight(weightMedium).TextColor(ink).NoWrap()
	})
}
