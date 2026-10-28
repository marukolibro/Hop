package mihomo

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"hop/internal/repository/plainfile"
)

// maxGeodataBytes bounds one geodata file read: the databases are a few
// megabytes.
const maxGeodataBytes = 128 << 20

// geodataFiles are the geodata databases a config Hop generates can need,
// under the exact names the core looks for in its home dir (case matters on
// case-sensitive systems): geoip.metadb serves GEOIP rules, which is what
// the core runs with because Hop never turns on geodata-mode, and
// GeoSite.dat serves GEOSITE ones. Without them the core downloads each
// through the network on first use, which is slow and can fail.
var geodataFiles = []string{GeoSiteFile, "geoip.metadb"}

// GeoSiteFile is the GEOSITE database's name in a core's home dir.
const GeoSiteFile = "GeoSite.dat"

// SeedGeodata copies the geodata that ships with the app (srcDir,
// resources/geodata) into the core's home dir, for the files that are
// missing there. A file already in the home dir is never overwritten: the
// core may have fetched a newer one itself. A file missing from srcDir is
// warned about and skipped; the core downloads geodata itself when a config
// needs one Hop did not seed. Only plain files are read (plainfile.Read): the
// privileged helper seeds from a directory Hop names.
func SeedGeodata(srcDir string, homeDir string) error {
	for _, name := range geodataFiles {
		src := filepath.Join(srcDir, name)
		dst := filepath.Join(homeDir, name)
		if _, err := os.Stat(dst); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("check for geodata in the core's home (path=%q): %w", dst, err)
		}
		data, err := plainfile.Read(src, maxGeodataBytes)
		if errors.Is(err, os.ErrNotExist) {
			slog.Warn("the app ships no bundled geodata, the core will download it", "file", name, "dir", srcDir)
			continue
		}
		if err != nil {
			return fmt.Errorf("read bundled geodata (path=%q): %w", src, err)
		}
		if err := os.MkdirAll(homeDir, homeDirPerm); err != nil {
			return fmt.Errorf("create mihomo home dir (path=%q): %w", homeDir, err)
		}
		if err := os.WriteFile(dst, data, configPerm); err != nil {
			return fmt.Errorf("seed geodata into the core's home (path=%q): %w", dst, err)
		}
		slog.Info("seeded geodata for the core", "file", name, "bytes", len(data))
	}
	return nil
}

// ReplaceGeodata copies both geodata files from srcDir over dir, each through
// a temporary file and a rename, so an interrupted replacement never leaves
// half a database in place. Unlike SeedGeodata it overwrites what is there:
// the caller has verified the new files and wants the core to read them. perm
// is their mode: owner-only in Hop's own directories, readable by the root
// core under the helper. Only plain files are read (plainfile.Read): the
// privileged helper replaces from a directory Hop names, and must never copy
// a file the user cannot read into a place the user can.
func ReplaceGeodata(srcDir string, dir string, perm os.FileMode) error {
	if err := os.MkdirAll(dir, homeDirPerm); err != nil {
		return fmt.Errorf("create geodata dir (path=%q): %w", dir, err)
	}
	for _, name := range geodataFiles {
		data, err := plainfile.Read(filepath.Join(srcDir, name), maxGeodataBytes)
		if err != nil {
			return fmt.Errorf("read geodata to replace (path=%q): %w", filepath.Join(srcDir, name), err)
		}
		if err := writeFileAtomic(filepath.Join(dir, name), data, perm); err != nil {
			return err
		}
	}
	return nil
}

// writeFileAtomic writes data through a temporary file in the target's
// directory and renames it into place.
func writeFileAtomic(path string, data []byte, perm os.FileMode) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("stage file (path=%q): %w", path, err)
	}
	name := tmp.Name()
	defer func() {
		if err == nil {
			return
		}
		if removeErr := os.Remove(name); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove staged file (path=%q): %w", name, removeErr))
		}
	}()
	if _, writeErr := tmp.Write(data); writeErr != nil {
		return errors.Join(fmt.Errorf("write staged file (path=%q): %w", name, writeErr), tmp.Close())
	}
	if err := tmp.Chmod(perm); err != nil {
		return errors.Join(fmt.Errorf("set staged file's mode (path=%q): %w", name, err), tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close staged file (path=%q): %w", name, err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("put file in place (from=%q to=%q): %w", name, path, err)
	}
	return nil
}
