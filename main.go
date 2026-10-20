package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"hop/config"
	"hop/internal/handler"
	"hop/internal/repository/mihomo"
	"hop/internal/repository/profile"
	"hop/internal/repository/subscription"
	"hop/internal/service"
)

const (
	controllerTimeout = 5 * time.Second
	downloadTimeout   = 30 * time.Second
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
	cfg, err := config.Load(userConfigDir)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := profile.NewStore(cfg.AppDir)
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
	profiles := service.NewProfileService(
		subscription.NewFetcher(&http.Client{Timeout: downloadTimeout}),
		store,
		client,
		settings,
	)

	startupConfig, err := profiles.StartupConfig()
	if err != nil {
		return err
	}
	core, err := mihomo.Start(ctx, cfg, startupConfig)
	if err != nil {
		return err
	}
	if err := mihomo.WaitReady(ctx, client); err != nil {
		cancel()
		// The readiness error is the root cause; the exit is only logged.
		if waitErr := core.Wait(); waitErr != nil {
			slog.Warn("mihomo exited after failed start", "error", waitErr)
		}
		return err
	}

	dashboard := handler.NewDashboard(handler.Services{
		Proxies:       service.NewProxyService(client),
		Subscriptions: profiles,
		Modes:         service.NewModeService(client, store),
		Monitor:       service.NewMonitorService(client),
	}, fmt.Sprintf("%s:%d", bindAddress, cfg.MixedPort))
	// readyErr is set on the main thread when the UI cannot be built; Run
	// returns after the Quit that follows it.
	var readyErr error
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
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

	cancel()
	if err := core.Wait(); err != nil {
		slog.Warn("mihomo did not exit cleanly", "error", err)
	}
	return runErr
}

func main() {
	if err := run(); err != nil {
		slog.Error("hop failed", "error", err)
		os.Exit(1)
	}
}
