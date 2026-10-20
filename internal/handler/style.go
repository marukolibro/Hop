package handler

import "github.com/egoist/mygo/ui"

// Hop's type scale, spacing and radii. Every view takes its sizes from
// here, so the same kind of thing looks the same on every page.
const (
	// fontCaption is for tags and timestamps, fontSecondary for labels and
	// explanations, fontBody for running text and every control,
	// fontEmphasis for tab and tile titles, fontHeadline for the exit node.
	fontCaption   float32 = 11
	fontSecondary float32 = 12
	fontBody      float32 = 13
	fontEmphasis  float32 = 14
	fontHeadline  float32 = 18

	weightMedium   = 500
	weightSemibold = 600
	weightBold     = 700

	gapTight   float32 = 6
	gapSmall   float32 = 8
	gapMedium  float32 = 12
	gapLarge   float32 = 16
	gapSection float32 = 20

	pagePadding float32 = 24

	controlSpacing float32 = 3.5
	radiusControl  float32 = 8
	radiusPanel    float32 = 12
	radiusChip     float32 = 4
)

// mutedText is secondary text: labels, explanations, counts.
func mutedText(c *ui.Context, t *ui.Theme, text string) ui.Element {
	return ui.Text(c, text).FontSize(fontSecondary).TextColor(t.TextMuted)
}

// sectionTitle heads a group of things on a page.
func sectionTitle(c *ui.Context, text string) ui.Element {
	return ui.Text(c, text).FontSize(fontBody).FontWeight(weightSemibold)
}

// chip is a small label for a node's line or rate, or a log level.
func chip(c *ui.Context, text string, color ui.Color) {
	ui.Box(c).Key(text).Padding(1, 6).Radius(radiusChip).Background(color.Alpha(0.12)).Children(func() {
		ui.Text(c, text).FontSize(fontCaption).FontWeight(weightMedium).TextColor(color).NoWrap()
	})
}

// toolbar is the row of controls at the top of a page.
func toolbar(c *ui.Context, build func()) {
	ui.Row(c).Gap(gapMedium).AlignItems(ui.Center).Padding(0, pagePadding, gapLarge, pagePadding).Children(build)
}

// emptyState says why a page has nothing to show and what to do about it.
func emptyState(c *ui.Context, t *ui.Theme, title string, body string) {
	ui.Column(c).Grow(1).Center().Gap(gapTight).Padding(40, pagePadding).Children(func() {
		ui.Text(c, title).FontSize(fontEmphasis).FontWeight(weightSemibold)
		ui.Text(c, body).FontSize(fontSecondary).TextColor(t.TextMuted).TextAlign(ui.Center).MaxWidth(420)
	})
}
