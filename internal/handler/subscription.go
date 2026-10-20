package handler

import (
	"fmt"
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
)

// subscriptionBar sits under the route. It asks for the link until a
// subscription exists, then shows where it came from, how fresh it is,
// how much of the account is used and when it ends.
func (d *Dashboard) subscriptionBar(c *ui.Context, t *ui.Theme) {
	if !d.subscription.saved || d.subscription.editing {
		d.linkInput(c, t)
		return
	}
	d.subscriptionSummary(c, t)
}

func (d *Dashboard) linkInput(c *ui.Context, t *ui.Theme) {
	sub := &d.subscription
	toolbar(c, func() {
		ui.TextInput(c, &sub.input).Placeholder("粘贴订阅链接").Grow(1).Disabled(sub.busy).OnSubmit(d.submitLink)
		label := "导入"
		if sub.busy {
			label = "导入中"
		}
		button(c, buttonSpec{label: label, icon: iconImport, style: primaryButton(t), disabled: strings.TrimSpace(sub.input) == "", busy: sub.busy}, d.submitLink)
		if sub.saved {
			button(c, buttonSpec{label: "取消", style: softButton(t), disabled: sub.busy}, func() { sub.editing = false })
		}
	})
}

func (d *Dashboard) submitLink() {
	link := strings.TrimSpace(d.subscription.input)
	if link == "" || d.subscription.busy {
		return
	}
	d.importSubscription(link)
}

func (d *Dashboard) subscriptionSummary(c *ui.Context, t *ui.Theme) {
	sub := &d.subscription
	summary := sub.summary
	now := time.Now()
	ui.Column(c).Gap(10).Padding(0, pagePadding, gapLarge, pagePadding).Children(func() {
		ui.Row(c).Gap(gapMedium).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Gap(gapLarge).Grow(1).AlignItems(ui.Center).Children(func() {
				planNote(c, t, "订阅", summary.Host)
				mutedText(c, t, updatedText(summary.UpdatedAt, now)).NoWrap()
			})
			label := "刷新订阅"
			if sub.busy {
				label = "刷新中"
			}
			button(c, buttonSpec{label: label, icon: iconRefresh, style: softButton(t), busy: sub.busy}, d.updateSubscription)
			button(c, buttonSpec{label: "换订阅链接", icon: iconLink, style: softButton(t), disabled: sub.busy}, func() { sub.editing = true })
		})
		switch {
		case summary.HasUsage:
			usageLine(c, t, summary.Usage, now)
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
	})
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

// splitPlanNote splits "剩余流量：1023.86 GB" into its label and value; a
// note without a colon is all value.
func splitPlanNote(note string) (string, string) {
	for _, sep := range []string{"：", ":"} {
		if label, value, found := strings.Cut(note, sep); found {
			return strings.TrimSpace(label), strings.TrimSpace(value)
		}
	}
	return "", note
}
