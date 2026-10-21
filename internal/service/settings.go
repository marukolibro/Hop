package service

import (
	"fmt"
	"slices"
	"time"

	"hop/internal/model"
)

// SettingsStore keeps the user's settings between starts.
type SettingsStore interface {
	LoadSettings() (model.Settings, bool, error)
	SaveSettings(settings model.Settings) error
}

// Appearances are the looks the settings offer, in the order they show.
var Appearances = []model.Appearance{model.AppearanceSystem, model.AppearanceLight, model.AppearanceDark}

// DelayTimeouts are the delay test timeouts the settings offer, in seconds.
var DelayTimeouts = []int{2, 3, 5}

// SubscriptionUpdateIntervals are the automatic refresh intervals the
// settings offer, in hours; 0 turns the automatic refresh off.
var SubscriptionUpdateIntervals = []int{0, 6, 12, 24}

// DefaultSettings are the settings before the user changed any: the system's
// look, a timeout most nodes answer well within, and a subscription refresh
// twice a day.
func DefaultSettings() model.Settings {
	return model.Settings{
		Appearance:              model.AppearanceSystem,
		DelayTimeoutSeconds:     3,
		SubscriptionUpdateHours: model.DefaultSubscriptionUpdateHours,
	}
}

// SettingsService loads and keeps the user's settings.
type SettingsService struct {
	store SettingsStore
}

// NewSettingsService returns a service keeping settings in store.
func NewSettingsService(store SettingsStore) *SettingsService {
	return &SettingsService{store: store}
}

// Load returns the saved settings, or DefaultSettings before any were saved.
// Saved settings Hop does not offer are an error, not replaced.
func (s *SettingsService) Load() (model.Settings, error) {
	settings, ok, err := s.store.LoadSettings()
	if err != nil {
		return model.Settings{}, err
	}
	if !ok {
		return DefaultSettings(), nil
	}
	if err := validateSettings(settings); err != nil {
		return model.Settings{}, fmt.Errorf("saved settings: %w", err)
	}
	return settings, nil
}

// Save keeps settings for the next start.
func (s *SettingsService) Save(settings model.Settings) error {
	if err := validateSettings(settings); err != nil {
		return err
	}
	return s.store.SaveSettings(settings)
}

func validateSettings(settings model.Settings) error {
	if !slices.Contains(Appearances, settings.Appearance) {
		return fmt.Errorf("unknown appearance %q", settings.Appearance)
	}
	if !slices.Contains(DelayTimeouts, settings.DelayTimeoutSeconds) {
		return fmt.Errorf("unsupported delay test timeout %ds (want one of %v)", settings.DelayTimeoutSeconds, DelayTimeouts)
	}
	if !slices.Contains(SubscriptionUpdateIntervals, settings.SubscriptionUpdateHours) {
		return fmt.Errorf("unsupported subscription update interval %dh (want one of %v)", settings.SubscriptionUpdateHours, SubscriptionUpdateIntervals)
	}
	return nil
}

// DelayTimeout is how long a delay test waits for one node under settings.
func DelayTimeout(settings model.Settings) time.Duration {
	return time.Duration(settings.DelayTimeoutSeconds) * time.Second
}
