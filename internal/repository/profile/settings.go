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

// settingsFile is the settings as kept on disk.
type settingsFile struct {
	Appearance          string `json:"appearance"`
	DelayTimeoutSeconds int    `json:"delayTimeoutSeconds"`
	SystemProxy         bool   `json:"systemProxy"`
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
	return model.Settings{
		Appearance:          model.Appearance(file.Appearance),
		DelayTimeoutSeconds: file.DelayTimeoutSeconds,
		SystemProxy:         file.SystemProxy,
	}, true, nil
}

// SaveSettings keeps settings for the next start.
func (s *Store) SaveSettings(settings model.Settings) error {
	raw, err := json.Marshal(settingsFile{
		Appearance:          string(settings.Appearance),
		DelayTimeoutSeconds: settings.DelayTimeoutSeconds,
		SystemProxy:         settings.SystemProxy,
	})
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	return writeAtomic(filepath.Join(s.dir, settingsFileName), raw)
}
