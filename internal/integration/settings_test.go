package integration

import (
	"os"
	"path/filepath"
	"testing"

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
	settings.Theme = model.ThemeDusk
	if err := settingsService.Save(settings); err != nil {
		t.Fatal(err)
	}
	reopened, err := profile.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := service.NewSettingsService(reopened).Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved != settings {
		t.Fatalf("settings after reopen: %+v, want %+v", saved, settings)
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
