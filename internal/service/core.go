package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"hop/internal/model"
)

const rollbackRestartTimeout = 30 * time.Second

// ErrCoreUpToDate is returned when an upgrade is asked for and the running
// core is not older than the newest release.
var ErrCoreUpToDate = errors.New("the running mihomo core is already the newest release")

// ErrCoreNotUpgradable is returned when the core was chosen by the
// developer's environment variable, which an upgrade could not replace.
var ErrCoreNotUpgradable = errors.New("the running core comes from HOP_MIHOMO_BINARY; unset it to let Hop manage the core")

// ErrCoreLockedByTun is returned when the core is asked to change while TUN
// is on: the helper's root core owns the ports, and Hop's own core could not
// start beside it.
var ErrCoreLockedByTun = errors.New("the core cannot be changed while TUN is on; turn TUN off first")

// CoreVersionReader asks the running core what version it is.
type CoreVersionReader interface {
	Version(ctx context.Context) (string, error)
}

// CoreReleases finds and downloads mihomo releases.
type CoreReleases interface {
	Latest(ctx context.Context) (model.CoreRelease, error)
	Release(ctx context.Context, tag string) (model.CoreRelease, error)
	Download(ctx context.Context, release model.CoreRelease, archivePath string, binaryPath string, progress func(done int64, total int64)) error
	DownloadArchive(ctx context.Context, release model.CoreRelease, archivePath string, progress func(done int64, total int64)) error
}

// CoreInstalls keeps the cores Hop downloaded.
type CoreInstalls interface {
	BinaryPath(tag string) (string, error)
	ArchivePath(tag string) (string, error)
	KeptArchive(tag string) (string, bool, error)
	Activate(tag string) error
	Deactivate() error
	Remove(tag string) error
	Prune(keepTag string) error
}

// CoreProcess replaces the running core with another binary.
type CoreProcess interface {
	Restart(ctx context.Context, binary string) error
}

// CoreChecker runs a candidate core on the running config before it takes
// over, and returns the version it reports.
type CoreChecker interface {
	Check(ctx context.Context, binary string) (string, error)
}

// CoreService reports which mihomo core runs and upgrades it.
type CoreService struct {
	version  CoreVersionReader
	releases CoreReleases
	installs CoreInstalls
	process  CoreProcess
	checker  CoreChecker
	// tun says whether TUN is on: the helper's core runs then, and a change
	// of Hop's own core must wait.
	tun *TunFlag

	// mu serializes upgrades with each other and with reads of the state
	// they change.
	mu            sync.Mutex
	binary        string
	bundledBinary string
	origin        model.CoreOrigin
}

// NewCoreService returns a service for a core started from binary of origin;
// bundledBinary is the core that ships with the app, and tun says whether
// TUN is on.
func NewCoreService(version CoreVersionReader, releases CoreReleases, installs CoreInstalls, process CoreProcess, checker CoreChecker, binary string, bundledBinary string, origin model.CoreOrigin, tun *TunFlag) *CoreService {
	return &CoreService{
		version: version, releases: releases, installs: installs, process: process, checker: checker,
		binary: binary, bundledBinary: bundledBinary, origin: origin, tun: tun,
	}
}

// Info returns the version the running core reports and where it came from.
func (s *CoreService) Info(ctx context.Context) (model.CoreInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.info(ctx)
}

func (s *CoreService) info(ctx context.Context) (model.CoreInfo, error) {
	version, err := s.version.Version(ctx)
	if err != nil {
		return model.CoreInfo{}, fmt.Errorf("read the running core's version: %w", err)
	}
	return model.CoreInfo{Version: version, Origin: s.origin}, nil
}

// CheckLatest returns the newest release and whether the running core is
// older than it.
func (s *CoreService) CheckLatest(ctx context.Context) (model.CoreRelease, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	info, err := s.info(ctx)
	if err != nil {
		return model.CoreRelease{}, false, err
	}
	release, err := s.releases.Latest(ctx)
	if err != nil {
		return model.CoreRelease{}, false, err
	}
	return release, isNewer(release.Tag, info.Version), nil
}

// Upgrade downloads the newest release, checks it runs the current config,
// and restarts the core on it. When the new core does not come up, the old
// one is started again, so a failed upgrade ends where it began. progress
// hears each step; the proxy is unavailable for the restart, a second or two.
func (s *CoreService) Upgrade(ctx context.Context, progress func(model.CoreProgress)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tun.On() {
		return ErrCoreLockedByTun
	}
	if s.origin == model.CoreOverride {
		return ErrCoreNotUpgradable
	}
	info, err := s.info(ctx)
	if err != nil {
		return err
	}
	release, err := s.releases.Latest(ctx)
	if err != nil {
		return err
	}
	if !isNewer(release.Tag, info.Version) {
		return ErrCoreUpToDate
	}
	binary, err := s.installs.BinaryPath(release.Tag)
	if err != nil {
		return err
	}
	archive, err := s.installs.ArchivePath(release.Tag)
	if err != nil {
		return err
	}

	progress(model.CoreProgress{Stage: model.CoreStageDownload, Total: release.Size})
	err = s.releases.Download(ctx, release, archive, binary, func(done int64, total int64) {
		progress(model.CoreProgress{Stage: model.CoreStageDownload, Done: done, Total: total})
	})
	if err != nil {
		return s.discard(release.Tag, err)
	}

	progress(model.CoreProgress{Stage: model.CoreStageVerify})
	reported, err := s.checker.Check(ctx, binary)
	if err != nil {
		return s.discard(release.Tag, err)
	}
	if reported != release.Tag {
		return s.discard(release.Tag, fmt.Errorf("the downloaded core reports %s, not the release %s it was downloaded as", reported, release.Tag))
	}

	progress(model.CoreProgress{Stage: model.CoreStageRestart})
	if err := s.process.Restart(ctx, binary); err != nil {
		return s.discard(release.Tag, s.restore(s.binary, fmt.Errorf("the new core did not start: %w", err)))
	}
	if err := s.installs.Activate(release.Tag); err != nil {
		return s.discard(release.Tag, s.restore(s.binary, fmt.Errorf("remember the new core: %w", err)))
	}
	s.binary, s.origin = binary, model.CoreInstalled
	// The upgrade worked; what is left over is only disk space.
	if err := s.installs.Prune(release.Tag); err != nil {
		slog.Warn("could not remove older downloaded cores", "error", err)
	}
	return nil
}

// HelperArchive returns the release archive of the core running now, which
// the privileged helper verifies against GitHub before it runs that core
// itself. An upgraded core kept its archive; for any other, such as the
// bundled one, the archive of the same release is downloaded once and kept.
func (s *CoreService) HelperArchive(ctx context.Context) (model.CoreArchive, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	info, err := s.info(ctx)
	if err != nil {
		return model.CoreArchive{}, err
	}
	path, kept, err := s.installs.KeptArchive(info.Version)
	if err != nil {
		return model.CoreArchive{}, fmt.Errorf("the running core cannot run under the helper: %w", err)
	}
	if kept {
		return model.CoreArchive{Tag: info.Version, Path: path}, nil
	}
	release, err := s.releases.Release(ctx, info.Version)
	if err != nil {
		return model.CoreArchive{}, err
	}
	if err := s.releases.DownloadArchive(ctx, release, path, func(int64, int64) {}); err != nil {
		return model.CoreArchive{}, err
	}
	return model.CoreArchive{Tag: info.Version, Path: path}, nil
}

// RestoreBundled goes back to the core that ships with the app, forgetting
// the upgraded one.
func (s *CoreService) RestoreBundled(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tun.On() {
		return ErrCoreLockedByTun
	}
	if s.origin != model.CoreInstalled {
		return errors.New("the bundled core is already running")
	}
	if s.bundledBinary == "" {
		return errors.New("this build has no bundled core to go back to")
	}
	previous := s.binary
	if err := s.process.Restart(ctx, s.bundledBinary); err != nil {
		return s.restore(previous, fmt.Errorf("the bundled core did not start: %w", err))
	}
	if err := s.installs.Deactivate(); err != nil {
		return s.restore(previous, fmt.Errorf("forget the upgraded core: %w", err))
	}
	s.binary, s.origin = s.bundledBinary, model.CoreBundled
	if err := s.installs.Prune(""); err != nil {
		slog.Warn("could not remove the downloaded cores", "error", err)
	}
	return nil
}

// restore starts the core that was running before a failed change, on its
// own bounded context, and says in the error whether that worked.
func (s *CoreService) restore(binary string, cause error) error {
	ctx, cancel := context.WithTimeout(context.Background(), rollbackRestartTimeout)
	defer cancel()
	if err := s.process.Restart(ctx, binary); err != nil {
		return errors.Join(cause, fmt.Errorf("putting the previous core back failed, the proxy is not running: %w", err))
	}
	return fmt.Errorf("%w (the previous core was put back)", cause)
}

// discard removes a core that failed on its way in and returns cause with
// any failure of that cleanup.
func (s *CoreService) discard(tag string, cause error) error {
	if err := s.installs.Remove(tag); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

// isNewer reports whether release is a later version than current. Versions
// that are not plain "vMAJOR.MINOR.PATCH" differ if they are not equal.
func isNewer(release string, current string) bool {
	r, rok := parseVersion(release)
	c, cok := parseVersion(current)
	if !rok || !cok {
		return release != current
	}
	for i := range r {
		if r[i] != c[i] {
			return r[i] > c[i]
		}
	}
	return false
}

func parseVersion(v string) ([3]int, bool) {
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	var out [3]int
	if len(parts) != len(out) {
		return out, false
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
