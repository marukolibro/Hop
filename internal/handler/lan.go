package handler

import (
	"context"
	"log/slog"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/locales"
)

// lanUIState is what the LAN rows show: the access as the service reports
// it, read again with the network so the addresses follow it.
type lanUIState struct {
	status model.LANStatus
	loaded bool
	busy   bool
}

// applyLANStatus shows status, or logs why it could not be read: the next
// read tries again.
func (d *Dashboard) applyLANStatus(status model.LANStatus, err error) {
	if err != nil {
		slog.Warn("could not read the LAN access", "error", err)
		return
	}
	d.lan.status, d.lan.loaded = status, true
}

// readLAN reads the LAN access once; the network watch keeps it current.
// Goroutine ownership: started by Start, it ends after one read.
func (d *Dashboard) readLAN(win Updater) {
	if d.services.LAN == nil {
		return
	}
	status, err := d.services.LAN.Status()
	win.Update(func() { d.applyLANStatus(status, err) })
}

// beginLAN makes the access enabled, with an account asked for or not, in the
// background, and keeps the choice for the next start once it took.
func (d *Dashboard) beginLAN(enabled bool, requireAccount bool) {
	l := &d.lan
	if l.busy || d.controlsLocked() || d.services.LAN == nil {
		return
	}
	l.busy = true
	ctx, win := d.opCtx, d.opWin
	go func() {
		err := d.services.LAN.Apply(ctx, enabled, requireAccount)
		status, statusErr := d.services.LAN.Status()
		win.Update(func() {
			l.busy = false
			d.applyLANStatus(status, statusErr)
			if err != nil {
				d.fail(actionLAN, err)
				return
			}
			next := d.settings.settings
			next.AllowLAN, next.LANNoPassword = enabled, !requireAccount
			d.saveSettings(next)
			d.reloadViewAfterCore(ctx, win)
		})
	}()
}

// beginLANReset gives the account a new password, in the background.
func (d *Dashboard) beginLANReset() {
	l := &d.lan
	if l.busy || d.controlsLocked() || d.services.LAN == nil {
		return
	}
	l.busy = true
	ctx, win := d.opCtx, d.opWin
	go func() {
		err := d.services.LAN.ResetAccount(ctx)
		status, statusErr := d.services.LAN.Status()
		win.Update(func() {
			l.busy = false
			d.applyLANStatus(status, statusErr)
			if err != nil {
				d.fail(actionLAN, err)
				return
			}
			d.reloadViewAfterCore(ctx, win)
		})
	}()
}

// reloadViewAfterCore reads the node view again after the core reloaded its
// config. It runs on the UI thread.
func (d *Dashboard) reloadViewAfterCore(ctx context.Context, win Updater) {
	gen := d.nextViewGen()
	go d.loadView(ctx, win, gen)
}

// lanRows are the settings rows of the LAN access: the switch, and while it is
// on, whether an account is asked for, the address devices use and the
// account.
func (d *Dashboard) lanRows(c *ui.Context, t *ui.Theme) []func() {
	l := &d.lan
	locked := !l.loaded || l.busy || d.controlsLocked()
	asksAccount := l.status.Account != (model.LANCredential{})
	rows := []func(){func() {
		settingRow(c, t, locales.LanAllow(), locales.LanIntro(), func() {
			on := l.status.Enabled
			ui.Box(c).Disabled(locked).Children(func() {
				ui.Switch(c.Key("lan"), &on).Label(locales.LanAllow()).OnChange(func() {
					d.beginLAN(!l.status.Enabled, !d.settings.settings.LANNoPassword)
				})
			})
		})
	}}
	if !l.status.Enabled {
		return rows
	}
	rows = append(rows,
		func() {
			note := locales.LanAccountOn()
			if !asksAccount {
				note = locales.LanAccountOff()
			}
			settingRow(c, t, locales.LanRequireAccount(), note, func() {
				on := asksAccount
				ui.Box(c).Disabled(locked).Children(func() {
					ui.Switch(c.Key("lan-account"), &on).Label(locales.LanRequireAccount()).OnChange(func() {
						d.beginLAN(true, !asksAccount)
					})
				})
			})
		},
	)
	if !asksAccount {
		rows = append(rows, func() {
			ui.Text(c, locales.LanOpenWarning()).FontSize(fontSecondary).FontWeight(weightMedium).TextColor(textOn(t, t.Warning))
		})
	}
	rows = append(rows, func() {
		settingRow(c, t, locales.LanAddress(), locales.LanProtocols(), func() { d.lanAddressList(c, t) })
	})
	if asksAccount {
		account := l.status.Account
		rows = append(rows, func() {
			settingRow(c, t, locales.LanAccount(), "", func() { d.lanCopyValue(c, t, locales.LanUsername(), account.Username) })
		}, func() {
			settingRow(c, t, locales.LanPassword(), "", func() {
				ui.Row(c).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
					d.lanCopyValue(c, t, locales.LanPassword(), account.Password)
					ui.Box(c).Disabled(locked).Children(func() {
						button(c, buttonSpec{label: locales.LanResetPassword(), icon: iconRefresh, style: mainSoftButton(t)}, d.beginLANReset)
					})
				})
			})
		})
	}
	rows = append(rows, func() { mutedText(c, t, locales.LanFirewallNote()) })
	return rows
}

// lanAddressList shows each address a device can use, with its copy button, or
// that this computer is on no network.
func (d *Dashboard) lanAddressList(c *ui.Context, t *ui.Theme) {
	addresses := d.lan.status.Addresses
	if len(addresses) == 0 {
		mutedText(c, t, locales.LanNoNetwork())
		return
	}
	ui.Column(c).Gap(gapSmall).AlignItems(ui.End).Children(func() {
		for _, address := range addresses {
			ui.Row(c).Key(address).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
				d.lanCopyValue(c, t, locales.LanAddress(), address)
			})
		}
	})
}

// lanCopyValue shows value in the monospace face, with a button that copies it.
func (d *Dashboard) lanCopyValue(c *ui.Context, t *ui.Theme, what string, value string) {
	ui.Row(c).Key(what + value).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
		ui.Text(c, value).Font("monospace").FontSize(fontSecondary).FontWeight(weightMedium).NoWrap()
		button(c, buttonSpec{label: locales.Copy(), style: mainSoftButton(t)}, func() {
			c.WriteClipboard(value)
			c.Toast(locales.Copied() + what)
		})
	})
}
