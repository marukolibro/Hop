package handler

import (
	"fmt"
	"runtime"

	"github.com/egoist/mygo"

	"hop/internal/service"
)

const (
	shortcutShowWindow  = "CmdOrCtrl+Alt+H"
	shortcutToggleProxy = "CmdOrCtrl+Alt+P"
	shortcutCycleMode   = "CmdOrCtrl+Alt+M"
)

// shortcutDisplayLabels formats the three global shortcuts using the current
// platform's modifier symbols.
func shortcutDisplayLabels() (show, proxy, mode string) {
	if runtime.GOOS == "darwin" {
		return "⌘⌥H", "⌘⌥P", "⌘⌥M"
	}
	return "Ctrl+Alt+H", "Ctrl+Alt+P", "Ctrl+Alt+M"
}

// SetToggleWindow installs the callback that shows or hides the main window
// when the global shortcut fires.
func (d *Dashboard) SetToggleWindow(fn func()) {
	d.toggleWindow = fn
}

// applyGlobalShortcuts registers or unregisters Hop's system-wide shortcuts.
// Existing registrations are always cleared first so toggling never leaks.
func (d *Dashboard) applyGlobalShortcuts(enabled bool) error {
	mygo.GlobalShortcut.UnregisterAll()
	if !enabled {
		return nil
	}
	runUI := func(fn func()) {
		if d.opWin != nil {
			d.opWin.Update(fn)
		}
	}
	bindings := []struct {
		acc  string
		desc string
		fn   func()
	}{
		{
			acc:  shortcutShowWindow,
			desc: "Show or hide Hop window",
			fn: func() {
				if d.toggleWindow != nil {
					d.toggleWindow()
				}
			},
		},
		{
			acc:  shortcutToggleProxy,
			desc: "Toggle Hop system proxy",
			fn: func() {
				runUI(func() {
					if d.sysProxy.loaded && !d.sysProxy.busy {
						d.beginSystemProxy(!d.sysProxy.on)
					}
				})
			},
		},
		{
			acc:  shortcutCycleMode,
			desc: "Cycle Hop outbound mode",
			fn: func() {
				runUI(func() {
					if d.setMode == nil || d.controlsLocked() || d.modeLocked() {
						return
					}
					next := service.Modes[(d.modeIndex+1)%len(service.Modes)]
					d.setMode(next)
				})
			},
		},
	}
	for _, b := range bindings {
		fn := b.fn
		if err := mygo.GlobalShortcut.RegisterWith(b.acc, mygo.GlobalShortcutOptions{Description: b.desc}, fn); err != nil {
			mygo.GlobalShortcut.UnregisterAll()
			return fmt.Errorf("register %s: %w", b.acc, err)
		}
	}
	return nil
}

// setGlobalShortcuts enables or disables system-wide hotkeys and persists the
// choice when registration succeeds.
func (d *Dashboard) setGlobalShortcuts(enabled bool) {
	if err := d.applyGlobalShortcuts(enabled); err != nil {
		d.fail(actionGlobalShortcuts, err)
		return
	}
	next := d.settings.settings
	next.GlobalShortcuts = enabled
	d.saveSettings(next)
}
