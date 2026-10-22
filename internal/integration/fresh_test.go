package integration

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"hop/config"
	"hop/internal/repository/mihomo"
	"hop/internal/repository/profile"
	"hop/internal/service"
)

// TestFreshInstallStartup runs main's startup steps, in its order, on a Mac
// Hop never ran on: the user config dir has no hop folder at all. 0.1.2
// exited at once there, writing the controller secret into a folder nothing
// had made yet; developers' machines always had it.
func TestFreshInstallStartup(t *testing.T) {
	if os.Getenv("HOP_MIHOMO_BINARY") == "" {
		t.Skip("HOP_MIHOMO_BINARY is not set")
	}
	userConfigDir := t.TempDir()
	resources := filepath.Join("..", "..", "resources")

	cfg, err := config.Load(config.Release, userConfigDir, resources)
	if err != nil {
		t.Fatalf("load the config on a fresh install: %v", err)
	}
	secret, err := os.Stat(filepath.Join(cfg.AppDir, "controller-secret"))
	if err != nil {
		t.Fatal(err)
	}
	// Windows has no Unix modes: the user's profile directory keeps the app
	// data private there.
	if runtime.GOOS != "windows" {
		info, err := os.Stat(cfg.AppDir)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Errorf("app dir mode %o, want 700: it holds the subscriptions and the controller secret", perm)
		}
		if perm := secret.Mode().Perm(); perm != 0o600 {
			t.Errorf("controller secret mode %o, want 600", perm)
		}
	}

	store, err := profile.NewStore(cfg.AppDir)
	if err != nil {
		t.Fatalf("open the store on a fresh install: %v", err)
	}
	if _, err := service.NewTrafficService(store); err != nil {
		t.Fatalf("open the traffic history on a fresh install: %v", err)
	}
	if _, err := service.StartupMode(store); err != nil {
		t.Fatalf("read the startup mode on a fresh install: %v", err)
	}
	if _, err := service.NewSettingsService(store).Load(); err != nil {
		t.Fatalf("load the settings on a fresh install: %v", err)
	}
	appGeodata := filepath.Join(cfg.AppDir, "geodata")
	if err := mihomo.SeedGeodata(filepath.Join(resources, "geodata"), appGeodata); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{cfg.HomeDir, filepath.Join(cfg.AppDir, "validate")} {
		if err := mihomo.SeedGeodata(appGeodata, dir); err != nil {
			t.Fatal(err)
		}
	}

	// The core starts on the built-in profile, as with no subscription yet.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	h := newHarness(t, cfg.AppDir)
	coreCtx, stopCore := context.WithCancel(ctx)
	core := h.startCore(t, coreCtx)
	stopCore()
	if err := core.Wait(); err != nil {
		t.Errorf("stop core: %v", err)
	}
}
