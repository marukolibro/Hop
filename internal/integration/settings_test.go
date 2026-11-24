package integration

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"hop/internal/model"
	"hop/internal/repository/profile"
	"hop/internal/service"
)

// TestColorThemeSettings exercises migration and persistence through the real
// store and service without touching the user's settings or network switches.
func TestColorThemeSettings(t *testing.T) {
	dir := t.TempDir()
	store, err := profile.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	settingsService := service.NewSettingsService(store)
	settings, err := settingsService.Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Theme != model.ThemeOcean || settings.WindowClarity != 68 {
		t.Fatalf("fresh settings: theme=%q clarity=%d", settings.Theme, settings.WindowClarity)
	}
	legacy := []byte(`{"appearance":"system","delayTimeoutSeconds":3,"subscriptionUpdateHours":6,"menuBarSpeed":false,"windowClarity":23}`)
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err = settingsService.Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Theme != model.ThemeOcean || settings.WindowClarity != 23 || settings.SubscriptionUpdateHours != 6 {
		t.Fatalf("legacy settings were not preserved: %+v", settings)
	}
	for _, theme := range service.ColorThemes {
		settings.Theme = theme
		if err := settingsService.Save(settings); err != nil {
			t.Fatalf("save theme %q: %v", theme, err)
		}
		reopened, err := profile.NewStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		saved, err := service.NewSettingsService(reopened).Load()
		if err != nil {
			t.Fatalf("load theme %q: %v", theme, err)
		}
		if saved != settings {
			t.Fatalf("settings after reopen for %q: %+v, want %+v", theme, saved, settings)
		}
	}
	reopened, err := profile.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := service.NewSettingsService(reopened).Load()
	if err != nil {
		t.Fatal(err)
	}
	invalid := saved
	invalid.Theme = model.ColorTheme("unsupported")
	if err := settingsService.Save(invalid); err == nil {
		t.Fatal("unsupported theme was accepted")
	}
	unchanged, err := service.NewSettingsService(reopened).Load()
	if err != nil {
		t.Fatal(err)
	}
	if unchanged != saved {
		t.Fatal("rejected theme changed the saved settings")
	}
}

// TestCorruptTrafficHistory checks that a zero-filled traffic.json, as a power
// loss can leave it, is set aside and Hop starts with an empty history.
func TestCorruptTrafficHistory(t *testing.T) {
	dir := t.TempDir()
	store, err := profile.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "traffic.json"), make([]byte, 512), 0o600); err != nil {
		t.Fatal(err)
	}
	traffic, err := service.NewTrafficService(store)
	if err != nil {
		t.Fatalf("a corrupt traffic history kept the service from starting: %v", err)
	}
	if sources := traffic.Summary(time.Now()).Sources; len(sources) != 0 {
		t.Fatal("the history set aside still counts")
	}
	if _, err := os.Stat(filepath.Join(dir, "traffic.json.corrupt")); err != nil {
		t.Fatalf("the corrupt history was not kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "traffic.json")); !os.IsNotExist(err) {
		t.Fatalf("traffic.json is still in place: %v", err)
	}
}
