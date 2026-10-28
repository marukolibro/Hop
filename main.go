package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"hop/config"
	"hop/internal/handler"
	"hop/internal/model"
	"hop/internal/repository/corerelease"
	"hop/internal/repository/corestore"
	"hop/internal/repository/exitip"
	"hop/internal/repository/geodata"
	"hop/internal/repository/helper"
	"hop/internal/repository/lan"
	"hop/internal/repository/logfile"
	"hop/internal/repository/mihomo"
	"hop/internal/repository/profile"
	"hop/internal/repository/routetable"
	"hop/internal/repository/subscription"
	"hop/internal/repository/sysproxy"
	"hop/internal/repository/unlock"
	"hop/internal/service"
	"hop/locales"
)

const (
	controllerTimeout = 5 * time.Second
	downloadTimeout   = 30 * time.Second
	// validationTimeout bounds one config check: the first may have to download
	// geodata.
	validationTimeout = 30 * time.Second
	// geodataDownloadTimeout bounds the two rule databases, which are a few
	// megabytes through Hop's own proxy.
	geodataDownloadTimeout = 2 * time.Minute
	// quitReleaseTimeout bounds clearing the system proxy and stopping the
	// helper's core at quit.
	quitReleaseTimeout = 10 * time.Second
	// helperCallTimeout bounds one request to the privileged helper; a
	// start waits for the core's controller, which can take seconds.
	helperCallTimeout = 20 * time.Second
	// geodataHelperTimeout bounds giving the helper new geodata, which
	// restarts its core once the ports are free again.
	geodataHelperTimeout = 90 * time.Second
	// logFileName, logFileBytes and logFileCount keep the log to a few
	// megabytes: the newest file and two older ones.
	logFileName          = "hop.log"
	logFileBytes   int64 = 2 << 20
	logFileCount         = 3
	bindAddress          = "127.0.0.1"
	coreLogLevel         = "info"
	bootstrapGroup       = "Hop"

	// windowMaterial is the native material behind the main window on
	// macOS. Change it here to try another.
	windowMaterial = mygo.VibrancyUnderWindow
	// windowsMaterial is the system backdrop behind the main window on
	// Windows 11. Mica only tints with the wallpaper and costs the GPU
	// nothing; the wallpaper bitmap and veil sit above it.
	windowsMaterial = mygo.VibrancyMica

	// trafficLightX and trafficLightY place the close button's top-left
	// corner so the traffic lights centre on the header's 60 DIP row.
	trafficLightX = 20
	trafficLightY = 23
)

// startupError is a failure before the window could open. Nothing else
// would tell the user why Hop vanished, so main shows it in a dialog.
type startupError struct{ err error }

func (e startupError) Error() string { return e.err.Error() }
func (e startupError) Unwrap() error { return e.err }

func run() (err error) {
	// uiStarted is set once the app's run loop takes over; an error before
	// that is a startupError.
	uiStarted := false
	defer func() {
		if err != nil && !uiStarted {
			err = startupError{err: err}
		}
	}()
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("locate user config dir: %w", err)
	}
	resourcesDir, err := mygo.App.Path(mygo.PathResources)
	if err != nil {
		return fmt.Errorf("locate app resources: %w", err)
	}
	instance := runningInstance()
	cfg, err := config.Load(instance, userConfigDir, resourcesDir)
	if err != nil {
		return err
	}
	logs, err := logfile.Open(cfg.LogDir, logFileName, logFileBytes, logFileCount)
	if err != nil {
		return err
	}
	defer logs.Close()
	// Hop's own log goes to the file as well as the console. The core's goes
	// there too, warnings and errors only: the Logs page shows the rest live.
	slog.SetDefault(slog.New(slog.NewTextHandler(io.MultiWriter(os.Stderr, logs), nil)))
	cfg.CoreOutput = io.MultiWriter(os.Stderr, logfile.NewWarningsOnly(logs))
	// appGeodataDir is Hop's own copy of the geo databases: updates write
	// there, never into the app bundle, and the helper is seeded from it.
	appGeodataDir := filepath.Join(cfg.AppDir, "geodata")
	validateDir := filepath.Join(cfg.AppDir, "validate")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store, err := profile.NewStore(cfg.AppDir)
	if err != nil {
		return err
	}
	// The proxy port is the one the user chose, if any; everything below
	// reads it from portState, which the port service moves while Hop runs.
	ownPorts, err := cfg.OwnPorts()
	if err != nil {
		return err
	}
	mixedPort, err := service.StartupMixedPort(store, cfg.MixedPort, ownPorts)
	if err != nil {
		return err
	}
	cfg.MixedPort = mixedPort
	portState := service.NewMixedPortState(mixedPort)
	traffic, err := service.NewTrafficService(store)
	if err != nil {
		return err
	}
	traffic.Start(ctx)
	// The version in mygo.json, which mygo's updater compares with the
	// release and the settings row shows.
	appVersion := mygo.App.Version()
	mode, err := service.StartupMode(store)
	if err != nil {
		return err
	}
	settings := service.RuntimeSettings{
		MixedPort:          cfg.MixedPort,
		ControllerAddr:     cfg.ControllerAddr,
		ControllerSecret:   cfg.Secret,
		BindAddress:        bindAddress,
		LogLevel:           coreLogLevel,
		BootstrapGroupName: bootstrapGroup,
		Mode:               mode,
		NodeDNSListen:      cfg.NodeDNSAddr,
		ProbePort:          cfg.ProbePort,
	}
	client := mihomo.NewClient(cfg.ControllerAddr, cfg.Secret, mihomo.ConfigPath(cfg.HomeDir), &http.Client{}, controllerTimeout)
	helperClient, geodataHelper, helperInstaller := helperAccess(instance)
	tunFlag := service.NewTunFlag()
	lanState := service.NewLANState()
	profiles := service.NewSubscriptionService(
		subscription.NewFetcher(&http.Client{Timeout: downloadTimeout}),
		store,
		// The custom rules go ahead of every config the service builds.
		store,
		// Configs go to whichever core serves the controller: the helper's
		// while TUN is on, Hop's own otherwise.
		service.NewCoreApplier(client, helperClient, tunFlag),
		// Config checks borrow the core's own -t, in an isolated directory.
		mihomo.NewConfigValidator(cfg.MihomoBinary, validateDir, validationTimeout),
		client,
		settings,
		tunFlag,
		lanState,
		portState,
	)

	settingsService := service.NewSettingsService(store)
	userSettings, err := settingsService.Load()
	if err != nil {
		return err
	}
	mygo.Theme.SetSource(handler.AppearanceSource(userSettings.Appearance))
	// The LAN access the user left on is part of the first config the core
	// starts with.
	lanService := service.NewLANService(lanState, lan.NewCredentials(cfg.AppDir), lan.Source{}, profiles, portState)
	if err := lanService.Prepare(userSettings.AllowLAN, !userSettings.LANNoPassword); err != nil {
		return err
	}

	startupConfig, err := profiles.StartupConfig()
	if err != nil {
		return err
	}
	// The core would download geodata through the network on first use,
	// which is slow and can fail where GitHub cannot be reached; what ships
	// with the app is seeded in first, and only when missing. Hop keeps its
	// own copy under the app dir: updates write there, never into the app
	// bundle, and the helper is seeded from it too.
	// The config checks run the core in a directory of their own, and a
	// config with GEOSITE or GEOIP rules (Hop's rule template has both) makes
	// even -t load the databases: seed that directory too.
	if err := mihomo.SeedGeodata(filepath.Join(resourcesDir, "geodata"), appGeodataDir); err != nil {
		return err
	}
	for _, dir := range []string{cfg.HomeDir, validateDir} {
		if err := mihomo.SeedGeodata(appGeodataDir, dir); err != nil {
			return err
		}
	}
	runner := mihomo.NewRunner(ctx, cfg, client)

	coreService := service.NewCoreService(
		client,
		corerelease.NewSource(hopProxyClient(portState), corerelease.ReleaseAPI),
		corestore.New(cfg.AppDir),
		runner,
		mihomo.NewCoreChecker(mihomo.ConfigPath(cfg.HomeDir), validateDir, validationTimeout),
		cfg.MihomoBinary,
		cfg.BundledBinary,
		cfg.CoreOrigin,
		tunFlag,
	)

	appUpdateService := service.NewAppUpdateService()
	customRulesService := service.NewCustomRulesService(store, profiles)
	geodataService := service.NewGeodataService(
		geodata.NewSource(geodataDownloadClient(portState), geodata.DefaultBaseURL),
		mihomo.NewConfigValidator(cfg.MihomoBinary, validateDir, validationTimeout),
		profiles,
		store,
		geodataHelper,
		runner,
		tunFlag,
		profiles,
		appGeodataDir,
		cfg.HomeDir,
		validateDir,
	)
	systemProxy := service.NewSystemProxyService(sysproxy.New(), bindAddress, portState)
	helperTool, toolErr := helper.BundledToolPath(resourcesDir)
	if toolErr != nil {
		// The app runs without TUN; turning it on then fails, naming what
		// is missing.
		slog.Warn("this build ships no helper binary; TUN stays unavailable", "error", toolErr)
	}
	tun := service.NewTunService(
		helperClient,
		helperInstaller,
		routetable.Reader{},
		runner,
		profiles,
		mihomo.NewConfigValidator(cfg.MihomoBinary, validateDir, validationTimeout),
		cfg.MihomoBinary,
		appGeodataDir,
		helperTool,
		tunFlag,
		client,
	)
	// Apps must not be left sending traffic to a core that is gone. This runs
	// in OnQuit, which mygo fires before it starts a Relaunch: App.Run returns
	// only after finish, so the old core and proxy must be released here for
	// the new instance to find their ports free. The defer covers a Run that
	// returns without finish (and an early error after a helper core started).
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			releaseCtx, releaseDone := context.WithTimeout(context.Background(), quitReleaseTimeout)
			if err := systemProxy.Disable(releaseCtx); err != nil {
				slog.Error("could not clear the system proxy at quit", "error", err)
			}
			if err := tun.Release(releaseCtx); err != nil {
				slog.Error("could not stop the helper's core at quit", "error", err)
			}
			releaseDone()
			cancel()
			if err := runner.Stop(); err != nil {
				slog.Warn("mihomo did not exit cleanly", "error", err)
			}
			if err := traffic.Flush(); err != nil {
				slog.Error("could not save traffic history at quit", "error", err)
			}
		})
	}
	mygo.App.OnQuit(release)
	defer release()
	// TUN settles before Hop's own core starts, whatever the setting says:
	// a helper core a crashed Hop left running is adopted (TUN on) or
	// stopped (TUN off) — left alone it would hold the ports Hop's own core
	// needs. A failure runs Hop's own core rather than none.
	tunOn, tunErr := tun.Startup(ctx, userSettings.Tun)
	if tunErr != nil {
		slog.Error("could not settle TUN at start; running Hop's own core", "error", tunErr)
	}
	portService := service.NewMixedPortService(portState, store, mihomo.PortProbe{}, profiles, systemProxy, runner, ownPorts)
	dashboard := handler.NewDashboard(handler.Services{
		Proxies:       service.NewProxyService(client),
		Subscriptions: profiles,
		Modes:         service.NewModeService(client, store),
		Monitor:       service.NewMonitorService(client),
		Traffic:       traffic,
		Core:          coreService,
		Settings:      settingsService,
		Geodata:       geodataService,
		Rules:         customRulesService,
		AppUpdate:     appUpdateService,
		SystemProxy:   systemProxy,
		Tun:           tun,
		LoginItem:     mygo.App,
		Folders:       mygo.Shell,
		LogDir:        cfg.LogDir,
		ExitIP:        service.NewExitIPService(exitip.NewClient(exitip.TraceURL, hopProxyClient(portState))),
		Unlock:        service.NewUnlockService(client, unlockLanes(cfg.ProbePort)),
		LAN:           lanService,
		Port:          portService,
	}, bindAddress, userSettings, appVersion)
	// The dashboard hears what supervision does after a crash before the
	// core starts, so the first crash cannot go unreported.
	runner.OnRestart(dashboard.ReportCoreRestart)
	if !tunOn {
		if err := startCore(ctx, runner, profiles, portService, mihomo.PortProbe{}, instance.MixedPort, startupConfig); err != nil {
			return err
		}
	}

	// readyErr is set on the main thread when the UI cannot be built; Run
	// returns after the Quit that follows it.
	var readyErr error
	mygo.App.WhenReady(func() {
		// The language is the user's choice, or the system's while none was
		// made; mygo keeps it too, and this makes the settings the one truth.
		mygo.App.SetLocale(string(userSettings.Language))
		// Started at login, Hop runs in the menu bar only: no window and no
		// Dock icon until the user opens it.
		atLogin := mygo.App.WasOpenedAtLogin()
		if atLogin {
			mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
		}
		win := mygo.NewWindow(mygo.WindowOptions{
			Hidden:    atLogin,
			Title:     "Hop",
			Width:     860,
			Height:    720,
			MinWidth:  640,
			MinHeight: 440,
			StateKey:  "main",
			Content:   ui.View(dashboard.View),
			// The header is the title bar: the traffic lights sit on its
			// centre line, level with the switches beside them.
			TitleBarStyle:        mygo.TitleBarHidden,
			TrafficLightPosition: &mygo.Point{X: trafficLightX, Y: trafficLightY},
			Vibrancy:             windowVibrancy(),
		})
		menuBar, err := handler.NewMenuBar(win, dashboard)
		if err != nil {
			readyErr = err
			mygo.App.Quit()
			return
		}
		dashboard.Start(ctx, menuBar)
	})
	uiStarted = true
	runErr := mygo.App.Run()
	if readyErr != nil {
		runErr = readyErr
	}
	// The defer and OnQuit listener above have released the proxy and core.
	return runErr
}

// startCore starts Hop's own core on startupConfig. When the proxy port the
// user chose is held by another program, Hop runs this time on the instance's
// own port, which the settings row says; the choice stays saved for the next
// start. A listener on the port, once a leftover core of an earlier Hop is
// stopped, is another program, and is seen at once; a core that fails to start
// with the port taken is the same case seen late, after the wait for a port to
// free up. Any other failure is returned as it is.
func startCore(ctx context.Context, runner *mihomo.Runner, profiles *service.SubscriptionService, ports *service.MixedPortService, probe mihomo.PortProbe, instancePort int, startupConfig []byte) error {
	chosen := ports.Current()
	fallBack := func(cause error) error {
		slog.Warn("the proxy port the user chose is held by another program; using the default", "chosen", chosen, "default", instancePort, "error", cause)
		ports.FallBack(instancePort)
		fallbackConfig, err := profiles.StartupConfig()
		if err != nil {
			return fmt.Errorf("build the config for the default proxy port: %w", err)
		}
		return runner.Start(fallbackConfig)
	}
	if chosen == instancePort {
		return runner.Start(startupConfig)
	}
	if err := runner.ReapLeftover(); err != nil {
		return err
	}
	if listenErr := probe.Listening(ctx, chosen); listenErr == nil {
		return fallBack(fmt.Errorf("something listens on port %d", chosen))
	}
	startErr := runner.Start(startupConfig)
	if startErr == nil || probe.Free(chosen) == nil {
		return startErr
	}
	return fallBack(startErr)
}

// helperConnection is what the config applier and the TUN service need of the
// helper's client.
type helperConnection interface {
	service.HelperControl
	service.ConfigApplier
}

// helperAccess is the helper's client, the one geodata updates use, which
// waits longer, and its installer. An instance that does not use the helper
// gets stand-ins that fail and reach nothing: its start and quit must not
// stop the core the installed Hop runs under the helper.
func helperAccess(instance config.Instance) (helperConnection, helperConnection, service.HelperInstall) {
	if !instance.UsesHelper {
		return helper.Absent{}, helper.Absent{}, helper.Absent{}
	}
	return helper.NewClient(helper.SocketPath, helperCallTimeout), helper.NewClient(helper.SocketPath, geodataHelperTimeout), helper.Installer{}
}

// runningInstance is the development instance under mygo dev and the
// release one otherwise, so a dev build never shares files, ports or the core
// with an installed Hop.
func runningInstance() config.Instance {
	if mygo.IsDev() {
		return config.Development
	}
	return config.Release
}

// windowVibrancy is the material behind the main window: the macOS material,
// or Windows 11's Mica. MyGo shows the material only where the system has it
// (Windows 11 22H2 and later); elsewhere the window keeps the solid palette.
// Linux has no material.
func windowVibrancy() mygo.Vibrancy {
	switch runtime.GOOS {
	case "darwin":
		return windowMaterial
	case "windows":
		return windowsMaterial
	}
	return mygo.VibrancyNone
}

// hopProxyClient reaches the internet through Hop's own core, so a download
// or lookup works wherever the subscription's lines reach. It does not touch
// the system proxy or any other client's.
func hopProxyClient(port *service.MixedPortState) *http.Client {
	return &http.Client{Transport: &http.Transport{Proxy: func(*http.Request) (*url.URL, error) {
		// The port is read for each request: the user can move it while Hop runs.
		return hopProxyURL(port.Get()), nil
	}}}
}

// unlockLanes is one checker per probe lane, each through its own listener,
// which sends everything to the group the unlock check picked in that lane,
// whatever the rules say. Keep-alives are off: a connection kept from one
// group's check must not carry the next group's.
func unlockLanes(probePort int) []service.UnlockChecker {
	lanes := make([]service.UnlockChecker, 0, model.ProbeLanes)
	for lane := range model.ProbeLanes {
		client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true, Proxy: http.ProxyURL(hopProxyURL(probePort + lane))}}
		lanes = append(lanes, unlock.NewClient(client))
	}
	return lanes
}

// hopProxyURL is Hop's own mixed port, where its core accepts HTTP proxies.
func hopProxyURL(mixedPort int) *url.URL {
	return &url.URL{Scheme: "http", Host: net.JoinHostPort(bindAddress, strconv.Itoa(mixedPort))}
}

// geodataDownloadClient reaches the databases through the same proxy, with
// an overall timeout: a stuck connection must not hold the update lock
// forever.
func geodataDownloadClient(port *service.MixedPortState) *http.Client {
	client := hopProxyClient(port)
	client.Timeout = geodataDownloadTimeout
	return client
}

// showStartupError runs the app only to say why Hop cannot start, then
// quits: started from Finder, Hop would otherwise just disappear.
func showStartupError(err error) {
	mygo.App.WhenReady(func() {
		go func() {
			if _, dialogErr := mygo.Dialog.Message(mygo.MessageOptions{
				Type:    mygo.MessageError,
				Title:   locales.StartupFailedTitle(),
				Message: locales.StartupFailedMessage(),
				Detail:  err.Error(),
			}); dialogErr != nil {
				slog.Error("could not show why Hop cannot start", "error", dialogErr)
			}
			mygo.App.Quit()
		}()
	})
	if runErr := mygo.App.Run(); runErr != nil {
		slog.Error("could not run the app to show why Hop cannot start", "error", runErr)
	}
}

func main() {
	if err := run(); err != nil {
		slog.Error("hop failed", "error", err)
		if errors.As(err, new(startupError)) {
			showStartupError(err)
		}
		os.Exit(1)
	}
}
