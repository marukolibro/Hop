package handler

import (
	"fmt"
	"image"
	"log/slog"
	"math"
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
	"hop/locales"
)

// idleRate is the throughput below which the menu bar shows 0K and the
// route shows no packets.
const idleRate = 1024

// reopenGuard is how long after the panel closed the icon's click is taken
// as the end of the press that closed it: a press outside the panel closes
// it, and its release lands on the icon, which would open it again.
const reopenGuard = 500 * time.Millisecond

// flyoutCornerRadius rounds the panel's material and shadow.
const flyoutCornerRadius = 14

// hopFrameTime is how long each frame of the ball's hop shows.
const hopFrameTime = 30 * time.Millisecond

// trayShownKey identifies the rendered menu bar image so identical frames
// skip PNG encoding and SetIcon calls.
type trayShownKey struct {
	state   trayState
	dark    bool
	speed   string
	hopping bool
}

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
	panel *mygo.Window
	// panelHeight is the height the panel last fitted its content to, so
	// the next one opens at it; 0 before any panel opened.
	panelHeight int
	closedAt    time.Time
	quitting    bool
	// shown tracks what the menu bar icon currently displays; shownSet is
	// false before the first draw.
	shown    trayShownKey
	shownSet bool
	tooltip  string
	// arcsMask is the tray mark's two arcs precomputed as an alpha mask once;
	// each state and hop frame colors over it in microseconds.
	arcsMask *image.Alpha
	// speed is the rate column beside the mark, nil while the speed is off;
	// speedPainter draws it.
	speed        *image.RGBA
	speedPainter *traySpeedPainter
	// sample is the latest traffic, drawn when a hop ends.
	sample model.Traffic
	// hopping says the ball is hopping in the icon.
	hopping bool
}

// NewMenuBar puts Hop in the menu bar and keeps it running when win is
// closed. Call it before d.Start, so the menu bar hears the traffic, and
// start d with the menu bar's Updater, so the panel follows every change.
func NewMenuBar(win *mygo.Window, d *Dashboard) (*MenuBar, error) {
	arcsMask := trayArcsMask()
	initialState := d.trayState()
	initialVis := trayVisualFor(initialState, false)
	icon, err := encodeTrayIcon(trayMarkImage(arcsMask, initialState, false, 0))
	if err != nil {
		return nil, err
	}
	tray, err := mygo.NewTray(mygo.TrayOptions{Icon: icon, IconIsTemplate: initialVis.template, ToolTip: "Hop"})
	if err != nil {
		return nil, fmt.Errorf("create menu bar item: %w", err)
	}
	m := &MenuBar{
		tray:         tray,
		win:          win,
		d:            d,
		arcsMask:     arcsMask,
		speedPainter: newTraySpeedPainter(),
	}
	// Sync once immediately using tray.Dark() so dark menu bars start with the
	// right palette and tooltip.
	m.drawRest()
	tray.OnClick(m.togglePanel)
	tray.OnRightClick(m.popUpMenu)
	offAppearance := tray.OnAppearanceChange(func() {
		mygo.RunOnMain(func() {
			if !m.quitting {
				m.syncTray()
			}
		})
	})
	d.OnTraffic(m.showSpeed)
	d.OnHop(m.hop)
	d.OnTrayChange(m.syncTray)
	d.SetToggleWindow(m.toggleWindow)
	if d.settings.settings.GlobalShortcuts {
		if err := d.applyGlobalShortcuts(true); err != nil {
			slog.Warn("could not register saved global shortcuts at startup", "error", err)
		}
	}
	win.OnClose(func(e *mygo.CloseEvent) {
		if !m.quitting {
			e.PreventDefault()
			m.hideWindow()
		}
	})
	// The services are checked as the window opens and while it is in front.
	win.OnShow(func() { m.Update(func() { d.setForeground(true, true) }) })
	win.OnFocus(func() { m.Update(func() { d.setForeground(true, false) }) })
	win.OnBlur(func() { m.Update(func() { d.setForeground(false, false) }) })
	win.OnHide(func() { m.Update(func() { d.setForeground(false, false) }) })
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { m.quitting = true })
	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows {
			m.showWindow()
		}
	})
	offResume := mygo.Power.OnResume(func() { m.Update(d.HandlePowerResume) })
	offNet := mygo.Network.OnChanged(func(s mygo.NetworkStatus) { m.Update(func() { d.HandleNetworkChange(s) }) })
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		offAppearance()
		offResume()
		offNet()
		d.stopNetworkRecovery()
		mygo.GlobalShortcut.UnregisterAll()
		tray.Destroy()
	})
	return m, nil
}

// toggleWindow hides the main window when it is currently focused, or brings
// it to the front otherwise.
func (m *MenuBar) toggleWindow() {
	if m.d.foreground {
		m.hideWindow()
		return
	}
	m.showWindow()
}

// HandleOpenURL processes a deep-link URL delivered by the OS (such as
// clash://install-config?url=...&name=...), bringing the main window forward
// and pre-filling the add-subscription dialog for user confirmation.
func (m *MenuBar) HandleOpenURL(raw string) {
	sub, ok, err := service.ParseInstallConfigURL(raw)
	if !ok {
		return
	}
	m.showWindow()
	m.Update(func() {
		if err != nil {
			m.d.fail(actionOpenURL, err)
			return
		}
		slog.Info("opening subscription import from deep link", "host", service.RedactDeepLinkHost(sub.URL))
		m.d.openEditorWithImport(sub.Name, sub.URL)
	})
}

// Update runs fn on the main thread and redraws the window and, when it is
// open, the panel: both show the dashboard. It also keeps the menu bar icon in
// step with proxy, TUN, core, and appearance changes.
func (m *MenuBar) Update(fn func()) {
	m.win.Update(func() {
		fn()
		m.syncTray()
		if m.panel != nil {
			m.panel.Invalidate()
		}
	})
}

// syncTray refreshes the menu bar icon and tooltip whenever the running state,
// menu bar appearance, or speed preference changed.
func (m *MenuBar) syncTray() {
	if m.quitting || m.hopping {
		return
	}
	m.drawRest()
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
	if time.Since(m.closedAt) < reopenGuard {
		return
	}
	if m.panelHeight == 0 {
		m.panelHeight = flyoutHeight
	}
	m.panel = mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:      m.tray,
		Placement: mygo.PlacementBottom,
		Gap:       4,
		Width:     flyoutWidth,
		Height:    m.panelHeight,
		Focusable: true,
		Shadow:    true,
		// The panel is its own window, not an NSPopover: its material
		// follows the app's appearance, as the palette drawn over it does,
		// where a popover takes the menu bar's.
		Vibrancy:     flyoutVibrancy(m.d.settings.settings.Theme),
		CornerRadius: flyoutCornerRadius,
		Content: ui.View(func(c *ui.Context) {
			m.d.flyoutView(c, m.showWindow, mygo.App.Quit, m.fitPanel)
		}),
	})
	m.panel.OnClosed(func() {
		m.panel = nil
		m.closedAt = time.Now()
	})
	m.d.testQuickRoutes()
}

// fitPanel makes the open panel height tall, as its content asks. It runs on
// the UI thread while a frame is built, so the resize goes to a goroutine,
// which ends once the main thread applied it, after the frame.
func (m *MenuBar) fitPanel(height int) {
	if m.panel == nil || height == m.panelHeight {
		return
	}
	m.panelHeight = height
	panel := m.panel
	go panel.SetContentSize(flyoutWidth, height)
}

// flyoutVibrancy is the material behind the panel: the popover material on
// macOS, which Windows 11 shows as Acrylic. Elsewhere, and under the
// cyberpunk theme, the panel draws its own background.
func flyoutVibrancy(theme model.ColorTheme) mygo.Vibrancy {
	if theme == model.ThemeHack {
		// One opaque look on every platform.
		return mygo.VibrancyNone
	}
	switch runtime.GOOS {
	case "darwin", "windows":
		return mygo.VibrancyPopover
	}
	return mygo.VibrancyNone
}

// popUpMenu is the right click's plain menu, for when the panel is not
// wanted.
func (m *MenuBar) popUpMenu() {
	if m.panel != nil {
		m.panel.Close()
	}
	m.tray.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: locales.OpenHop(), Click: func(*mygo.MenuItem, *mygo.Window) { m.showWindow() }},
		mygo.Separator(),
		{Label: locales.QuitHop(), Role: mygo.RoleQuit},
	}))
	defer m.tray.SetMenu(nil)
	m.tray.PopUpMenu()
}

// showSpeed keeps the latest traffic and, unless the ball is hopping, draws it.
func (m *MenuBar) showSpeed(sample model.Traffic) {
	m.sample = sample
	if m.hopping {
		return
	}
	m.drawRest()
}

// drawRest draws the mark at rest in the current running state with optional
// speed beside it, upload over download in a column of fixed width, so the
// item does not shift as the numbers change; idle, it reads 0K rather than
// dropping the speed, so the item keeps one width. With the speed turned off
// in the settings it shows only the mark. It redraws only when state,
// appearance, or displayed speed changes.
func (m *MenuBar) drawRest() {
	state := m.d.trayState()
	dark := m.tray.Dark()
	vis := trayVisualFor(state, dark)
	m.updateTooltip(state)

	if !m.d.settings.settings.MenuBarSpeed {
		key := trayShownKey{state: state, dark: dark}
		if m.shownSet && m.shown == key {
			return
		}
		icon, err := encodeTrayIcon(trayMarkImage(m.arcsMask, state, dark, 0))
		if err != nil {
			slog.Warn("draw menu bar icon", "error", err)
			return
		}
		m.speed = nil
		m.setIcon(key, icon, vis.template)
		return
	}

	up, down := compactRate(m.sample.Up), compactRate(m.sample.Down)
	key := trayShownKey{state: state, dark: dark, speed: up + "/" + down}
	if m.shownSet && m.shown == key {
		return
	}
	speed := m.speedPainter.draw(up, down, vis.speedInk)
	icon, err := traySpeedIcon(trayMarkImage(m.arcsMask, state, dark, 0), speed)
	if err != nil {
		slog.Warn("draw menu bar speed", "error", err)
		return
	}
	m.speed = speed
	m.setIcon(key, icon, vis.template)
}

// updateTooltip names the running state in the menu bar item's tooltip,
// setting it only when the words change.
func (m *MenuBar) updateTooltip(state trayState) {
	tip := locales.TrayTooltip(m.d.statusText())
	if state == trayStateTun {
		tip = locales.TrayTooltipTun()
	}
	if tip == m.tooltip {
		return
	}
	m.tray.SetToolTip(tip)
	m.tooltip = tip
}

// hop plays the ball's hop, one frame after another on the main thread, and
// draws the rest again when it lands.
func (m *MenuBar) hop() {
	if m.hopping || m.quitting {
		return
	}
	m.hopping = true
	m.shown = trayShownKey{hopping: true}
	m.shownSet = true
	m.hopFrame(1)
}

// hopFrame draws the ball at its height in frame step of the hop and queues
// the next; after the last it lands.
func (m *MenuBar) hopFrame(step int) {
	if m.quitting {
		return
	}
	if step >= trayHopFrames {
		m.hopping = false
		m.drawRest()
		return
	}
	state := m.d.trayState()
	dark := m.tray.Dark()
	vis := trayVisualFor(state, dark)
	icon, err := m.hopIcon(state, dark, hopLift(float64(step)/trayHopFrames))
	if err == nil {
		err = m.tray.SetIcon(icon, vis.template)
	}
	if err != nil {
		slog.Warn("draw menu bar hop", "error", err)
		m.hopping = false
		m.drawRest()
		return
	}
	time.AfterFunc(hopFrameTime, func() { mygo.RunOnMain(func() { m.hopFrame(step + 1) }) })
}

// hopIcon is the icon with the ball raised by lift mark units, and the speed
// beside it when the item shows one.
func (m *MenuBar) hopIcon(state trayState, dark bool, lift float64) ([]byte, error) {
	mark := trayMarkImage(m.arcsMask, state, dark, lift)
	if m.speed == nil {
		return encodeTrayIcon(mark)
	}
	return traySpeedIcon(mark, m.speed)
}

// setIcon shows icon in the menu bar and records its rendered key.
func (m *MenuBar) setIcon(key trayShownKey, icon []byte, template bool) {
	if m.shownSet && key == m.shown {
		return
	}
	if err := m.tray.SetIcon(icon, template); err != nil {
		slog.Warn("set menu bar icon", "error", err)
		return
	}
	m.shown = key
	m.shownSet = true
}

// compactRate is a speed short enough for the menu bar, four characters at
// most: one decimal below 10, such as "2.4M", whole numbers from 10, such
// as "36K", and the next unit once the number would round to 1000. Below
// idleRate it is "0K", so a trickle never shows as bare bytes.
func compactRate(bytesPerSecond int64) string {
	if bytesPerSecond < idleRate {
		return "0K"
	}
	value := float64(bytesPerSecond) / 1024
	units := []string{"K", "M", "G", "T"}
	for _, unit := range units[:len(units)-1] {
		if math.Round(value) < 1000 {
			return compactNumber(value) + unit
		}
		value /= 1024
	}
	return compactNumber(value) + units[len(units)-1]
}

// compactNumber is value with one decimal below 10 and none from 10.
func compactNumber(value float64) string {
	if value < 9.95 {
		return fmt.Sprintf("%.1f", value)
	}
	return fmt.Sprintf("%.0f", value)
}
