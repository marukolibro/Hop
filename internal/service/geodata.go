package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"hop/internal/model"
	"hop/internal/repository/mihomo"
)

const (
	// geodataUpdateEvery is how often the rule databases refresh on their
	// own: the lists change slowly, and a weekly download keeps them useful
	// without restarting the core often.
	geodataUpdateEvery = 7 * 24 * time.Hour
	// geodataRetryBackoff keeps a failed automatic update from retrying at
	// every check: the first failure waits this long, and the date shown to
	// the user stays the last success.
	geodataRetryBackoff = time.Hour
	// geodataFilePerm is how Hop keeps the databases: owner-only in its own
	// directories, which are owner-only themselves.
	geodataFilePerm = 0o600
)

// GeodataDownloader fetches both databases into dir under the names the core
// reads, verified.
type GeodataDownloader interface {
	Download(ctx context.Context, dir string) error
}

// GeodataValidator has the core check a config with the databases in dir.
type GeodataValidator interface {
	ValidateWithGeodata(ctx context.Context, config []byte, dir string) error
}

// GeodataConfigs builds the config the core runs now, for validation.
type GeodataConfigs interface {
	CurrentConfig(ctx context.Context) ([]byte, error)
}

// GeodataStore keeps when the databases last updated.
type GeodataStore interface {
	LoadGeodataUpdatedAt() (time.Time, bool, error)
	SaveGeodataUpdatedAt(at time.Time) error
}

// HelperGeodata makes the helper's running core read new databases.
type HelperGeodata interface {
	InstallGeodata(ctx context.Context, dir string) error
}

// CoreRestarter restarts Hop's own core on the config it was running.
type CoreRestarter interface {
	RestartCurrent(ctx context.Context) error
}

// RuntimeLocker is a subscription service's runtime lock: geodata replacement
// takes it so no config apply runs while the core restarts.
type RuntimeLocker interface {
	LockRuntime()
	UnlockRuntime()
}

// GeodataService downloads the geo databases Hop's rules use, checks them
// with the core before anything replaces a good file, and makes the running
// core read them. The core loads geodata once, at start, so the last step is
// a restart, not a config reload: mihomo v1.19.32's ApplyConfig builds rules
// from its cached matchers and never re-reads the files.
type GeodataService struct {
	source    GeodataDownloader
	validator GeodataValidator
	configs   GeodataConfigs
	store     GeodataStore
	helper    HelperGeodata
	core      CoreRestarter
	tun       *TunFlag
	runtime   RuntimeLocker
	// appDir is Hop's own copy of the databases, which the helper is seeded
	// from; homeDir is the core's working directory; validateDir is where
	// config tests run. All three are updated together.
	appDir      string
	homeDir     string
	validateDir string

	// mu serializes updates: a manual click and the automatic check never
	// download or replace at the same time. lastAttempt is when one last
	// began, which the automatic check backs off from after a failure.
	mu          sync.Mutex
	lastAttempt time.Time
}

// NewGeodataService returns a service downloading through source, checking
// with validator, taking the running config from configs, keeping the last
// success in store, and reaching the helper and Hop's own core through helper
// and core. The databases live in appDir, homeDir and validateDir.
func NewGeodataService(source GeodataDownloader, validator GeodataValidator, configs GeodataConfigs, store GeodataStore, helper HelperGeodata, core CoreRestarter, tun *TunFlag, runtime RuntimeLocker, appDir string, homeDir string, validateDir string) *GeodataService {
	return &GeodataService{
		source: source, validator: validator, configs: configs, store: store,
		helper: helper, core: core, tun: tun, runtime: runtime,
		appDir: appDir, homeDir: homeDir, validateDir: validateDir,
	}
}

// Status reports when the databases last updated; a zero time means they are
// the ones the app ships.
func (s *GeodataService) Status() (model.GeodataStatus, error) {
	at, found, err := s.store.LoadGeodataUpdatedAt()
	if err != nil {
		return model.GeodataStatus{}, err
	}
	if !found {
		return model.GeodataStatus{}, nil
	}
	return model.GeodataStatus{UpdatedAt: at}, nil
}

// Update downloads and checks both databases, then replaces every copy the
// core reads and restarts the core on them. Nothing replaces a good file
// until the downloaded pair passed its published checksums and the core's own
// config test; a failed restart records no success.
func (s *GeodataService) Update(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.updateLocked(ctx)
}

// Due says whether an automatic update is due: the stored last success, or
// before any the seeding of the shipped databases, is a week old, and a failed attempt is not still backing off. The
// dashboard asks before it ends its streams, since an update restarts the
// core.
func (s *GeodataService) Due() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dueLocked()
}

// AutoUpdate updates the databases when Due says so. A failure waits
// geodataRetryBackoff before the next automatic attempt. It returns whether
// an update was applied.
func (s *GeodataService) AutoUpdate(ctx context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	due, err := s.dueLocked()
	if err != nil || !due {
		return false, err
	}
	if err := s.updateLocked(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// dueLocked is Due for a caller holding mu.
func (s *GeodataService) dueLocked() (bool, error) {
	if !s.lastAttempt.IsZero() && time.Since(s.lastAttempt) < geodataRetryBackoff {
		return false, nil
	}
	last, found, err := s.store.LoadGeodataUpdatedAt()
	if err != nil {
		return false, err
	}
	if !found {
		// Never updated: the databases are the ones the app shipped, and
		// they count from when they were seeded into Hop's copy, so a fresh
		// install does not download and restart the core in its first minute.
		info, err := os.Stat(filepath.Join(s.appDir, mihomo.GeoSiteFile))
		if err != nil {
			return false, fmt.Errorf("read when the shipped geodata was seeded: %w", err)
		}
		last = info.ModTime()
	}
	return time.Since(last) >= geodataUpdateEvery, nil
}

// updateLocked is Update for a caller holding mu.
func (s *GeodataService) updateLocked(ctx context.Context) error {
	s.lastAttempt = time.Now()
	if err := os.MkdirAll(s.appDir, 0o700); err != nil {
		return fmt.Errorf("create geodata dir (path=%q): %w", s.appDir, err)
	}
	tmp, err := os.MkdirTemp(s.appDir, "geodata-update-*")
	if err != nil {
		return fmt.Errorf("create geodata staging dir (dir=%q): %w", s.appDir, err)
	}
	defer func() {
		if removeErr := os.RemoveAll(tmp); removeErr != nil {
			slog.Warn("could not remove the geodata staging dir", "dir", tmp, "error", removeErr)
		}
	}()
	if err := s.source.Download(ctx, tmp); err != nil {
		return err
	}
	config, err := s.configs.CurrentConfig(ctx)
	if err != nil {
		return fmt.Errorf("read the running config to check the new geodata: %w", err)
	}
	if err := s.validator.ValidateWithGeodata(ctx, config, tmp); err != nil {
		return fmt.Errorf("the core rejected the downloaded geodata: %w", err)
	}

	// The core and the config file are the subscription service's to change:
	// holding its lock keeps an apply from racing the restart below.
	s.runtime.LockRuntime()
	defer s.runtime.UnlockRuntime()
	for _, dir := range []string{s.appDir, s.homeDir, s.validateDir} {
		if err := mihomo.ReplaceGeodata(tmp, dir, geodataFilePerm); err != nil {
			return err
		}
	}
	if s.tun.On() {
		// The helper's core is root's; only the helper may write its files
		// and restart it, which it does over its socket.
		if err := s.helper.InstallGeodata(ctx, s.appDir); err != nil {
			return fmt.Errorf("give the helper's core the new geodata: %w", err)
		}
	} else {
		if err := s.core.RestartCurrent(ctx); err != nil {
			return fmt.Errorf("restart the core on the new geodata: %w", err)
		}
	}
	now := time.Now()
	if err := s.store.SaveGeodataUpdatedAt(now); err != nil {
		return fmt.Errorf("record the geodata update: %w", err)
	}
	slog.Info("updated the core's geodata", "updated_at", now)
	return nil
}
