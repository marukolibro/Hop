// Package handler is Hop's presentation layer: it turns user input into
// service calls and service results into the native user interface.
package handler

import (
	"context"
	"log/slog"
	"slices"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
	"hop/locales"
)

const (
	historyLimit = 120
	logLimit     = 1000
	// minPeak keeps the sparkline readable while the link is idle.
	minPeak = float32(1024)

	// headerHeight is the header's row, which is also the window's title
	// bar; main centres the traffic lights on it.
	headerHeight = 68

	statusConnecting = "connecting"
	statusConnected  = "connected"
	statusFailed     = "failed"

	testingAll = "*"

	// testRefreshEvery is how often the node view reloads during a delay test.
	testRefreshEvery = 300 * time.Millisecond
)

// Pages of the main window, in the order the switch shows them.
const (
	pageNodes = iota
	pageConnections
	pageStats
	pageLogs
	pageSettings
)

func pageLabels() []string {
	return []string{locales.PageNodes(), locales.PageConnections(), locales.PageStats(), locales.PageLogs(), locales.PageSettings()}
}

// modeLabels name service.Modes, index for index.
func modeLabels() []string {
	return []string{locales.ModeRule(), locales.ModeGlobal(), locales.RouteDirect()}
}

// ProxyService is what the dashboard needs to show and switch nodes.
type ProxyService interface {
	View(ctx context.Context) (model.ProxyView, error)
	Select(ctx context.Context, group string, proxy string) error
	TestDelay(ctx context.Context, group string, timeout time.Duration) error
	TestAutoGroups(ctx context.Context, timeout time.Duration) error
	WatchTraffic(ctx context.Context, handle func(model.Traffic)) error
}

// ExitIPService reads the public address the default route leaves by.
type ExitIPService interface {
	Lookup(ctx context.Context) (model.ExitIP, error)
}

// LANService lets other devices on the network use the proxy port.
type LANService interface {
	Status() (model.LANStatus, error)
	Apply(ctx context.Context, enabled bool, requireAccount bool) error
	ResetAccount(ctx context.Context) error
}

// PortService changes the port Hop's proxy listens on.
type PortService interface {
	Current() int
	// Skipped is the port the user chose that could not be used at start.
	Skipped() int
	Change(ctx context.Context, port int) error
}

// UnlockService finds which services work through which nodes.
type UnlockService interface {
	Detect(ctx context.Context, report func(service.UnlockReport)) error
}

// SubscriptionService is what the dashboard needs to manage saved profiles:
// subscriptions kept whole, one of them in use.
type SubscriptionService interface {
	List(ctx context.Context) (model.SubscriptionsView, error)
	Source(ctx context.Context, id string) (model.SubscriptionInput, error)
	Add(ctx context.Context, input model.SubscriptionInput) error
	Edit(ctx context.Context, id string, input model.SubscriptionInput) error
	Use(ctx context.Context, id string) error
	Update(ctx context.Context, id string) error
	UpdateAll(ctx context.Context) error
	// AutoUpdate refreshes what the stored times say is due; interval 0
	// turns it off.
	AutoUpdate(ctx context.Context, interval time.Duration) (bool, error)
	Delete(ctx context.Context, id string, replacementID string) error
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

// TrafficService records how much traffic Hop carries and reports it.
type TrafficService interface {
	Record(conns model.Connections, source model.TrafficSource, now time.Time)
	Summary(now time.Time) model.TrafficView
}

// CoreService reports which mihomo core runs and upgrades it.
type CoreService interface {
	Info(ctx context.Context) (model.CoreInfo, error)
	CheckLatest(ctx context.Context) (model.CoreRelease, bool, error)
	Upgrade(ctx context.Context, progress func(model.CoreProgress)) error
	RestoreBundled(ctx context.Context) error
}

// SettingsService keeps the user's settings.
type SettingsService interface {
	Load() (model.Settings, error)
	Save(settings model.Settings) error
}

// GeodataService keeps the rule databases current.
type GeodataService interface {
	Status() (model.GeodataStatus, error)
	Update(ctx context.Context) error
	// Due says whether an automatic update is due, before the streams are
	// ended for the restart it causes.
	Due() (bool, error)
	// AutoUpdate updates the databases when the stored last success is old
	// enough; it reports whether one was applied.
	AutoUpdate(ctx context.Context) (bool, error)
}

// CustomRulesService keeps the rules the user puts ahead of every profile's.
type CustomRulesService interface {
	List() ([]model.CustomRule, error)
	Save(ctx context.Context, rules []model.CustomRule) error
}

// AppUpdateService checks for and installs a newer Hop through mygo's signed
// updater.
type AppUpdateService interface {
	Enabled() bool
	DisabledReason() string
	Due() (bool, error)
	Check(ctx context.Context) (model.AppUpdate, error)
	Install(ctx context.Context, progress func(downloaded int64, total int64)) error
}

// SystemProxyService points the system proxy at Hop and back, never over
// another app's.
type SystemProxyService interface {
	Status(ctx context.Context) (model.SystemProxyStatus, error)
	Enable(ctx context.Context) error
	Disable(ctx context.Context) error
}

// TunService turns TUN mode on and off through the privileged helper, and
// removes the helper when the user asks.
type TunService interface {
	Status(ctx context.Context) (model.TunStatus, error)
	Enable(ctx context.Context) error
	Disable(ctx context.Context) error
	Uninstall(ctx context.Context) error
}

// LoginItem starts Hop when the user logs in: mygo.App in production.
type LoginItem interface {
	OpenAtLogin() bool
	SetOpenAtLogin(open bool) error
}

// Services are the business services the dashboard drives.
type Services struct {
	Proxies       ProxyService
	Subscriptions SubscriptionService
	Modes         ModeService
	Monitor       MonitorService
	Traffic       TrafficService
	Core          CoreService
	Settings      SettingsService
	Geodata       GeodataService
	Rules         CustomRulesService
	AppUpdate     AppUpdateService
	SystemProxy   SystemProxyService
	Tun           TunService
	LoginItem     LoginItem
	ExitIP        ExitIPService
	Unlock        UnlockService
	LAN           LANService
	Port          PortService
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

// subscriptionEditor is the state of the add/edit dialog.
type subscriptionEditor struct {
	// seq orders opens, so a Source reply from an older open is dropped.
	seq int
	// editingID is the subscription being edited, "" when adding.
	editingID string
	// intent is what the dialog was opened for, which sets its title and
	// description.
	intent editorIntent
	// original is the input as stored; sourceLoaded is set once Source answered,
	// or at once when adding.
	original     model.SubscriptionInput
	sourceLoaded bool
	// sourceIndex is the chosen source, an index of service.ProfileSources.
	// Editing keeps the stored one: a profile cannot change source.
	sourceIndex int
	// name and url are the fields being edited; rulesIndex is the rule
	// source chosen, an index of service.RuleSources.
	name       string
	url        string
	rulesIndex int
	// content holds a pasted config or the text read from a file; fileName is
	// the base name of the file it came from, shown only while the dialog is
	// open and never stored. Editing a local profile leaves content empty to
	// keep what is stored.
	content  string
	fileName string
	// fileBusy is set while the file picker or the read runs; busy is set
	// while Add or Edit runs; err is the form's last error.
	fileBusy bool
	busy     bool
	err      string
}

// deleteDialogState is the state of the delete confirmation.
type deleteDialogState struct {
	// seq orders opens; a completion from an older open turns into a notice.
	seq int
	// id is the subscription to delete and name what the copy calls it.
	id   string
	name string
	// replacement is the successor chosen when the subscription in use is
	// deleted and others remain.
	replacement string
	// busy is set while Delete runs; err is the form's last error.
	busy bool
	err  string
}

// subscriptionsState is the saved collection, the entry in use and the dialogs
// and busy marks of managing them.
type subscriptionsState struct {
	// view is the latest collection; loaded is set once List answered, so an
	// empty list reads as empty, not unread.
	view   model.SubscriptionsView
	loaded bool
	// managing shows the management page instead of the nodes page.
	managing bool
	// activeChoice is what the bar's picker holds, set back to the view's
	// ActiveID after every reload.
	activeChoice string
	// busyIDs are the subscriptions with an operation running; listBusy marks
	// the refresh of all of them; mutations counts running mutations, at most
	// one at a time, whose applies the mode switch waits for.
	busyIDs   map[string]struct{}
	listBusy  bool
	mutations int
	// editorOpen and deleteOpen show the dialogs; editor and deleting hold
	// their fields between frames.
	editorOpen bool
	editor     subscriptionEditor
	deleteOpen bool
	deleting   deleteDialogState
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
	// proxyHost is the address Hop's proxy listens on; the port is the
	// service's, since the user can change it.
	proxyHost string
	// appVersion is Hop's own version, as the settings row shows it.
	appVersion string

	page         int
	status       string
	failure      *failure
	subscription subscriptionsState
	core         coreState
	settings     settingsState
	geodata      geodataState
	rules        customRulesState
	appUpdate    appUpdateState
	sysProxy     sysProxyState
	tun          tunState
	lan          lanUIState
	port         portUIState
	// autoBusy is set while the background check refreshes what is due; it
	// locks the controls like any other mutation.
	autoBusy bool
	// quickTestedAt is when opening the menu bar panel last tested its
	// routes.
	quickTestedAt time.Time
	// openAtLogin is whether Hop starts at login, as the system says.
	openAtLogin bool
	// watchCancel ends the streams from the core, which restart with it.
	watchCancel context.CancelFunc
	view        model.ProxyView
	// modeIndex is the index in service.Modes the mode switch shows.
	modeIndex int
	// modeBusy is set while the outbound mode is being applied, so subscription
	// mutations wait: an apply right after a mode change could read a stale mode
	// and undo the user's choice.
	modeBusy bool
	// viewGen is the core's runtime epoch: work that changes or reads the
	// runtime takes the token on the UI thread as it launches, and replies from
	// an older epoch are dropped.
	viewGen int
	// coreEpoch counts core starts and restarts, so the same resolved exit
	// is asked for its IP again after the core comes back.
	coreEpoch int
	// exitIP is the default route's public address, as the last lookup found
	// it, and the lookup's state.
	exitIP exitIPState
	// markRun is the logo's run, played when the route's exit changes;
	// hopExit is the exit it last saw, and onHop hears each change.
	markRun markRun
	hopExit string
	onHop   func()
	// unlock is what the unlock check found for each node, and its run.
	unlock unlockState
	// delayCancel is UI-thread-owned and cancels the previous epoch's delay
	// probes.
	delayCancel context.CancelFunc
	// activeGroup is the group whose nodes the grid shows.
	activeGroup string
	// testing is the group whose delays are being measured, testingAll
	// during the test at startup, "" for none.
	testing string
	// delaysBeforeTest are the node delays when the running test began; the
	// delay order ranks by them until it ends.
	delaysBeforeTest map[string]model.Delay
	// nodeOrder is how the node grid orders its tiles, one of the nodeOrder
	// constants; it is not kept between starts.
	nodeOrder int
	// nodeRegion is the filter over the node grid, one of the nodeFilter
	// values; it is not kept between starts.
	nodeRegion  string
	last        model.Traffic
	history     model.TrafficHistory
	connections connectionsState
	logs        logsState
	// stats is what the statistics page shows, refreshed with every
	// connection sample.
	stats model.TrafficView
	// coreRestarts carries what the runner's supervision did after the core
	// left on its own; it is a channel because it is fed from the
	// supervisor's goroutine, unlike everything else here.
	coreRestarts chan model.CoreRestart

	// onTraffic, when set, hears every traffic sample on the UI thread.
	onTraffic func(model.Traffic)

	// opCtx/opWin are the context and updater Start got, for late starters such
	// as dialog submits.
	opCtx context.Context
	opWin Updater

	selectProxy             func(group string, proxy string)
	setMode                 func(mode string)
	testDelay               func(group string)
	useSubscription         func(id string)
	refreshSubscription     func(id string)
	refreshAllSubscriptions func()
	// openEditor opens the editor for an intent, "" id to add.
	openEditor      func(intent editorIntent, id string)
	submitEditor    func()
	openDelete      func(id string)
	submitDelete    func()
	closeConnection func(id string)
	closeAll        func()
}

// NewDashboard returns a dashboard that has not started loading yet, on the
// settings saved from the last run.
func NewDashboard(services Services, proxyHost string, settings model.Settings, appVersion string) *Dashboard {
	d := &Dashboard{
		services:     services,
		proxyHost:    proxyHost,
		appVersion:   appVersion,
		settings:     newSettingsState(settings),
		status:       statusConnecting,
		coreRestarts: make(chan model.CoreRestart, 8),
	}
	d.subscription.busyIDs = map[string]struct{}{}
	d.port.input = strconv.Itoa(services.Port.Current())
	if services.AppUpdate != nil {
		d.appUpdate.enabled = services.AppUpdate.Enabled()
		d.appUpdate.disabledReason = services.AppUpdate.DisabledReason()
	}
	d.logs.list.FollowEnd = true
	d.connections.list.Key = func(row int) any { return d.visibleConnections()[row].ID }
	d.stats = services.Traffic.Summary(time.Now())
	return d
}

// OnHop makes fn hear every run of the logo, on the UI thread. Call it before
// Start.
func (d *Dashboard) OnHop(fn func()) {
	d.onHop = fn
}

// noticeExit runs the logo when the route's exit changes to another exit. The
// first exit after a start, and one that returns after the route was empty,
// are not changes. It runs on the UI thread.
func (d *Dashboard) noticeExit() {
	exit := d.exitIPTarget().exit
	previous := d.hopExit
	d.hopExit = exit
	if previous == "" || exit == "" || exit == previous {
		return
	}
	d.markRun = markRun{started: time.Now()}
	if d.onHop != nil {
		d.onHop()
	}
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
	d.opCtx, d.opWin = ctx, win
	d.selectProxy = func(group string, proxy string) {
		if d.controlsLocked() {
			return
		}
		go d.choose(ctx, win, group, proxy, d.nextViewGen())
	}
	d.setMode = func(mode string) {
		// A mode change and a subscription apply cannot run together, or the
		// reload could carry the mode the user just left back in.
		if d.controlsLocked() {
			return
		}
		d.modeBusy = true
		go d.switchMode(ctx, win, mode, d.nextViewGen())
	}
	d.testDelay = func(group string) {
		if d.controlsLocked() {
			return
		}
		gen := d.nextViewGen()
		testCtx, cancel := context.WithCancel(ctx)
		d.delayCancel = cancel
		d.startTesting(group)
		timeout := service.DelayTimeout(d.settings.settings)
		// The probe belongs to the runtime epoch and window context.
		go func() {
			defer cancel()
			stop := d.refreshWhileTesting(testCtx, win, gen)
			err := d.services.Proxies.TestDelay(testCtx, group, timeout)
			stop()
			d.finishDelayTest(testCtx, win, gen, err)
		}()
	}
	d.useSubscription = func(id string) {
		if d.controlsLocked() || id == d.subscription.view.ActiveID {
			return
		}
		d.beginSubOp(subOp{
			a: actionUseSubscription, id: id,
			// A failed switch may have left the runtime mid-way; reload either
			// way.
			reloadAfter: true, reloadOnError: true,
			run: func(opCtx context.Context) error { return d.services.Subscriptions.Use(opCtx, id) },
			finish: func(err error) {
				if err != nil {
					d.fail(actionUseSubscription, err)
				}
			},
		})
	}
	d.refreshSubscription = func(id string) {
		if d.controlsLocked() {
			return
		}
		isActive := id == d.subscription.view.ActiveID
		d.beginSubOp(subOp{
			a: actionRefreshSubscription, id: id,
			// Read actual runtime after an apply, persistence or rollback
			// failure.
			reloadAfter: isActive, reloadOnError: isActive,
			run: func(opCtx context.Context) error { return d.services.Subscriptions.Update(opCtx, id) },
			finish: func(err error) {
				if err != nil {
					d.fail(actionRefreshSubscription, err)
				}
			},
		})
	}
	d.refreshAllSubscriptions = func() {
		refreshable := slices.ContainsFunc(d.subscription.view.Items, func(item model.SubscriptionSummary) bool {
			return item.Source == model.SourceRemote
		})
		if d.controlsLocked() || d.subscription.listBusy || !refreshable {
			return
		}
		hasActive := d.subscription.view.ActiveID != ""
		d.beginSubOp(subOp{
			a: actionRefreshSubscription, list: true,
			// A partial failure may still have refreshed the entry in use;
			// reload then too.
			reloadAfter: hasActive, reloadOnError: hasActive,
			run: func(opCtx context.Context) error { return d.services.Subscriptions.UpdateAll(opCtx) },
			finish: func(err error) {
				if err != nil {
					d.fail(actionRefreshSubscription, err)
				}
			},
		})
	}
	d.openEditor = func(intent editorIntent, id string) {
		if seq := d.openEditorDialog(intent, id); id != "" {
			go d.loadSource(ctx, win, id, seq)
		}
	}
	d.submitEditor = func() { d.beginEditorSubmit() }
	d.openDelete = func(id string) {
		for _, item := range d.subscription.view.Items {
			if item.ID == id {
				s := &d.subscription
				s.deleting = deleteDialogState{seq: s.deleting.seq + 1, id: item.ID, name: item.Name}
				s.deleteOpen = true
				return
			}
		}
	}
	d.submitDelete = func() { d.beginDeleteSubmit() }
	d.closeConnection = func(id string) { go d.runClose(ctx, win, id) }
	d.closeAll = func() { go d.runCloseAll(ctx, win) }
	go d.loadSubscriptions(ctx, win)
	go d.loadRules(ctx, win)
	go d.loadGeodata(ctx, win)
	go d.startupTest(ctx, win, d.viewGen)
	go d.loadCore(ctx, win)
	go d.startSystemProxy(ctx, win, d.settings.settings.SystemProxy)
	go d.startTun(ctx, win)
	go d.readLAN(win)
	go d.watchNetState(ctx, win)
	d.openAtLogin = d.services.LoginItem.OpenAtLogin()
	d.startWatchers(ctx, win)
	go d.watchCoreRestarts(ctx, win)
	go d.runUpdateScheduler(ctx, win)
}

// nextViewGen starts work that publishes a new view and supersedes older views
// and test indicators.
func (d *Dashboard) nextViewGen() int {
	d.viewGen++
	if d.delayCancel != nil {
		d.delayCancel()
		d.delayCancel = nil
	}
	d.stopTesting()
	return d.viewGen
}

// startTesting marks group as being measured and keeps the delays the nodes
// show now for ranking.
func (d *Dashboard) startTesting(group string) {
	d.testing = group
	d.delaysBeforeTest = nodeDelays(d.view)
}

func (d *Dashboard) stopTesting() {
	d.testing = ""
	d.delaysBeforeTest = nil
}

// loadView reads the node view; a reply from an epoch older than the current one
// is dropped, so state from an old profile cannot cover the new one.
func (d *Dashboard) loadView(ctx context.Context, win Updater, gen int) {
	view, err := d.services.Proxies.View(ctx)
	win.Update(func() {
		if gen != d.viewGen {
			return
		}
		if err != nil {
			d.failConnection(actionLoadGroups, err)
			return
		}
		d.status = statusConnected
		d.view = view
		d.modeIndex = modeIndexOf(view.Mode)
		d.activeGroup = keepActiveGroup(view, d.activeGroup)
		d.noticeExit()
		d.considerExitIP()
	})
}

// startupTest loads the profile and starts background probes without holding
// subscription completion.
func (d *Dashboard) startupTest(ctx context.Context, win Updater, gen int) {
	// A new core epoch is taken before the view so its apply asks for the
	// exit IP again, even when the same node is still selected.
	win.Update(func() {
		if gen != d.viewGen || ctx.Err() != nil {
			return
		}
		d.coreEpoch++
		d.considerExitIP()
	})
	d.loadView(ctx, win, gen)
	win.Update(func() {
		if gen != d.viewGen || ctx.Err() != nil {
			return
		}
		testCtx, cancel := context.WithCancel(ctx)
		d.delayCancel = cancel
		d.startTesting(testingAll)
		timeout := service.DelayTimeout(d.settings.settings)
		// The probes belong to the runtime epoch and window context.
		go func() {
			defer cancel()
			stop := d.refreshWhileTesting(testCtx, win, gen)
			err := d.services.Proxies.TestAutoGroups(testCtx, timeout)
			stop()
			d.finishDelayTest(testCtx, win, gen, err)
		}()
	})
}

// refreshWhileTesting reloads the node view at a steady pace while a delay
// test runs, so each node shows its latency as soon as it answers instead of
// all of them at the end. The returned function ends it and waits until its
// last reload is in, so none arrives after the test's own final one.
func (d *Dashboard) refreshWhileTesting(ctx context.Context, win Updater, gen int) (stop func()) {
	done := make(chan struct{})
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		ticker := time.NewTicker(testRefreshEvery)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				d.loadView(ctx, win, gen)
			}
		}
	}()
	return func() {
		close(done)
		<-exited
	}
}

// finishDelayTest publishes only current probe results; intentional cancellation
// is not a failure.
func (d *Dashboard) finishDelayTest(ctx context.Context, win Updater, gen int, err error) {
	if ctx.Err() != nil {
		return
	}
	d.loadView(ctx, win, gen)
	win.Update(func() {
		if gen != d.viewGen {
			return
		}
		d.delayCancel = nil
		d.stopTesting()
		if err != nil {
			d.fail(actionTestLatency, err)
		}
	})
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
		win.Update(func() {
			d.connections.data = conns
			d.recordTraffic(conns, time.Now())
		})
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

func (d *Dashboard) choose(ctx context.Context, win Updater, group string, proxy string, gen int) {
	if err := d.services.Proxies.Select(ctx, group, proxy); err != nil {
		win.Update(func() { d.fail(actionSwitchNode, err) })
	}
	// Reload either way, so the view shows what the core really uses.
	d.loadView(ctx, win, gen)
}

// switchMode applies the outbound mode and reloads the view; modeBusy clears
// whatever the epoch, as it must.
func (d *Dashboard) switchMode(ctx context.Context, win Updater, mode string, gen int) {
	err := d.services.Modes.Set(ctx, mode)
	win.Update(func() {
		d.modeBusy = false
		if err != nil {
			d.fail(actionSwitchMode, err)
		}
	})
	d.loadView(ctx, win, gen)
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

// action names what the user or the dashboard was doing: id is for the log,
// and title is worded for the user as the title of its failure.
type action struct {
	id    string
	title func() string
}

var (
	actionReadSubscription    = action{"read-subscription", locales.ActionReadSubscription}
	actionLoadSource          = action{"load-source", locales.ActionLoadSource}
	actionLoadGroups          = action{"load-groups", locales.ActionLoadGroups}
	actionWatchTraffic        = action{"watch-traffic", locales.ActionWatchTraffic}
	actionWatchConnections    = action{"watch-connections", locales.ActionWatchConnections}
	actionWatchLogs           = action{"watch-logs", locales.ActionWatchLogs}
	actionSwitchNode          = action{"switch-node", locales.ActionSwitchNode}
	actionSwitchMode          = action{"switch-mode", locales.ActionSwitchMode}
	actionTestLatency         = action{"test-latency", locales.ActionTestLatency}
	actionCheckUnlock         = action{"check-unlock", locales.ActionCheckUnlock}
	actionLAN                 = action{"l-a-n", locales.ActionLan}
	actionPort                = action{"port", locales.ActionPort}
	actionUseSubscription     = action{"use-subscription", locales.ActionUseSubscription}
	actionAddSubscription     = action{"add-subscription", locales.ActionAddSubscription}
	actionEditSubscription    = action{"edit-subscription", locales.ActionEditSubscription}
	actionDeleteSubscription  = action{"delete-subscription", locales.ActionDeleteSubscription}
	actionRefreshSubscription = action{"refresh-subscription", locales.ActionRefreshSubscription}
	actionCloseConnection     = action{"close-connection", locales.ActionCloseConnection}
	actionReadCore            = action{"read-core", locales.ActionReadCore}
	actionCheckCore           = action{"check-core", locales.ActionCheckCore}
	actionUpgradeCore         = action{"upgrade-core", locales.ActionUpgradeCore}
	actionRestoreCore         = action{"restore-core", locales.ActionRestoreCore}
	actionCoreRestarted       = action{"core-restarted", locales.ActionCoreRestarted}
	actionCoreGaveUp          = action{"core-gave-up", locales.ActionCoreGaveUp}
	actionSaveSettings        = action{"save-settings", locales.ActionSaveSettings}
	actionReadRules           = action{"read-rules", locales.ActionReadRules}
	actionSaveRules           = action{"save-rules", locales.ActionSaveRules}
	actionReadGeodata         = action{"read-geodata", locales.ActionReadGeodata}
	actionUpdateGeodata       = action{"update-geodata", locales.ActionUpdateGeodata}
	actionCheckAppUpdate      = action{"check-app-update", locales.ActionCheckAppUpdate}
	actionInstallAppUpdate    = action{"install-app-update", locales.ActionInstallAppUpdate}
	actionReadSystemProxy     = action{"read-system-proxy", locales.ActionReadSystemProxy}
	actionSystemProxyOn       = action{"system-proxy-on", locales.ActionSystemProxyOn}
	actionSystemProxyOff      = action{"system-proxy-off", locales.ActionSystemProxyOff}
	actionOpenAtLogin         = action{"open-at-login", locales.ActionOpenAtLogin}
	actionReadTun             = action{"read-tun", locales.ActionReadTun}
	actionTunOn               = action{"tun-on", locales.ActionTunOn}
	actionTunOff              = action{"tun-off", locales.ActionTunOff}
	actionUninstallHelper     = action{"uninstall-helper", locales.ActionUninstallHelper}
)

// fail shows what went wrong without touching the connection status.
func (d *Dashboard) fail(a action, err error) {
	slog.Error("dashboard action failed", "action", a.id, "error", err)
	d.failure = &failure{title: a.title(), detail: err.Error()}
}

// failConnection is fail for errors that mean the core cannot be reached.
func (d *Dashboard) failConnection(a action, err error) {
	d.fail(a, err)
	d.status = statusFailed
}

// View builds one frame of the main window.
func (d *Dashboard) View(c *ui.Context) {
	c.SetTheme(d.windowTheme(c))
	t := c.Theme()
	// The material paints the themed backdrop and veil before any controls.
	c.Root().Background(ui.Transparent)
	root := ui.Column(c).Fill()
	if onGlass(t) {
		root.Material(themedWindowMaterial{palette: materialPalette(t.Dark, d.settings.settings.Theme), veil: glassBaseColor(t)})
	} else {
		root.Background(t.Background)
	}
	root.Children(func() {
		d.header(c, t)
		d.notice(c, t)
		// One surface per page, keyed by the page, so landing on another page is a
		// new element that enters.
		ui.Column(c).Key("page-" + strconv.Itoa(d.page)).Grow(1).Basis(0).MinHeight(0).Transition(pageTransition).Children(func() {
			switch d.page {
			case pageConnections:
				d.connectionsPage(c, t)
			case pageStats:
				d.statsPage(c, t)
			case pageLogs:
				d.logsPage(c, t)
			case pageSettings:
				d.settingsPage(c, t)
			default:
				d.nodesPage(c, t)
			}
		})
	})
	// The subscription and settings dialogs sit over whatever page shows.
	d.editorDialog(c, t)
	d.deleteDialog(c, t)
	d.tunDialog(c, t)
	d.customRuleDialog(c, t)
}

// header holds what applies to every page: the page switch, and how traffic
// reaches Hop with whether it does now.
func (d *Dashboard) header(c *ui.Context, t *ui.Theme) {
	// The window has no title bar of its own: the header is it. It keeps
	// clear of the native window controls on either side and drags the window.
	left := max(pagePadding, c.TitleBar().Left+gapSmall)
	right := max(pagePadding, c.TitleBar().Right+gapSmall)
	width, _ := c.Size()
	controls := c.TitleBar()
	headerGap := gapLarge
	if width >= 1050 {
		headerGap = 30
	}
	ui.Column(c).Background(headerFill(t)).Children(func() {
		if controls.Right > 0 && width-controls.Right < headerMinContentWidth {
			ui.Box(c).Height(controls.Height).DragWindow()
			right = pagePadding
		}
		ui.Row(c).Gap(headerGap).AlignItems(ui.Center).Height(headerHeight).Padding(0, right, 0, left).DragWindow().Children(func() {
			if width >= 760 {
				brandLockup(c, t, 28, d.markRun)
			} else {
				brandMark(c, t, 28, d.markRun)
			}
			pageTabs(c, t, locales.PageSwitchLabel(), &d.page, pageLabels(), func(int) {})
			ui.Box(c).Grow(1)
			// The two switches belong together: a tight gap of their own, not the
			// header's wide one between the brand, the tabs and this group.
			ui.Row(c).Gap(gapSmall).AlignItems(ui.Center).Children(func() {
				d.trafficSwitches(c, t)
			})
		})
		horizontalHairline(c, t)
	})
}

// proxying says whether traffic is handed to Hop now: its core runs and the
// system proxy points at it or its TUN has taken the routes. That, not the
// core merely running, is what the status dot shows as live.
func (d *Dashboard) proxying() bool {
	return d.status == statusConnected && (d.sysProxy.on || d.tun.on)
}

func (d *Dashboard) statusText() string {
	switch {
	case d.proxying():
		return locales.StatusProxying()
	case d.status == statusConnected:
		return locales.StatusProxyOff()
	case d.status == statusFailed:
		return locales.StatusNotConnected()
	default:
		return locales.StatusConnecting()
	}
}

// statusColor is green only while Hop carries the system's traffic, red
// when the core cannot be reached, and quiet otherwise.
func (d *Dashboard) statusColor(t *ui.Theme) ui.Color {
	switch {
	case d.proxying():
		return t.Success
	case d.status == statusFailed:
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
	fill := t.Danger.Alpha(0.1)
	if onGlass(t) {
		// The failure is read over the page; give it a panel face so its
		// text never meets the desktop.
		fill = glassPanel(t).Mix(t.Danger, glassNoticeDanger)
	}
	ui.Row(c).Gap(gapMedium).AlignItems(ui.Center).Padding(10, pagePadding).Background(fill).Children(func() {
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, d.failure.title).FontSize(fontBody).FontWeight(weightSemibold).TextColor(t.Danger)
			mutedText(c, t, d.failure.detail)
		})
		iconButton(c, locales.DismissNotice(), iconClose, dangerButton(t), func() { d.failure = nil })
	})
	horizontalHairline(c, t)
}
