package profile

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const fakeIPExceptionsFileName = "fake-ip-exceptions.json"

// fakeIPExceptionsFile is the fake-ip exceptions as kept on disk.
type fakeIPExceptionsFile struct {
	Names []string `json:"names"`
}

// LoadFakeIPExceptions returns the names the user keeps out of fake-ip under
// TUN. No file yet means none were added.
func (s *Store) LoadFakeIPExceptions() ([]string, error) {
	path := filepath.Join(s.dir, fakeIPExceptionsFileName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read fake-ip exceptions (path=%q): %w", path, err)
	}
	var file fakeIPExceptionsFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("parse fake-ip exceptions (path=%q): %w", path, err)
	}
	return file.Names, nil
}

// SaveFakeIPExceptions keeps the names for the next config and the next start.
func (s *Store) SaveFakeIPExceptions(names []string) error {
	if names == nil {
		names = []string{}
	}
	raw, err := json.Marshal(fakeIPExceptionsFile{Names: names})
	if err != nil {
		return fmt.Errorf("encode fake-ip exceptions: %w", err)
	}
	return writeAtomic(filepath.Join(s.dir, fakeIPExceptionsFileName), raw)
}
