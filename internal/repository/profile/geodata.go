package profile

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const geodataFileName = "geodata.json"

// geodataFile is when the geo databases last updated successfully.
type geodataFile struct {
	UpdatedAt time.Time `json:"updatedAt"`
}

// LoadGeodataUpdatedAt returns when the geo databases last updated, and
// false when they never did: they are then the ones the app ships.
func (s *Store) LoadGeodataUpdatedAt() (time.Time, bool, error) {
	path := filepath.Join(s.dir, geodataFileName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("read geodata update time (path=%q): %w", path, err)
	}
	var file geodataFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return time.Time{}, false, fmt.Errorf("parse geodata update time (path=%q): %w", path, err)
	}
	if file.UpdatedAt.IsZero() {
		return time.Time{}, false, fmt.Errorf("the stored geodata update time is zero (path=%q)", path)
	}
	return file.UpdatedAt, true, nil
}

// SaveGeodataUpdatedAt records a successful geodata update.
func (s *Store) SaveGeodataUpdatedAt(at time.Time) error {
	raw, err := json.Marshal(geodataFile{UpdatedAt: at})
	if err != nil {
		return fmt.Errorf("encode geodata update time: %w", err)
	}
	return writeAtomic(filepath.Join(s.dir, geodataFileName), raw)
}
