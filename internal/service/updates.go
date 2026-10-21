package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"hop/internal/model"
)

// appUpdateCheckEvery is how often a running Hop checks for a new version
// after the first check of the session.
const appUpdateCheckEvery = 24 * time.Hour

// appUpdateCheckTimeout bounds one manifest check, including the GitHub API
// hop a tagged feed takes. Downloads are separate and may take longer.
const appUpdateCheckTimeout = 30 * time.Second

// AppUpdateService wraps mygo's signed updater for the dashboard. The
// updater reads the public key built into the app, checks the manifests of
// the latest GitHub release, verifies the Ed25519 signature of the archive
// it downloads, and replaces the app bundle. It fetches GitHub with the
// standard library's default client, directly, not through Hop's proxy.
type AppUpdateService struct {
	// mu guards the check clock and the update Check found. checkMu
	// serializes the checks themselves.
	mu             sync.Mutex
	checkMu        sync.Mutex
	checkedThisRun bool
	lastCheck      time.Time
	found          *mygo.Update
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
// once a day. The time is not stored, so a restart checks once more.
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

	s.checkMu.Lock()
	defer s.checkMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, appUpdateCheckTimeout)
	defer cancel()
	update, err := mygo.Updater.Check(ctx)
	if err != nil {
		return model.AppUpdate{}, err
	}
	s.mu.Lock()
	s.found = update
	s.mu.Unlock()
	if update == nil {
		return model.AppUpdate{}, nil
	}
	return model.AppUpdate{Available: true, Version: update.Version, Notes: update.Notes}, nil
}

// Install downloads and installs the update version that Check found,
// reporting the bytes downloaded. It replaces the app bundle but does not
// quit or relaunch: the caller uses mygo.App.Relaunch so the normal quit
// releases the system proxy and any helper core first.
func (s *AppUpdateService) Install(ctx context.Context, version string, progress func(downloaded int64, total int64)) error {
	if !s.Enabled() {
		return mygo.ErrUpdatesDisabled
	}
	s.mu.Lock()
	update := s.found
	s.mu.Unlock()
	if update == nil || update.Version != version {
		return fmt.Errorf("no checked update is named %q", version)
	}
	return update.Install(ctx, progress)
}
