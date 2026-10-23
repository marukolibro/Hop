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

// unlockState is the unlock check of the node grid: what it found for each
// node, kept until Hop quits, and the run in progress.
type unlockState struct {
	results map[string]model.Unlock
	// group is the group being checked, "" while no run goes; done and total
	// count its nodes.
	group string
	done  int
	total int
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

// unlockSummary says what a node with no working service shows: that every
// check was refused, or that some could not be made.
func unlockSummary(u model.Unlock) string {
	for _, r := range []model.UnlockResult{u.ChatGPT, u.Netflix, u.YouTube} {
		if r.State == model.UnlockUnknown {
			return unlockUnsureText
		}
	}
	return unlockNoneText
}

// toggleUnlock starts the check of group's nodes, or ends the one running.
// It runs on the UI thread.
func (d *Dashboard) toggleUnlock(group string) {
	if d.unlock.group != "" {
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
	d.unlock.group, d.unlock.done, d.unlock.total, d.unlock.cancel = group, 0, 0, cancel
	seq, win := d.unlock.seq, d.opWin
	go func() {
		defer cancel()
		err := d.services.Unlock.Detect(ctx, group, func(r service.UnlockReport) {
			win.Update(func() {
				if seq != d.unlock.seq {
					return
				}
				d.unlock.results[r.Node] = r.Result
				d.unlock.done, d.unlock.total = r.Done, r.Total
			})
		})
		win.Update(func() {
			if seq != d.unlock.seq {
				return
			}
			d.unlock.group, d.unlock.cancel = "", nil
			if err != nil && !errors.Is(err, context.Canceled) {
				d.fail(actionCheckUnlock, err)
			}
		})
	}()
}

// unlockControls is the check's progress and its button, beside the latency
// test's.
func (d *Dashboard) unlockControls(c *ui.Context, t *ui.Theme, group string) {
	running := d.unlock.group != ""
	if running && d.unlock.group == group && d.unlock.total > 0 {
		captionText(c, t, fmt.Sprintf("%d/%d", d.unlock.done, d.unlock.total)).FontFeatures("tnum").
			ContentTransition(ui.ContentTransition{Numeric: true})
	}
	spec := buttonSpec{label: unlockStartLabel, icon: iconUnlock, style: mainSoftButton(t),
		disabled: d.services.Unlock == nil || d.controlsLocked() || d.testing != ""}
	if running {
		spec.label, spec.icon, spec.disabled = unlockStopLabel, iconClose, false
	}
	button(c, spec, func() { d.toggleUnlock(group) })
}

// unlockRow is what the check found for a node, under its latency line: a
// chip for each service that works, in the accent colour for a full one and
// neutral for Netflix's own productions only.
func (d *Dashboard) unlockRow(c *ui.Context, t *ui.Theme, node model.NodeView) {
	result, checked := d.unlock.results[node.Name]
	if !checked || node.Via != "" {
		return
	}
	labels := unlockLabels(result)
	ui.Row(c).Key("unlock").Gap(gapTight).Height(chipHeight).Clip().AlignItems(ui.Center).Children(func() {
		if len(labels) == 0 {
			captionText(c, t, unlockSummary(result)).MaxLines(1)
			return
		}
		for _, label := range labels {
			unlockChip(c, t, label)
		}
	})
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
