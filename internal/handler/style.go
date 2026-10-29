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
	// fontExit is the hero's exit name.
	fontExit float32 = 22
	fontStat float32 = 30

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
	// headerMinContentWidth keeps the page tabs and traffic controls whole;
	// narrower headers sit below native controls on the right.
	headerMinContentWidth float32 = 600
	// pageContentWidth keeps a page of one column from stretching across a
	// wide window.
	pageContentWidth float32 = 640

	themeSegmentWidth float32 = 72
	// languageSegmentWidth fits the longest language name, in its own script.
	languageSegmentWidth float32 = 88

	controlSpacing float32 = 3.5
	radiusControl  float32 = 8
	radiusPanel    float32 = 12
	radiusChip     float32 = 4
	// hairlineWidth is the one-pixel rule between regions and on tiles.
	hairlineWidth float32 = 1
	// chipTintAlpha is the colored wash behind a chip on solid pages; glass
	// leaves chips unfilled so their ink stays on the window tint.
	chipTintAlpha float32 = 0.12

	// cardIconTile is the square a subscription's source icon sits on,
	// cardIconGlyph the globe inside it; cardUsageBar* the account bar in a
	// card's metrics.
	cardIconTile       float32 = 36
	cardIconGlyph      float32 = 18
	cardUsageBarWidth  float32 = 260
	cardExpiryMinWidth float32 = 160
	cardUpdateMinWidth float32 = 120
	cardUsageBarHeight float32 = 4

	// dialogWidth is the subscription dialogs' width; localEditorHeight keeps
	// a pasted config to a fixed, scrollable box; sourceSegmentWidth fits the
	// two source labels; pickerPadX pads the trigger and its options;
	// pickerItemHeight is one option's height.
	dialogWidth float32 = 440
	// servicesDialogWidth fits a service's name, route, region, latency and
	// state on one row; servicesListHeight scrolls the list past about ten
	// rows; serviceMarkTile holds a service's mark at serviceMarkGlyph.
	servicesDialogWidth float32 = 560
	servicesListHeight  float32 = 560
	serviceMarkTile     float32 = 30
	serviceMarkGlyph    float32 = 16
	// The home cards: a card asks for at least homeCardMinWidth, so four fit
	// a wide window and the row falls to two, then one; a service's tile on
	// the services card holds its mark at serviceTileMark with a serviceDot
	// for its state; the memory bar is statMemoryBar tall.
	homeCardMinWidth   float32 = 222
	serviceTileMark    float32 = 24
	serviceDot         float32 = 9
	localEditorHeight  float32 = 168
	sourceSegmentWidth float32 = 88
	pickerPadX         float32 = 10
	pickerItemHeight   float32 = 32
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
	statChartHeight  float32 = 160
	statChartInset   float32 = 8
	statBarRadius    float32 = 2
	statTooltipPadX  float32 = 8
	statTooltipPadY  float32 = 6
	statTooltipLine  float32 = 15
	statDot          float32 = 6
	statMemoryBar    float32 = 8
	statMemoryGap    float32 = 3
	statBarWidthPart float32 = 0.62
)

// chipHeight is the height of a tile's label line.
const (
	chipHeight float32 = 17
)

// mutedText is secondary text: labels, explanations, counts. On glass it is
// medium: CJK strokes get thin over a blurred background.
func mutedText(c *ui.Context, t *ui.Theme, text string) ui.Element {
	e := ui.Text(c, text).FontSize(fontSecondary).TextColor(t.TextMuted)
	if windowGlass(t) {
		e.FontWeight(weightMedium)
	}
	return e
}

// captionText is the smallest secondary text: times, units and hints, medium
// on glass for the same reason as mutedText.
func captionText(c *ui.Context, t *ui.Theme, text string) ui.Element {
	e := ui.Text(c, text).FontSize(fontCaption).TextColor(t.TextMuted)
	if windowGlass(t) {
		e.FontWeight(weightMedium)
	}
	return e
}

// sectionTitle heads a group of things on a page.
func sectionTitle(c *ui.Context, text string) ui.Element {
	return ui.Text(c, text).FontSize(fontBody).FontWeight(weightSemibold)
}

// horizontalHairline separates one full-width region from the next.
func horizontalHairline(c *ui.Context, t *ui.Theme) ui.Element {
	return ui.Box(c).Height(hairlineWidth).Background(t.Border)
}

// verticalHairline separates the sidebar from the content.
func verticalHairline(c *ui.Context, t *ui.Theme) ui.Element {
	return ui.Box(c).Width(hairlineWidth).Background(t.Border)
}

// chip is a small label for a node's line or rate, or a log level. CJK
// glyphs sit low in a box this small, so a label with any is lifted; a Latin
// one such as "UDP" is centred already.
func chip(c *ui.Context, t *ui.Theme, text string, color ui.Color) {
	fill := color.Alpha(chipTintAlpha)
	if windowGlass(t) {
		fill = ui.Transparent
	}
	ui.Box(c).Key(text).Padding(1, 6).Radius(radiusChip).Background(fill).Children(func() {
		label := ui.Text(c, text).FontSize(fontCaption).FontWeight(weightMedium).TextColor(color).NoWrap()
		if strings.ContainsFunc(text, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
			label.Translate(0, -opticalLift)
		}
	})
}

// pageGutter is the page-scroll inset at the design's three widths.
func pageGutter(c *ui.Context) float32 {
	width, _ := c.Size()
	switch {
	case width < 760:
		return 18
	case width < 1050:
		return 24
	default:
		return 34
	}
}

// pageHeading names a page and says what it holds.
func pageHeading(c *ui.Context, t *ui.Theme, title string, subtitle string) ui.Element {
	return ui.Column(c).Key("heading").Gap(5).Children(func() {
		ui.Text(c, title).FontSize(fontHeadline).FontWeight(weightSemibold).LetterSpacing(-0.3)
		if subtitle != "" {
			mutedText(c, t, subtitle)
		}
	})
}

// pageHeadingRow puts a badge or action at the right of a page heading.
func pageHeadingRow(c *ui.Context, t *ui.Theme, title string, subtitle string, accessory func()) {
	ui.Row(c).Gap(gapMedium).AlignItems(ui.Start).Children(func() {
		pageHeading(c, t, title, subtitle).Grow(1)
		if accessory != nil {
			accessory()
		}
	})
}

// badge is the small tinted label beside a page heading.
func badge(c *ui.Context, t *ui.Theme, text string) {
	fill := t.Accent.Alpha(0.10)
	if onGlass(t) {
		fill = glassPanel(t).Mix(t.Accent, glassBadgeTint)
	}
	ui.Box(c).Key("badge").Padding(3, 7).Radius(5).Background(fill).Children(func() {
		ui.Text(c, text).FontSize(fontCaption).TextColor(accentInk(t)).NoWrap()
	})
}

// panelCard is the translucent face the design gives lists, charts and
// tables. Solid windows use their opaque surface.
func panelCard(c *ui.Context, t *ui.Theme) ui.Element {
	panel := ui.Column(c).Border(hairlineWidth, glassMaterialLine(t)).Radius(radiusPanel)
	if onGlass(t) {
		return panel.Background(glassPanel(t))
	}
	return panel.Background(t.Surface)
}

// mainSoftButton is the main window's material button; the flyout keeps the
// opaque soft fill over its native panel.
func mainSoftButton(t *ui.Theme) buttonStyle {
	if onGlass(t) {
		return buttonStyle{fill: glassButton(t), hover: glassFill(t, glassButtonHover), ink: t.Text, outline: true}
	}
	style := softButton(t)
	style.outline = true
	return style
}

// mainDangerButton is the main window's quiet destructive button.
func mainDangerButton(t *ui.Theme) buttonStyle {
	if onGlass(t) {
		return buttonStyle{fill: glassButton(t).Mix(t.Danger, glassDangerFill), hover: glassFill(t, glassButtonHover).Mix(t.Danger, glassDangerHover), ink: textOn(t, t.Danger), outline: true}
	}
	style := dangerButton(t)
	style.outline = true
	return style
}

// dialogField is the face of an input or picker inside an opaque dialog: the
// window's glass surface would wash out on the card.
func dialogField(t *ui.Theme) ui.Color {
	return overlaySurface(t).Mix(t.Text, 0.04)
}

// dialogSwitchStyle is the page switch's opaque twin for dialog cards: the
// window's glass fills are too faint on an opaque panel.
func dialogSwitchStyle(t *ui.Theme, segmentWidth float32) switchStyle {
	surface := overlaySurface(t)
	return switchStyle{
		track: surface.Mix(t.Text, 0.07), thumb: surface,
		active: t.Text, inactive: t.TextMuted, segmentWidth: segmentWidth,
	}
}

// toolbar is the row of controls at the top of a page.
func toolbar(c *ui.Context, build func()) {
	ui.Row(c).Gap(gapMedium).AlignItems(ui.Center).Padding(0, pageGutter(c), gapLarge, pageGutter(c)).Children(build)
}

// emptyState says why a page has nothing to show and what to do about it.
func emptyState(c *ui.Context, t *ui.Theme, title string, body string) {
	ui.Column(c).Grow(1).Center().Gap(gapTight).Padding(40, pagePadding).Children(func() {
		ui.Text(c, title).FontSize(fontEmphasis).FontWeight(weightSemibold)
		mutedText(c, t, body).TextAlign(ui.Center).MaxWidth(420)
	})
}
