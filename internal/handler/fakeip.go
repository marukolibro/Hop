package handler

import (
	"strings"
	"unicode"

	"github.com/egoist/mygo/ui"

	"hop/locales"
)

// fakeIPState is the fake-ip exceptions and their edit dialog.
type fakeIPState struct {
	names []string
	busy  bool
	// editorOpen and text hold the dialog between frames; err is what the
	// service refused the last list for.
	editorOpen bool
	text       string
	err        string
}

// fakeIPNamesFromText reads the names the user typed: one per line, though
// commas and spaces between names work too.
func fakeIPNamesFromText(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == ';' || unicode.IsSpace(r) })
}

// openFakeIPEditor shows the dialog with the stored names, one per line.
func (d *Dashboard) openFakeIPEditor() {
	f := &d.fakeIP
	if f.busy || d.controlsLocked() {
		return
	}
	f.names = d.services.FakeIP.List()
	f.text, f.err, f.editorOpen = strings.Join(f.names, "\n"), "", true
}

// submitFakeIPEditor saves the typed names in the background. The dialog
// stays open with the service's reason when the list is refused, and the
// service keeps the previous list when the core rejects the rebuilt config.
func (d *Dashboard) submitFakeIPEditor() {
	f := &d.fakeIP
	if f.busy || d.controlsLocked() {
		return
	}
	f.busy, f.err = true, ""
	names := fakeIPNamesFromText(f.text)
	ctx, win := d.opCtx, d.opWin
	go func() {
		err := d.services.FakeIP.Save(ctx, names)
		win.Update(func() {
			f.busy = false
			f.names = d.services.FakeIP.List()
			if err != nil {
				f.err = err.Error()
				return
			}
			f.editorOpen = false
		})
	}()
}

// fakeIPRows is the settings row of the fake-ip exceptions: how many names
// there are, and the button that edits them.
func (d *Dashboard) fakeIPRows(c *ui.Context, t *ui.Theme) []func() {
	f := &d.fakeIP
	return []func(){func() {
		settingRow(c, t, locales.FakeIPTitle(), locales.FakeIPNote(), func() {
			ui.Row(c).Gap(gapMedium).AlignItems(ui.Center).Children(func() {
				summary := locales.FakeIPNone()
				if len(f.names) > 0 {
					summary = locales.FakeIPCount(len(f.names))
				}
				mutedText(c, t, summary)
				button(c, buttonSpec{label: locales.FakeIPEdit(), style: mainSoftButton(t), disabled: d.controlsLocked()}, d.openFakeIPEditor)
			})
		})
	}}
}

// fakeIPDialog is the edit form over a dimmed window.
func (d *Dashboard) fakeIPDialog(c *ui.Context, t *ui.Theme) {
	f := &d.fakeIP
	animatedDialog(c, "fake-ip", &f.editorOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(dialogScrim)
		panel.Width(dialogWidth).Padding(pagePadding).Gap(gapLarge).Radius(radiusPanel).
			Background(overlaySurface(t)).Border(1, t.Border).Shadow(0, 10, 30, 0, dialogShadow)
		panel.Children(func() {
			ui.Column(c).Gap(gapSection).Children(func() {
				ui.Column(c).Gap(gapSmall).Children(func() {
					ui.Row(c).Gap(gapMedium).AlignItems(ui.Center).Children(func() {
						ui.Text(c, locales.FakeIPTitle()).FontSize(fontHeadline).FontWeight(weightSemibold).Grow(1)
						iconButton(c, locales.Close(), iconClose, softButton(t), func() { f.editorOpen = false })
					})
					mutedText(c, t, locales.FakeIPDialogNote())
				})
				ui.Column(c).Gap(gapSmall).Children(func() {
					ui.TextArea(c, &f.text).Label(locales.FakeIPTitle()).Placeholder(locales.FakeIPPlaceholder()).
						Height(localEditorHeight).Background(dialogField(t)).Disabled(f.busy)
					if f.err != "" {
						ui.Text(c, f.err).FontSize(fontSecondary).TextColor(t.Danger)
					}
				})
				ui.Row(c).Gap(gapSmall).Justify(ui.End).Children(func() {
					button(c, buttonSpec{label: locales.Cancel(), style: softButton(t), disabled: f.busy}, func() { f.editorOpen = false })
					button(c, buttonSpec{label: locales.SaveChanges(), style: primaryButton(t), busy: f.busy, disabled: f.busy}, d.submitFakeIPEditor)
				})
			})
		})
	})
}
