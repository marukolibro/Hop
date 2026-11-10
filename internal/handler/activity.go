package handler

import (
	"github.com/egoist/mygo/ui"

	"hop/locales"
)

// The activity page's two views.
const (
	activityConnections = iota
	activityLog
)

// activityLabels name the activity page's views, index for index.
func activityLabels() []string {
	return []string{locales.PageConnections(), locales.PageLogs()}
}

// activityPage is what is happening now: the connections the core carries, or
// its log, one under a switch in the heading. The log is rarely read, so it
// is a view of this page and not a page of its own.
func (d *Dashboard) activityPage(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Fill().Children(func() {
		ui.Column(c).Padding(28, pageGutter(c), 26, pageGutter(c)).Children(func() {
			pageHeadingRow(c, t, locales.PageActivity(), locales.ActivitySubtitle(), func() {
				slidingSwitch(c, locales.ActivityViewLabel(), &d.activity, activityLabels(), pageSwitchStyle(t, 64), func(int) {})
			})
		})
		switch d.activity {
		case activityLog:
			d.logsView(c, t)
		default:
			d.connectionsView(c, t)
		}
	})
}
