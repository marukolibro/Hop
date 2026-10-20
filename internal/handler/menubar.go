package handler

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"hop/internal/model"
)

// idleRate is the throughput below which the menu bar shows no speed and
// the route shows no packets.
const idleRate = 1024

// MenuBar is Hop's menu bar item: its mark with the live speed beside it,
// and a panel to see the route and switch the mode and the node without
// opening the window. Closing the window hides it and Hop's Dock icon; Hop
// keeps running in the menu bar until quit. All its methods run on the main
// thread.
type MenuBar struct {
	tray *mygo.Tray
	win  *mygo.Window
	d    *Dashboard
	// panel is the open panel, or nil.
	panel    *mygo.Window
	quitting bool
	// mark is the plain icon; shown is the speed the icon shows now, ""
	// for the plain mark.
	mark  []byte
	shown string
}

// NewMenuBar puts Hop in the menu bar and keeps it running when win is
// closed. Call it before d.Start, so the menu bar hears the traffic, and
// start d with the menu bar's Updater, so the panel follows every change.
func NewMenuBar(win *mygo.Window, d *Dashboard) (*MenuBar, error) {
	icon, err := trayIcon()
	if err != nil {
		return nil, err
	}
	tray, err := mygo.NewTray(mygo.TrayOptions{Icon: icon, IconIsTemplate: true, ToolTip: "Hop"})
	if err != nil {
		return nil, fmt.Errorf("create menu bar item: %w", err)
	}
	m := &MenuBar{tray: tray, win: win, d: d, mark: icon}
	tray.OnClick(m.togglePanel)
	tray.OnRightClick(m.popUpMenu)
	d.OnTraffic(m.showSpeed)
	win.OnClose(func(e *mygo.CloseEvent) {
		if !m.quitting {
			e.PreventDefault()
			m.hideWindow()
		}
	})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { m.quitting = true })
	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows {
			m.showWindow()
		}
	})
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) { tray.Destroy() })
	return m, nil
}

// Update runs fn on the main thread and redraws the window and, when it is
// open, the panel: both show the dashboard.
func (m *MenuBar) Update(fn func()) {
	m.win.Update(func() {
		fn()
		if m.panel != nil {
			m.panel.Invalidate()
		}
	})
}

// showWindow brings the window back, and Hop's Dock icon with it.
func (m *MenuBar) showWindow() {
	if m.panel != nil {
		m.panel.Close()
	}
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyRegular)
	m.win.Show()
	m.win.Focus()
}

// hideWindow puts the window away; Hop keeps running in the menu bar only,
// without a Dock icon, until the window is shown again.
func (m *MenuBar) hideWindow() {
	m.win.Hide()
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
}

// togglePanel opens the panel under the icon, or closes it. The panel
// closes by itself as the user clicks elsewhere or presses Escape.
func (m *MenuBar) togglePanel() {
	if m.panel != nil {
		m.panel.Close()
		return
	}
	m.panel = mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:      m.tray,
		Placement: mygo.PlacementBottom,
		Gap:       4,
		Width:     flyoutWidth,
		Height:    flyoutHeight,
		Focusable: true,
		Shadow:    true,
		Popover:   true,
		Content: ui.View(func(c *ui.Context) {
			m.d.flyoutView(c, m.showWindow, mygo.App.Quit)
		}),
	})
	m.panel.OnClosed(func() { m.panel = nil })
}

// popUpMenu is the right click's plain menu, for when the panel is not
// wanted.
func (m *MenuBar) popUpMenu() {
	if m.panel != nil {
		m.panel.Close()
	}
	m.tray.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: "打开 Hop", Click: func(*mygo.MenuItem, *mygo.Window) { m.showWindow() }},
		mygo.Separator(),
		{Label: "退出 Hop", Role: mygo.RoleQuit},
	}))
	defer m.tray.SetMenu(nil)
	m.tray.PopUpMenu()
}

// showSpeed draws the current speed beside the mark while traffic flows,
// upload over download in a column of fixed width, so the item does not
// shift as the numbers change. It redraws only when the numbers do.
func (m *MenuBar) showSpeed(sample model.Traffic) {
	if sample.Down+sample.Up < idleRate {
		m.setIcon("", m.mark)
		return
	}
	up, down := compactRate(sample.Up), compactRate(sample.Down)
	if up+"/"+down == m.shown {
		return
	}
	icon, err := traySpeedIcon(up, down)
	if err != nil {
		slog.Warn("draw menu bar speed", "error", err)
		return
	}
	m.setIcon(up+"/"+down, icon)
}

// setIcon shows icon in the menu bar as what shown names.
func (m *MenuBar) setIcon(shown string, icon []byte) {
	if shown == m.shown {
		return
	}
	if err := m.tray.SetIcon(icon, true); err != nil {
		slog.Warn("set menu bar icon", "error", err)
		return
	}
	m.shown = shown
}

// compactRate is a speed short enough for the menu bar, such as "2.4M".
func compactRate(bytesPerSecond int64) string {
	r := formatBytes(bytesPerSecond)
	return r.value + strings.TrimSuffix(r.unit, "B")
}
