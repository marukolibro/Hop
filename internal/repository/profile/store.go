// Package profile keeps the imported subscription on disk.
package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"hop/internal/model"
)

const (
	dirPerm  = 0o700
	filePerm = 0o600

	urlFileName  = "subscription.url"
	rawFileName  = "subscription.yaml"
	metaFileName = "subscription.meta"
	modeFileName = "mode"
)

// subscriptionMeta is what the store keeps about a download besides its
// body and link.
type subscriptionMeta struct {
	UserInfo  string    `json:"userInfo"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Store keeps one subscription under dir. Its files are readable by the
// owner only: the link carries the account's token.
type Store struct {
	dir string
}

// NewStore returns a store that keeps its files in dir.
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// Save replaces the stored subscription.
func (s *Store) Save(sub model.Subscription) error {
	if err := os.MkdirAll(s.dir, dirPerm); err != nil {
		return fmt.Errorf("create profile dir (path=%q): %w", s.dir, err)
	}
	meta, err := json.Marshal(subscriptionMeta{UserInfo: sub.UserInfo, UpdatedAt: sub.UpdatedAt})
	if err != nil {
		return fmt.Errorf("encode subscription meta: %w", err)
	}
	if err := writeAtomic(filepath.Join(s.dir, rawFileName), sub.Raw); err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(s.dir, metaFileName), meta); err != nil {
		return err
	}
	return writeAtomic(filepath.Join(s.dir, urlFileName), []byte(sub.URL))
}

// Load returns the stored subscription, and false when none was saved.
func (s *Store) Load() (model.Subscription, bool, error) {
	rawPath := filepath.Join(s.dir, rawFileName)
	urlPath := filepath.Join(s.dir, urlFileName)
	raw, err := os.ReadFile(rawPath)
	if errors.Is(err, fs.ErrNotExist) {
		return model.Subscription{}, false, nil
	}
	if err != nil {
		return model.Subscription{}, false, fmt.Errorf("read stored subscription (path=%q): %w", rawPath, err)
	}
	link, err := os.ReadFile(urlPath)
	if err != nil {
		return model.Subscription{}, false, fmt.Errorf("read stored subscription link (path=%q): %w", urlPath, err)
	}
	meta, err := s.loadMeta()
	if err != nil {
		return model.Subscription{}, false, err
	}
	return model.Subscription{URL: string(link), Raw: raw, UserInfo: meta.UserInfo, UpdatedAt: meta.UpdatedAt}, true, nil
}

// loadMeta reads what was kept about the last download. A subscription
// saved before the store kept it has none: its usage and time are unknown.
func (s *Store) loadMeta() (subscriptionMeta, error) {
	path := filepath.Join(s.dir, metaFileName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return subscriptionMeta{}, nil
	}
	if err != nil {
		return subscriptionMeta{}, fmt.Errorf("read subscription meta (path=%q): %w", path, err)
	}
	var meta subscriptionMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return subscriptionMeta{}, fmt.Errorf("decode subscription meta (path=%q): %w", path, err)
	}
	return meta, nil
}

// writeAtomic writes through a temporary file, so a crash never leaves half
// a file behind.
func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, filePerm); err != nil {
		return fmt.Errorf("write file (path=%q): %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("move file into place (from=%q to=%q): %w", tmp, path, err)
	}
	return nil
}

// SaveMode keeps the outbound mode for the next start.
func (s *Store) SaveMode(mode string) error {
	if err := os.MkdirAll(s.dir, dirPerm); err != nil {
		return fmt.Errorf("create profile dir (path=%q): %w", s.dir, err)
	}
	return writeAtomic(filepath.Join(s.dir, modeFileName), []byte(mode))
}

// LoadMode returns the saved outbound mode, and false when none was saved.
func (s *Store) LoadMode() (string, bool, error) {
	path := filepath.Join(s.dir, modeFileName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read saved mode (path=%q): %w", path, err)
	}
	return string(raw), true, nil
}
