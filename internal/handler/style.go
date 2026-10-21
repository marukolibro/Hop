package handler

import (
	"strings"
	"unicode"

	"github.com/egoist/mygo/ui"
)

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
	// gapLine is the hair gap between a title and its caption.
	gapLine float32 = 3

	pagePadding float32 = 24
	// pageContentWidth keeps a page of one column from stretching across a
	// wide window.
	pageContentWidth float32 = 640

	controlSpacing float32 = 3.5
	radiusControl  float32 = 8
	radiusPanel    float32 = 12
	radiusChip     float32 = 4

	// panelPadY/panelPadX pad the bar panels, matching the route bar.
	panelPadY float32 = 14
	panelPadX float32 = 18

	// cardIconTile is the square a subscription's source icon sits on,
	// cardIconGlyph the globe inside it; cardUsageBar* the account bar in a
	// card's metrics.
	cardIconTile       float32 = 36
	cardIconGlyph      float32 = 18
	cardUsageBarWidth  float32 = 260
	cardUsageBarHeight float32 = 4

	// dialogWidth is the subscription dialogs' width; selectMinWidth keeps the
	// picker readable; pickerPadX pads the picker trigger and its options,
	// pickerItemHeight is one option's height.
	dialogWidth      float32 = 440
	selectMinWidth   float32 = 180
	pickerPadX       float32 = 10
	pickerItemHeight float32 = 32
	// pickerPopupInset evenly insets the menu options.
	pickerPopupInset float32 = 4

	// Icon sizes of the subscription views: 12 inline, 16 leading in a bar, 18
	// in a dialog title, 28 in an empty state.
	iconInline float32 = 12
	iconLead   float32 = 16
	iconTitle  float32 = 18
	iconEmpty  float32 = 28

	// scrollTrim/scrollEnd pad the scrolling list top and bottom; emptyPadY pads
	// the empty state.
	scrollTrim float32 = 4
	scrollEnd  float32 = 28
	emptyPadY  float32 = 40

	// Statistics page: the daily chart's height and top inset, the bars'
	// corner, the tooltip's padding and line height, and the dot before a
	// split or legend label.
	statChartHeight  float32 = 124
	statChartInset   float32 = 8
	statBarRadius    float32 = 2
	statTooltipPadX  float32 = 8
	statTooltipPadY  float32 = 6
	statTooltipLine  float32 = 15
	statDot          float32 = 6
	statBarWidthPart float32 = 0.62
)

// chipHeight is one chip's height, and chipLineGap what keeps a wrapped
// second line of chips out of a row of one chip's height.
const (
	chipHeight  float32 = 17
	chipLineGap float32 = 4
)

// mutedText is secondary text: labels, explanations, counts.
func mutedText(c *ui.Context, t *ui.Theme, text string) ui.Element {
	return ui.Text(c, text).FontSize(fontSecondary).TextColor(t.TextMuted)
}

// sectionTitle heads a group of things on a page.
func sectionTitle(c *ui.Context, text string) ui.Element {
	return ui.Text(c, text).FontSize(fontBody).FontWeight(weightSemibold)
}

// chip is a small label for a node's line or rate, or a log level. CJK
// glyphs sit low in a box this small, so a label with any is lifted; a Latin
// one such as "UDP" is centred already.
func chip(c *ui.Context, text string, color ui.Color) {
	ui.Box(c).Key(text).Padding(1, 6).Radius(radiusChip).Background(color.Alpha(0.12)).Children(func() {
		label := ui.Text(c, text).FontSize(fontCaption).FontWeight(weightMedium).TextColor(color).NoWrap()
		if strings.ContainsFunc(text, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
			label.Translate(0, -opticalLift)
		}
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
