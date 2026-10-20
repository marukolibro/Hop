// Package handler is Hop's presentation layer: it turns user input into
// service calls and service results into the native user interface.
package handler

import (
	"context"
	"log/slog"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
)

const (
	historyLimit = 120
	logLimit     = 1000
	// minPeak keeps the sparkline readable while the link is idle.
	minPeak = float32(1024)

	// headerHeight is the header's row, which is also the window's title
	// bar; main centres the traffic lights on it.
	headerHeight = 60

	statusConnecting = "connecting"
	statusConnected  = "connected"
	statusFailed     = "failed"

	testingAll = "*"
)

// Pages of the main window, in the order the switch shows them.
const (
	pageNodes = iota
	pageConnections
	pageLogs
)

var pageLabels = []string{"节点", "连接", "日志"}

// modeLabels name service.Modes, index for index.
var modeLabels = []string{"规则", "全局", "直连"}

// ProxyService is what the dashboard needs to show and switch nodes.
type ProxyService interface {
	View(ctx context.Context) (model.ProxyView, error)
	Select(ctx context.Context, group string, proxy string) error
	TestDelay(ctx context.Context, group string) error
	TestAutoGroups(ctx context.Context) error
	WatchTraffic(ctx context.Context, handle func(model.Traffic)) error
}

// SubscriptionService is what the dashboard needs to manage the profile.
type SubscriptionService interface {
	Import(ctx context.Context, link string) error
	Update(ctx context.Context) error
	Summary() (model.SubscriptionSummary, bool, error)
}

// ModeService switches the outbound mode.
type ModeService interface {
	Set(ctx context.Context, mode string) error
}

// MonitorService shows what the core is doing.
type MonitorService interface {
	WatchConnections(ctx context.Context, handle func(model.Connections)) error
	CloseConnection(ctx context.Context, id string) error
	CloseAllConnections(ctx context.Context) error
	WatchLogs(ctx context.Context, level string, handle func(model.LogEntry)) error
}

// Services are the business services the dashboard drives.
type Services struct {
	Proxies       ProxyService
	Subscriptions SubscriptionService
	Modes         ModeService
	Monitor       MonitorService
}

// Updater runs fn on the window's UI thread, as mygo.Window does.
type Updater interface {
	Update(fn func())
}

// failure is the last thing that went wrong, as the user reads it.
type failure struct {
	title  string
	detail string
}

// subscriptionState is what the subscription bar shows and edits.
type subscriptionState struct {
	// saved is set once a subscription is imported; summary describes it.
	saved   bool
	summary model.SubscriptionSummary
	// input is the link being typed.
	input string
	// editing shows the input even though a subscription exists.
	editing bool
	// busy is set while a download is running.
	busy bool
}

// connectionsState is what the connections page shows and edits.
type connectionsState struct {
	data   model.Connections
	list   ui.ListState
	filter string
}

// logsState is what the log page shows and edits.
type logsState struct {
	entries []model.LogEntry
	list    ui.ListState
	// level is the index of the level filter: all, warnings and up, errors.
	level int
}

// Dashboard is the main window's state. Everything but the fields set in
// Start is read and written on the UI thread only: background work hands
// its results over through Updater.Update.
type Dashboard struct {
	services Services
	// proxyAddress is where apps send traffic to use Hop, as host:port.
	proxyAddress string

	page         int
	status       string
	failure      *failure
	subscription subscriptionState
	view         model.ProxyView
	// modeIndex is the index in service.Modes the mode switch shows.
	modeIndex int
	// activeGroup is the group whose nodes the grid shows.
	activeGroup string
	// groupFilter narrows the sidebar to groups whose name contains it.
	groupFilter string
	// testing is the group whose delays are being measured, testingAll
	// during the test at startup, "" for none.
	testing     string
	last        model.Traffic
	history     model.TrafficHistory
	connections connectionsState
	logs        logsState

	// onTraffic, when set, hears every traffic sample on the UI thread.
	onTraffic func(model.Traffic)

	selectProxy        func(group string, proxy string)
	setMode            func(mode string)
	testDelay          func(group string)
	importSubscription func(link string)
	updateSubscription func()
	closeConnection    func(id string)
	closeAll           func()
}

// NewDashboard returns a dashboard that has not started loading yet.
func NewDashboard(services Services, proxyAddress string) *Dashboard {
	d := &Dashboard{services: services, proxyAddress: proxyAddress, status: statusConnecting}
	d.logs.list.FollowEnd = true
	d.connections.list.Key = func(row int) any { return d.visibleConnections()[row].ID }
	return d
}

// OnTraffic makes fn hear every traffic sample, on the UI thread. Call it
// before Start.
func (d *Dashboard) OnTraffic(fn func(model.Traffic)) {
	d.onTraffic = fn
}

// Start begins loading the profile and streaming traffic, connections and
// the log. The goroutines it starts belong to main: they end when ctx is
// cancelled.
func (d *Dashboard) Start(ctx context.Context, win Updater) {
	d.selectProxy = func(group string, proxy string) { go d.choose(ctx, win, group, proxy) }
	d.setMode = func(mode string) { go d.switchMode(ctx, win, mode) }
	d.testDelay = func(group string) { go d.runDelayTest(ctx, win, group) }
	d.importSubscription = func(link string) { go d.runImport(ctx, win, link) }
	d.updateSubscription = func() { go d.runUpdate(ctx, win) }
	d.closeConnection = func(id string) { go d.runClose(ctx, win, id) }
	d.closeAll = func() { go d.runCloseAll(ctx, win) }
	go d.loadSummary(win)
	go d.startupTest(ctx, win)
	go d.watchTraffic(ctx, win)
	go d.watchConnections(ctx, win)
	go d.watchLogs(ctx, win)
}

func (d *Dashboard) loadSummary(win Updater) {
	summary, saved, err := d.services.Subscriptions.Summary()
	win.Update(func() {
		if err != nil {
			d.fail(actionReadSubscription, err)
			return
		}
		d.subscription.saved, d.subscription.summary = saved, summary
	})
}

func (d *Dashboard) loadView(ctx context.Context, win Updater) {
	view, err := d.services.Proxies.View(ctx)
	win.Update(func() {
		if err != nil {
			d.failConnection(actionLoadGroups, err)
			return
		}
		d.status = statusConnected
		d.view = view
		d.modeIndex = modeIndexOf(view.Mode)
		d.activeGroup = keepActiveGroup(view, d.activeGroup)
	})
}

// startupTest shows the profile at once, then measures the groups that pick
// by delay so they leave the placeholder they start on.
func (d *Dashboard) startupTest(ctx context.Context, win Updater) {
	d.loadView(ctx, win)
	win.Update(func() { d.testing = testingAll })
	err := d.services.Proxies.TestAutoGroups(ctx)
	win.Update(func() {
		d.testing = ""
		if err != nil {
			d.fail(actionTestLatency, err)
		}
	})
	d.loadView(ctx, win)
}

func modeIndexOf(mode string) int {
	for i, m := range service.Modes {
		if m == mode {
			return i
		}
	}
	return 0
}

// keepActiveGroup keeps the group the user was looking at when it still
// exists, and starts at the view's first group otherwise.
func keepActiveGroup(view model.ProxyView, current string) string {
	for _, g := range view.Groups {
		if g.Name == current {
			return current
		}
	}
	if len(view.Groups) == 0 {
		return ""
	}
	return view.Groups[0].Name
}

func (d *Dashboard) watchTraffic(ctx context.Context, win Updater) {
	err := d.services.Proxies.WatchTraffic(ctx, func(sample model.Traffic) {
		win.Update(func() {
			d.last = sample
			d.history = service.PushSample(d.history, sample, historyLimit)
			if d.onTraffic != nil {
				d.onTraffic(sample)
			}
		})
	})
	if ctx.Err() != nil {
		return
	}
	win.Update(func() { d.failConnection(actionWatchTraffic, err) })
}

func (d *Dashboard) watchConnections(ctx context.Context, win Updater) {
	err := d.services.Monitor.WatchConnections(ctx, func(conns model.Connections) {
		win.Update(func() { d.connections.data = conns })
	})
	if ctx.Err() != nil {
		return
	}
	win.Update(func() { d.fail(actionWatchConnections, err) })
}

func (d *Dashboard) watchLogs(ctx context.Context, win Updater) {
	err := d.services.Monitor.WatchLogs(ctx, model.LogInfo, func(entry model.LogEntry) {
		win.Update(func() { d.logs.entries = service.AppendLog(d.logs.entries, entry, logLimit) })
	})
	if ctx.Err() != nil {
		return
	}
	win.Update(func() { d.fail(actionWatchLogs, err) })
}

func (d *Dashboard) choose(ctx context.Context, win Updater, group string, proxy string) {
	if err := d.services.Proxies.Select(ctx, group, proxy); err != nil {
		win.Update(func() { d.fail(actionSwitchNode, err) })
	}
	// Reload either way, so the view shows what the core really uses.
	d.loadView(ctx, win)
}

func (d *Dashboard) switchMode(ctx context.Context, win Updater, mode string) {
	if err := d.services.Modes.Set(ctx, mode); err != nil {
		win.Update(func() { d.fail(actionSwitchMode, err) })
	}
	d.loadView(ctx, win)
}

func (d *Dashboard) runDelayTest(ctx context.Context, win Updater, group string) {
	win.Update(func() { d.testing = group })
	err := d.services.Proxies.TestDelay(ctx, group)
	win.Update(func() {
		d.testing = ""
		if err != nil {
			d.fail(actionTestLatency, err)
		}
	})
	d.loadView(ctx, win)
}

func (d *Dashboard) runImport(ctx context.Context, win Updater, link string) {
	win.Update(func() { d.subscription.busy = true })
	err := d.services.Subscriptions.Import(ctx, link)
	d.finishSubscription(ctx, win, actionImport, err)
}

func (d *Dashboard) runUpdate(ctx context.Context, win Updater) {
	win.Update(func() { d.subscription.busy = true })
	err := d.services.Subscriptions.Update(ctx)
	d.finishSubscription(ctx, win, actionUpdate, err)
}

// finishSubscription reports the outcome of a download and, when it worked,
// shows the new profile.
func (d *Dashboard) finishSubscription(ctx context.Context, win Updater, a action, err error) {
	summary, saved, summaryErr := d.services.Subscriptions.Summary()
	win.Update(func() {
		d.subscription.busy = false
		if err != nil {
			d.fail(a, err)
			return
		}
		if summaryErr != nil {
			d.fail(actionReadSubscription, summaryErr)
			return
		}
		d.failure = nil
		d.subscription.saved, d.subscription.summary = saved, summary
		d.subscription.input = ""
		d.subscription.editing = false
	})
	if err == nil {
		d.startupTest(ctx, win)
	}
}

func (d *Dashboard) runClose(ctx context.Context, win Updater, id string) {
	if err := d.services.Monitor.CloseConnection(ctx, id); err != nil {
		win.Update(func() { d.fail(actionCloseConnection, err) })
	}
}

func (d *Dashboard) runCloseAll(ctx context.Context, win Updater) {
	if err := d.services.Monitor.CloseAllConnections(ctx); err != nil {
		win.Update(func() { d.fail(actionCloseConnection, err) })
	}
}

// action names what the user or the dashboard was doing, worded for the
// user as the title of its failure.
type action string

const (
	actionReadSubscription action = "读取已保存的订阅失败"
	actionLoadGroups       action = "无法读取节点列表"
	actionWatchTraffic     action = "与内核的连接已断开"
	actionWatchConnections action = "无法读取连接列表"
	actionWatchLogs        action = "无法读取内核日志"
	actionSwitchNode       action = "切换节点失败"
	actionSwitchMode       action = "切换出站模式失败"
	actionTestLatency      action = "测速失败"
	actionImport           action = "导入订阅失败"
	actionUpdate           action = "刷新订阅失败"
	actionCloseConnection  action = "断开连接失败"
)

// fail shows what went wrong without touching the connection status.
func (d *Dashboard) fail(a action, err error) {
	slog.Error("dashboard action failed", "action", string(a), "error", err)
	d.failure = &failure{title: string(a), detail: err.Error()}
}

// failConnection is fail for errors that mean the core cannot be reached.
func (d *Dashboard) failConnection(a action, err error) {
	d.fail(a, err)
	d.status = statusFailed
}

// View builds one frame of the main window.
func (d *Dashboard) View(c *ui.Context) {
	c.SetTheme(hopTheme(c.Theme().Dark))
	t := c.Theme()
	ui.Column(c).Fill().Children(func() {
		d.header(c, t)
		d.notice(c, t)
		switch d.page {
		case pageConnections:
			d.connectionsPage(c, t)
		case pageLogs:
			d.logsPage(c, t)
		default:
			d.nodesPage(c, t)
		}
	})
}

// header holds what applies to every page: the page switch, the outbound
// mode and whether the core is reachable.
func (d *Dashboard) header(c *ui.Context, t *ui.Theme) {
	// The window has no title bar of its own: the header is it. It keeps
	// clear of the traffic lights and drags the window.
	left := max(pagePadding, c.TitleBar().Left+gapSmall)
	ui.Row(c).Gap(gapLarge).AlignItems(ui.Center).Height(headerHeight).Padding(0, pagePadding, 0, left).DragWindow().Children(func() {
		brandLockup(c, t, 24)
		slidingSwitch(c, "页面", &d.page, pageLabels, pageSwitchStyle(t, 56), func(int) {})
		ui.Box(c).Grow(1)
		d.modeSwitch(c, t, 52)
		d.statusCapsule(c, t)
	})
}

// modeSwitch switches the outbound mode; it waits for the core to report
// its mode before it takes clicks.
func (d *Dashboard) modeSwitch(c *ui.Context, t *ui.Theme, segmentWidth float32) {
	ui.Box(c).Key("mode").Disabled(d.view.Mode == "").Children(func() {
		slidingSwitch(c, "出站模式", &d.modeIndex, modeLabels, modeSwitchStyle(t, segmentWidth), func(i int) {
			d.setMode(service.Modes[i])
		})
	})
}

func (d *Dashboard) statusText() string {
	switch d.status {
	case statusConnected:
		return "运行中"
	case statusFailed:
		return "未连接"
	default:
		return "连接中"
	}
}

func (d *Dashboard) statusColor(t *ui.Theme) ui.Color {
	switch d.status {
	case statusConnected:
		return t.Success
	case statusFailed:
		return t.Danger
	default:
		return t.TextMuted
	}
}

// notice shows the last failure in plain words first and the technical
// detail after.
func (d *Dashboard) notice(c *ui.Context, t *ui.Theme) {
	if d.failure == nil {
		return
	}
	ui.Row(c).Gap(gapMedium).AlignItems(ui.Center).Padding(10, 14).Margin(0, pagePadding, gapMedium, pagePadding).Radius(radiusControl).Background(t.Danger.Alpha(0.1)).Children(func() {
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, d.failure.title).FontSize(fontBody).FontWeight(weightSemibold).TextColor(t.Danger)
			mutedText(c, t, d.failure.detail)
		})
		iconButton(c, "关闭提示", iconClose, dangerButton(t), func() { d.failure = nil })
	})
}
