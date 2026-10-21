package profile

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"hop/internal/model"
)

const settingsFileName = "settings.json"

// settingsFile is the settings as kept on disk. SubscriptionUpdateHours and
// MenuBarSpeed are pointers so a file written before the setting existed
// reads as the default, while a saved 0 or false stays so.
type settingsFile struct {
	Appearance              string `json:"appearance"`
	DelayTimeoutSeconds     int    `json:"delayTimeoutSeconds"`
	SubscriptionUpdateHours *int   `json:"subscriptionUpdateHours,omitempty"`
	SystemProxy             bool   `json:"systemProxy"`
	Tun                     bool   `json:"tun"`
	MenuBarSpeed            *bool  `json:"menuBarSpeed,omitempty"`
}

// LoadSettings returns the saved settings, and false when none were saved.
func (s *Store) LoadSettings() (model.Settings, bool, error) {
	path := filepath.Join(s.dir, settingsFileName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return model.Settings{}, false, nil
	}
	if err != nil {
		return model.Settings{}, false, fmt.Errorf("read settings (path=%q): %w", path, err)
	}
	var file settingsFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return model.Settings{}, false, fmt.Errorf("parse settings (path=%q): %w", path, err)
	}
	updateHours := model.DefaultSubscriptionUpdateHours
	if file.SubscriptionUpdateHours != nil {
		updateHours = *file.SubscriptionUpdateHours
	}
	menuBarSpeed := model.DefaultMenuBarSpeed
	if file.MenuBarSpeed != nil {
		menuBarSpeed = *file.MenuBarSpeed
	}
	return model.Settings{
		Appearance:              model.Appearance(file.Appearance),
		DelayTimeoutSeconds:     file.DelayTimeoutSeconds,
		SubscriptionUpdateHours: updateHours,
		SystemProxy:             file.SystemProxy,
		Tun:                     file.Tun,
		MenuBarSpeed:            menuBarSpeed,
	}, true, nil
}

// SaveSettings keeps settings for the next start.
func (s *Store) SaveSettings(settings model.Settings) error {
	updateHours, menuBarSpeed := settings.SubscriptionUpdateHours, settings.MenuBarSpeed
	raw, err := json.Marshal(settingsFile{
		Appearance:              string(settings.Appearance),
		DelayTimeoutSeconds:     settings.DelayTimeoutSeconds,
		SubscriptionUpdateHours: &updateHours,
		SystemProxy:             settings.SystemProxy,
		Tun:                     settings.Tun,
		MenuBarSpeed:            &menuBarSpeed,
	})
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	return writeAtomic(filepath.Join(s.dir, settingsFileName), raw)
}
