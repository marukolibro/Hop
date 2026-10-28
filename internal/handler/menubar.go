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

// shownHopping stands for the icon while the ball hops, so the first rate
// drawn after it is never taken for what is already there.
const shownHopping = "hopping"

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
	// mark is the plain icon; shown is the speed the icon shows now, ""
	// for the plain mark.
	mark  []byte
	shown string
	// arcs are the mark's arcs alone, drawn once; each frame of a hop and
	// each rate is the mark built over them.
	arcs *image.NRGBA
	// speed is the rate column beside the mark, nil while the speed is off.
	speed *image.RGBA
	// sample is the latest traffic, drawn when a hop ends.
	sample model.Traffic
	// hopping says the ball is hopping in the icon.
	hopping bool
}

// NewMenuBar puts Hop in the menu bar and keeps it running when win is
// closed. Call it before d.Start, so the menu bar hears the traffic, and
// start d with the menu bar's Updater, so the panel follows every change.
func NewMenuBar(win *mygo.Window, d *Dashboard) (*MenuBar, error) {
	arcs := arcsImage()
	icon, err := encodeTrayIcon(markImage(arcs, 0))
	if err != nil {
		return nil, err
	}
	tray, err := mygo.NewTray(mygo.TrayOptions{Icon: icon, IconIsTemplate: true, ToolTip: "Hop"})
	if err != nil {
		return nil, fmt.Errorf("create menu bar item: %w", err)
	}
	m := &MenuBar{tray: tray, win: win, d: d, mark: icon, arcs: arcs}
	tray.OnClick(m.togglePanel)
	tray.OnRightClick(m.popUpMenu)
	d.OnTraffic(m.showSpeed)
	d.OnHop(m.hop)
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
		Vibrancy:     flyoutVibrancy(),
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
// macOS, which Windows 11 shows as Acrylic. Elsewhere the panel draws its
// own background.
func flyoutVibrancy() mygo.Vibrancy {
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

// drawRest draws the mark at rest with the current speed beside it, upload
// over download in a column of fixed width, so the item does not shift as the
// numbers change; idle, it reads 0K rather than dropping the speed, so the
// item keeps one width. With the speed turned off in the settings it shows
// the plain mark. It redraws only when the numbers do.
func (m *MenuBar) drawRest() {
	if !m.d.settings.settings.MenuBarSpeed {
		m.speed = nil
		m.setIcon("", m.mark)
		return
	}
	up, down := compactRate(m.sample.Up), compactRate(m.sample.Down)
	if up+"/"+down == m.shown {
		return
	}
	speed := traySpeedColumn(up, down)
	icon, err := traySpeedIcon(markImage(m.arcs, 0), speed)
	if err != nil {
		slog.Warn("draw menu bar speed", "error", err)
		return
	}
	m.speed = speed
	m.setIcon(up+"/"+down, icon)
}

// hop plays the ball's hop, one frame after another on the main thread, and
// draws the rest again when it lands.
func (m *MenuBar) hop() {
	if m.hopping || m.quitting {
		return
	}
	m.hopping = true
	m.shown = shownHopping
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
	icon, err := m.hopIcon(hopLift(float64(step) / trayHopFrames))
	if err == nil {
		err = m.tray.SetIcon(icon, true)
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
func (m *MenuBar) hopIcon(lift float64) ([]byte, error) {
	mark := markImage(m.arcs, lift)
	if m.speed == nil {
		return encodeTrayIcon(mark)
	}
	return traySpeedIcon(mark, m.speed)
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
