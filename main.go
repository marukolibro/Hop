package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"hop/config"
	"hop/internal/handler"
	"hop/internal/repository/corerelease"
	"hop/internal/repository/corestore"
	"hop/internal/repository/helper"
	"hop/internal/repository/mihomo"
	"hop/internal/repository/profile"
	"hop/internal/repository/routetable"
	"hop/internal/repository/subscription"
	"hop/internal/repository/sysproxy"
	"hop/internal/service"
)

const (
	controllerTimeout = 5 * time.Second
	downloadTimeout   = 30 * time.Second
	// validationTimeout bounds one config check: the first may have to download
	// geodata.
	validationTimeout = 30 * time.Second
	// quitReleaseTimeout bounds clearing the system proxy and stopping the
	// helper's core at quit.
	quitReleaseTimeout = 10 * time.Second
	// helperCallTimeout bounds one request to the privileged helper; a
	// start waits for the core's controller, which can take seconds.
	helperCallTimeout = 20 * time.Second
	bindAddress       = "127.0.0.1"
	coreLogLevel      = "info"
	bootstrapGroup    = "Hop"

	// trafficLightX and trafficLightY place the close button's top-left
	// corner so the traffic lights centre on the header's 60 DIP row.
	trafficLightX = 20
	trafficLightY = 23
)

func run() error {
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("locate user config dir: %w", err)
	}
	resourcesDir, err := mygo.App.Path(mygo.PathResources)
	if err != nil {
		return fmt.Errorf("locate app resources: %w", err)
	}
	cfg, err := config.Load(userConfigDir, resourcesDir)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store, err := profile.NewStore(cfg.AppDir)
	if err != nil {
		return err
	}
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
	}
	client := mihomo.NewClient(cfg.ControllerAddr, cfg.Secret, mihomo.ConfigPath(cfg.HomeDir), &http.Client{}, controllerTimeout)
	helperClient := helper.NewClient(helper.SocketPath, helperCallTimeout)
	tunFlag := service.NewTunFlag()
	profiles := service.NewSubscriptionService(
		subscription.NewFetcher(&http.Client{Timeout: downloadTimeout}),
		store,
		// Configs go to whichever core serves the controller: the helper's
		// while TUN is on, Hop's own otherwise.
		service.NewCoreApplier(client, helperClient, tunFlag),
		// Config checks borrow the core's own -t, in an isolated directory.
		mihomo.NewConfigValidator(cfg.MihomoBinary, filepath.Join(cfg.AppDir, "validate"), validationTimeout),
		client,
		settings,
		tunFlag,
	)

	startupConfig, err := profiles.StartupConfig()
	if err != nil {
		return err
	}
	// The core would download geodata through the network on first use,
	// which is slow and can fail where GitHub cannot be reached; what ships
	// with the app is seeded in first, and only when missing.
	// The config checks run the core in a directory of their own, and a
	// config with GEOSITE or GEOIP rules (Hop's rule template has both) makes
	// even -t load the databases: seed that directory too.
	for _, dir := range []string{cfg.HomeDir, filepath.Join(cfg.AppDir, "validate")} {
		if err := mihomo.SeedGeodata(filepath.Join(resourcesDir, "geodata"), dir); err != nil {
			return err
		}
	}
	runner := mihomo.NewRunner(ctx, cfg, client)

	coreService := service.NewCoreService(
		client,
		corerelease.NewSource(coreDownloadClient(cfg.MixedPort), corerelease.ReleaseAPI),
		corestore.New(cfg.AppDir),
		runner,
		mihomo.NewCoreChecker(mihomo.ConfigPath(cfg.HomeDir), filepath.Join(cfg.AppDir, "validate"), validationTimeout),
		cfg.MihomoBinary,
		cfg.BundledBinary,
		cfg.CoreOrigin,
		tunFlag,
	)

	settingsService := service.NewSettingsService(store)
	userSettings, err := settingsService.Load()
	if err != nil {
		return err
	}
	systemProxy := service.NewSystemProxyService(sysproxy.New(), bindAddress, cfg.MixedPort)
	helperTool, toolErr := helper.BundledToolPath(resourcesDir)
	if toolErr != nil {
		// The app runs without TUN; turning it on then fails, naming what
		// is missing.
		slog.Warn("this build ships no helper binary; TUN stays unavailable", "error", toolErr)
	}
	tun := service.NewTunService(
		helperClient,
		helper.Installer{},
		routetable.Reader{},
		runner,
		profiles,
		mihomo.NewConfigValidator(cfg.MihomoBinary, filepath.Join(cfg.AppDir, "validate"), validationTimeout),
		cfg.MihomoBinary,
		filepath.Join(resourcesDir, "geodata"),
		helperTool,
		tunFlag,
		client,
	)
	// TUN settles before Hop's own core starts, whatever the setting says:
	// a helper core a crashed Hop left running is adopted (TUN on) or
	// stopped (TUN off) — left alone it would hold the ports Hop's own core
	// needs. A failure runs Hop's own core rather than none.
	tunOn, tunErr := tun.Startup(ctx, userSettings.Tun)
	if tunErr != nil {
		slog.Error("could not settle TUN at start; running Hop's own core", "error", tunErr)
	}
	dashboard := handler.NewDashboard(handler.Services{
		Proxies:       service.NewProxyService(client),
		Subscriptions: profiles,
		Modes:         service.NewModeService(client, store),
		Monitor:       service.NewMonitorService(client),
		Core:          coreService,
		Settings:      settingsService,
		SystemProxy:   systemProxy,
		Tun:           tun,
		LoginItem:     mygo.App,
	}, net.JoinHostPort(bindAddress, strconv.Itoa(cfg.MixedPort)), userSettings, mygo.App.Version())
	// The dashboard hears what supervision does after a crash before the
	// core starts, so the first crash cannot go unreported.
	runner.OnRestart(dashboard.ReportCoreRestart)
	if !tunOn {
		if err := runner.Start(startupConfig); err != nil {
			return err
		}
	}

	// readyErr is set on the main thread when the UI cannot be built; Run
	// returns after the Quit that follows it.
	var readyErr error
	mygo.App.WhenReady(func() {
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
		})
		menuBar, err := handler.NewMenuBar(win, dashboard)
		if err != nil {
			readyErr = err
			mygo.App.Quit()
			return
		}
		dashboard.Start(ctx, menuBar)
	})
	runErr := mygo.App.Run()
	if readyErr != nil {
		runErr = readyErr
	}

	// Apps must not be left sending traffic to a core that is gone. The
	// app's context is about to end, so the releases get their own.
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
	return runErr
}

// coreDownloadClient reaches GitHub through Hop's own core, so a core can be
// fetched wherever the subscription's lines can reach it. It does not touch
// the system proxy or any other client's.
func coreDownloadClient(mixedPort int) *http.Client {
	proxy := &url.URL{Scheme: "http", Host: net.JoinHostPort(bindAddress, strconv.Itoa(mixedPort))}
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}}
}

func main() {
	if err := run(); err != nil {
		slog.Error("hop failed", "error", err)
		os.Exit(1)
	}
}
