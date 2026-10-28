// Package corestore keeps the mihomo cores Hop downloaded on an upgrade,
// apart from the core that ships inside the app, which it never touches.
package corestore

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"hop/internal/repository/corerelease"
)

const (
	dirPerm     = 0o700
	filePerm    = 0o600
	dirName     = "core"
	pointerName = "current.json"
	archiveName = "release-archive"
)

// Store is the directory of downloaded cores under Hop's data directory.
type Store struct {
	dir string
}

// New returns the store under appDir.
func New(appDir string) *Store {
	return &Store{dir: filepath.Join(appDir, dirName)}
}

// pointer is the file that says which downloaded core to run.
type pointer struct {
	Tag string `json:"tag"`
}

// BinaryPath is where the core of release tag lives, whether or not it has
// been downloaded yet.
func (s *Store) BinaryPath(tag string) (string, error) {
	// Only a plain version becomes a directory name: it keeps a tampered
	// pointer or API answer from leaving the store.
	if err := corerelease.CheckTag(tag); err != nil {
		return "", err
	}
	name := "mihomo"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(s.dir, tag, name), nil
}

// ArchivePath is where the GitHub release archive of tag is kept, whether
// or not it has been downloaded yet: the privileged helper verifies a core
// from it. The bundled core's archive lives here too, without a binary.
func (s *Store) ArchivePath(tag string) (string, error) {
	if err := corerelease.CheckTag(tag); err != nil {
		return "", err
	}
	return filepath.Join(s.dir, tag, archiveName), nil
}

// KeptArchive returns where the release archive of tag is kept and whether
// it is there.
func (s *Store) KeptArchive(tag string) (string, bool, error) {
	path, err := s.ArchivePath(tag)
	if err != nil {
		return "", false, err
	}
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return path, false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("check for a kept release archive (path=%q): %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", false, fmt.Errorf("the kept release archive is not a file (path=%q mode=%s)", path, info.Mode())
	}
	return path, true, nil
}

// Current returns the downloaded core Hop should run, and false when the
// user never upgraded or went back to the bundled core.
func (s *Store) Current() (tag string, binary string, found bool, err error) {
	path := s.pointerPath()
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("read core pointer (path=%q): %w", path, err)
	}
	var p pointer
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", "", false, fmt.Errorf("decode core pointer (path=%q): %w", path, err)
	}
	binary, err = s.BinaryPath(p.Tag)
	if err != nil {
		return "", "", false, fmt.Errorf("core pointer (path=%q): %w", path, err)
	}
	if _, err := os.Stat(binary); err != nil {
		return "", "", false, fmt.Errorf("core named by the pointer is missing (path=%q): %w", binary, err)
	}
	return p.Tag, binary, true, nil
}

// Activate makes tag the core Hop runs from now on. Its binary must exist.
func (s *Store) Activate(tag string) error {
	binary, err := s.BinaryPath(tag)
	if err != nil {
		return err
	}
	if _, err := os.Stat(binary); err != nil {
		return fmt.Errorf("cannot activate a core that is not installed (path=%q): %w", binary, err)
	}
	raw, err := json.Marshal(pointer{Tag: tag})
	if err != nil {
		return fmt.Errorf("encode core pointer: %w", err)
	}
	return writeAtomic(s.pointerPath(), raw)
}

// Deactivate makes Hop run the bundled core again.
func (s *Store) Deactivate() error {
	if err := os.Remove(s.pointerPath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove core pointer (path=%q): %w", s.pointerPath(), err)
	}
	return nil
}

// Remove deletes the downloaded core of release tag, if any.
func (s *Store) Remove(tag string) error {
	binary, err := s.BinaryPath(tag)
	if err != nil {
		return err
	}
	dir := filepath.Dir(binary)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove downloaded core (path=%q): %w", dir, err)
	}
	return nil
}

// Prune deletes every downloaded core except keepTag; "" keeps none.
func (s *Store) Prune(keepTag string) error {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("list downloaded cores (path=%q): %w", s.dir, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == keepTag || corerelease.CheckTag(entry.Name()) != nil {
			continue
		}
		if err := s.Remove(entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) pointerPath() string {
	return filepath.Join(s.dir, pointerName)
}

// writeAtomic writes through a temporary file, so a crash never leaves a
// half-written pointer.
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("create dir (path=%q): %w", filepath.Dir(path), err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, filePerm); err != nil {
		return fmt.Errorf("write file (path=%q): %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("move file into place (from=%q to=%q): %w", tmp, path, err)
	}
	return nil
}
