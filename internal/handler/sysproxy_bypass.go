package handler

import (
	"strings"
	"unicode"

	"github.com/egoist/mygo/ui"

	"hop/locales"
)

// proxyBypassState holds the user's extra system proxy bypass entries and
// their edit dialog.
type proxyBypassState struct {
	entries    []string
	busy       bool
	editorOpen bool
	text       string
	err        string
}

// proxyBypassFromText parses the entries the user typed: one per line, or
// separated by commas, semicolons, or whitespace.
func proxyBypassFromText(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == ';' || unicode.IsSpace(r) })
}

// openProxyBypassEditor shows the dialog filled with the stored extra bypass
// entries, one per line.
func (d *Dashboard) openProxyBypassEditor() {
	b := &d.proxyBypass
	if b.busy || d.controlsLocked() || d.services.SystemProxy == nil {
		return
	}
	b.entries = d.services.SystemProxy.BypassList()
	b.text, b.err, b.editorOpen = strings.Join(b.entries, "\n"), "", true
}

// submitProxyBypassEditor saves the edited bypass entries in the background
// and re-applies them to the system proxy when Hop currently owns it.
func (d *Dashboard) submitProxyBypassEditor() {
	b := &d.proxyBypass
	if b.busy || d.controlsLocked() || d.services.SystemProxy == nil {
		return
	}
	b.busy, b.err = true, ""
	entries := proxyBypassFromText(b.text)
	ctx, win := d.opCtx, d.opWin
	go func() {
		err := d.services.SystemProxy.SaveBypass(ctx, entries)
		win.Update(func() {
			b.busy = false
			b.entries = d.services.SystemProxy.BypassList()
			if err != nil {
				if b.editorOpen {
					b.err = err.Error()
					return
				}
				d.fail(actionSaveProxyBypass, err)
				return
			}
			b.editorOpen = false
		})
	}()
}

// proxyBypassRow is the settings row under Network showing how many custom
// bypass entries are configured and opening the editor.
func (d *Dashboard) proxyBypassRow(c *ui.Context, t *ui.Theme) func() {
	return func() {
		b := &d.proxyBypass
		settingRow(c, t, locales.ProxyBypassTitle(), locales.ProxyBypassNote(), func() {
			ui.Row(c).Gap(gapMedium).AlignItems(ui.Center).Children(func() {
				summary := locales.ProxyBypassDefaultOnly()
				if len(b.entries) > 0 {
					summary = locales.ProxyBypassCount(len(b.entries))
				}
				mutedText(c, t, summary)
				button(c, buttonSpec{label: locales.ProxyBypassEdit(), style: mainSoftButton(t), disabled: d.controlsLocked()}, d.openProxyBypassEditor)
			})
		})
	}
}

// proxyBypassDialog is the edit modal for the user's extra system proxy bypass
// entries.
func (d *Dashboard) proxyBypassDialog(c *ui.Context, t *ui.Theme) {
	b := &d.proxyBypass
	animatedDialog(c, "proxy-bypass", &b.editorOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(dialogScrim)
		panel.Width(dialogWidth).Padding(pagePadding).Gap(gapLarge).Radius(radiusPanel).
			Background(overlaySurface(t)).Border(1, t.Border).Shadow(0, 10, 30, 0, dialogShadow)
		panel.Children(func() {
			ui.Column(c).Gap(gapSection).Children(func() {
				ui.Column(c).Gap(gapSmall).Children(func() {
					ui.Row(c).Gap(gapMedium).AlignItems(ui.Center).Children(func() {
						ui.Text(c, locales.ProxyBypassTitle()).FontSize(fontHeadline).FontWeight(weightSemibold).Grow(1)
						iconButton(c, locales.Close(), iconClose, softButton(t), func() { b.editorOpen = false })
					})
					mutedText(c, t, locales.ProxyBypassDialogNote())
				})
				ui.Column(c).Gap(gapSmall).Children(func() {
					ui.TextArea(c, &b.text).Label(locales.ProxyBypassTitle()).Placeholder(locales.ProxyBypassPlaceholder()).
						Height(localEditorHeight).Background(dialogField(t)).Disabled(b.busy)
					if b.err != "" {
						ui.Text(c, b.err).FontSize(fontSecondary).TextColor(t.Danger)
					}
				})
				ui.Row(c).Gap(gapSmall).Justify(ui.End).Children(func() {
					button(c, buttonSpec{label: locales.Cancel(), style: softButton(t), disabled: b.busy}, func() { b.editorOpen = false })
					button(c, buttonSpec{label: locales.SaveChanges(), style: primaryButton(t), busy: b.busy, disabled: d.controlsLocked()}, d.submitProxyBypassEditor)
				})
			})
		})
	})
}
