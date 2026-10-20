package mihomo

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// geodataFiles are the geodata databases a config Hop generates can need,
// under the exact names the core looks for in its home dir (case matters on
// case-sensitive systems): geoip.metadb serves GEOIP rules, which is what
// the core runs with because Hop never turns on geodata-mode, and
// GeoSite.dat serves GEOSITE ones. Without them the core downloads each
// through the network on first use, which is slow and can fail.
var geodataFiles = []string{"GeoSite.dat", "geoip.metadb"}

// SeedGeodata copies the geodata that ships with the app (srcDir,
// resources/geodata) into the core's home dir, for the files that are
// missing there. A file already in the home dir is never overwritten: the
// core may have fetched a newer one itself. A file missing from srcDir is
// warned about and skipped; the core downloads geodata itself when a config
// needs one Hop did not seed.
func SeedGeodata(srcDir string, homeDir string) error {
	for _, name := range geodataFiles {
		src := filepath.Join(srcDir, name)
		dst := filepath.Join(homeDir, name)
		if _, err := os.Stat(dst); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("check for geodata in the core's home (path=%q): %w", dst, err)
		}
		data, err := os.ReadFile(src)
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
