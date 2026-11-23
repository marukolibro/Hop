package profile

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const proxyBypassFileName = "proxy-bypass.json"

// proxyBypassFile is the user's extra system proxy bypass entries as kept on
// disk.
type proxyBypassFile struct {
	Entries []string `json:"entries"`
}

// LoadProxyBypass returns the extra system proxy bypass entries saved by the
// user. No file yet means none were added.
func (s *Store) LoadProxyBypass() ([]string, error) {
	path := filepath.Join(s.dir, proxyBypassFileName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read proxy bypass list (path=%q): %w", path, err)
	}
	var file proxyBypassFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("parse proxy bypass list (path=%q): %w", path, err)
	}
	return file.Entries, nil
}

// SaveProxyBypass keeps the extra system proxy bypass entries atomically.
func (s *Store) SaveProxyBypass(entries []string) error {
	if entries == nil {
		entries = []string{}
	}
	raw, err := json.Marshal(proxyBypassFile{Entries: entries})
	if err != nil {
		return fmt.Errorf("encode proxy bypass list: %w", err)
	}
	return writeAtomic(filepath.Join(s.dir, proxyBypassFileName), raw)
}
