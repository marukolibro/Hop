package handler

import (
	"fmt"
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
// opening the window. Closing the window hides it; Hop keeps running until
// quit. All its methods run on the main thread.
type MenuBar struct {
	tray *mygo.Tray
	win  *mygo.Window
	d    *Dashboard
	// panel is the open panel, or nil.
	panel    *mygo.Window
	quitting bool
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
	m := &MenuBar{tray: tray, win: win, d: d}
	tray.OnClick(m.togglePanel)
	tray.OnRightClick(m.popUpMenu)
	d.OnTraffic(m.showSpeed)
	win.OnClose(func(e *mygo.CloseEvent) {
		if !m.quitting {
			e.PreventDefault()
			win.Hide()
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

func (m *MenuBar) showWindow() {
	if m.panel != nil {
		m.panel.Close()
	}
	m.win.Show()
	m.win.Focus()
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

// showSpeed puts the current speed beside the mark while traffic flows.
func (m *MenuBar) showSpeed(sample model.Traffic) {
	if sample.Down+sample.Up < idleRate {
		m.tray.SetTitle("")
		return
	}
	m.tray.SetTitle("↓" + compactRate(sample.Down) + " ↑" + compactRate(sample.Up))
}

// compactRate is a speed short enough for the menu bar, such as "2.4M".
func compactRate(bytesPerSecond int64) string {
	r := formatBytes(bytesPerSecond)
	return r.value + strings.TrimSuffix(r.unit, "B")
}
