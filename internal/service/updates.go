package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"hop/internal/model"
)

// appUpdateCheckEvery is how often a running Hop checks for a new version
// after the first check of the session.
const appUpdateCheckEvery = 6 * time.Hour

// ErrNoAppUpdate is returned by Install when the release feed no longer
// offers a version newer than the running one.
var ErrNoAppUpdate = errors.New("no newer version is published")

// appUpdateCheckTimeout bounds one manifest check, including the GitHub API
// hop a tagged feed takes. Downloads are separate and may take longer.
const appUpdateCheckTimeout = 30 * time.Second

// AppUpdateService wraps mygo's signed updater for the dashboard. The
// updater reads the public key built into the app, checks the manifests of
// the latest GitHub release, verifies the Ed25519 signature of the archive
// it downloads, and replaces the app bundle. It fetches GitHub with the
// standard library's default client, directly, not through Hop's proxy.
type AppUpdateService struct {
	// mu guards the check clock. checkMu serializes the checks themselves.
	mu             sync.Mutex
	checkMu        sync.Mutex
	checkedThisRun bool
	lastCheck      time.Time
}

// NewAppUpdateService returns a service over mygo.Updater.
func NewAppUpdateService() *AppUpdateService {
	return &AppUpdateService{}
}

// Enabled reports whether this build can update itself: it was built with
// the update public key, is not a development build, and can write where it
// is installed. mygo does not tell which of those failed, so the row's
// reason stays approximate.
func (s *AppUpdateService) Enabled() bool {
	return mygo.Updater.Enabled()
}

// DisabledReason is the short Chinese reason shown when Enabled is false.
func (s *AppUpdateService) DisabledReason() string {
	if s.Enabled() {
		return ""
	}
	switch {
	case mygo.IsDev():
		return "开发版不能自动更新"
	case !mygo.App.IsPackaged():
		return "未打包运行，不能自动更新"
	default:
		return "应用目录不可写，不能自动更新"
	}
}

// Due reports whether the automatic check should run: the first pass of
// this session always checks, then the in-memory last check keeps it to
// every few hours, since a proxy client stays open for days. The time is not
// stored, so a restart checks once more.
func (s *AppUpdateService) Due() (bool, error) {
	if !s.Enabled() {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.checkedThisRun {
		return true, nil
	}
	return time.Since(s.lastCheck) >= appUpdateCheckEvery, nil
}

// Check asks mygo's updater for a newer release. It returns Available false
// when the app is up to date, and Notes from the release's changelog
// section when it is. ErrUpdatesDisabled is returned when the build cannot
// update itself; the dashboard hides the check instead of showing it.
func (s *AppUpdateService) Check(ctx context.Context) (model.AppUpdate, error) {
	if !s.Enabled() {
		return model.AppUpdate{}, mygo.ErrUpdatesDisabled
	}
	s.mu.Lock()
	s.checkedThisRun = true
	s.lastCheck = time.Now()
	s.mu.Unlock()

	update, err := s.fetch(ctx)
	if err != nil {
		return model.AppUpdate{}, err
	}
	if update == nil {
		return model.AppUpdate{}, nil
	}
	return model.AppUpdate{Available: true, Version: update.Version, Notes: update.Notes}, nil
}

// fetch asks mygo's updater for a newer release; nil means the app is up to
// date.
func (s *AppUpdateService) fetch(ctx context.Context) (*mygo.Update, error) {
	s.checkMu.Lock()
	defer s.checkMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, appUpdateCheckTimeout)
	defer cancel()
	return mygo.Updater.Check(ctx)
}

// Install downloads and installs the newest version, reporting the bytes
// downloaded. It checks again first: an app left open for days still holds
// the offer of an older check, and a release published since then must not be
// passed over. It returns ErrNoAppUpdate when nothing newer is published. It
// replaces the app bundle but does not quit or relaunch: the caller uses
// mygo.App.Relaunch so the normal quit releases the system proxy and any
// helper core first.
func (s *AppUpdateService) Install(ctx context.Context, progress func(downloaded int64, total int64)) error {
	if !s.Enabled() {
		return mygo.ErrUpdatesDisabled
	}
	update, err := s.fetch(ctx)
	if err != nil {
		return err
	}
	if update == nil {
		return ErrNoAppUpdate
	}
	return update.Install(ctx, progress)
}
