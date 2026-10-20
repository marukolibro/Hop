package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"hop/internal/model"
	"hop/internal/repository/helper"
)

const (
	// helperReadyAttempts/helperReadyInterval bound how long a freshly
	// installed helper gets to answer before an enable gives up.
	helperReadyAttempts = 30
	helperReadyInterval = 250 * time.Millisecond
)

// ErrTunTaken is returned when another client's TUN holds the default
// routes: Hop never takes them over.
var ErrTunTaken = errors.New("another TUN holds the default routes")

// HelperControl drives the helper's core over its socket.
type HelperControl interface {
	Version(ctx context.Context) (string, error)
	Status(ctx context.Context) (helper.Status, error)
	InstallCore(ctx context.Context, path string, geodataDir string) error
	StartCore(ctx context.Context, config []byte) error
	StopCore(ctx context.Context) error
}

// HelperInstall installs and removes the helper's LaunchDaemon.
type HelperInstall interface {
	InstalledVersion(ctx context.Context) (string, error)
	Install(ctx context.Context, toolSource string, coreSource string, geodataDir string, uid int) error
	Uninstall(ctx context.Context) error
}

// RouteTable reads the system's routing table, as `netstat -rn -f inet`
// prints it.
type RouteTable interface {
	Read(ctx context.Context) (string, error)
}

// IdleCloser closes the keep-alive connections a client holds to the
// controller.
type IdleCloser interface {
	CloseIdleConnections()
}

// TunRunner is Hop's own core, which TUN mode stops while the helper's runs.
type TunRunner interface {
	Start(config []byte) error
	Stop() error
}

// TunConfigs builds the runtime config with TUN on or off: the startup
// variant for a core nothing else replaces, and the live variant that keeps
// the mode the running core is in.
type TunConfigs interface {
	StartupConfigTun(tun bool) ([]byte, error)
	LiveConfigTun(ctx context.Context, tun bool) ([]byte, error)
}

// TunValidator has the core itself check a config (its own -t), which Hop
// asks before switching cores.
type TunValidator interface {
	Validate(ctx context.Context, config []byte) error
}

// TunFlag is whether TUN is on: the TUN service sets it, and everything
// that builds or applies a config reads it — under TUN the core is the
// helper's and every config must keep the tun section.
type TunFlag struct{ on atomic.Bool }

// NewTunFlag returns a flag that says TUN is off.
func NewTunFlag() *TunFlag { return &TunFlag{} }

// On says whether TUN is on.
func (f *TunFlag) On() bool { return f.on.Load() }

// Set records whether TUN is on; the TUN service is the only writer.
func (f *TunFlag) Set(on bool) { f.on.Store(on) }

// TunService turns TUN mode on and off. On, the core runs under the
// privileged helper as root and takes the default routes over its own TUN
// device; Hop's own core is stopped, and the helper's serves the same
// controller address, so the dashboard carries on against it. Off, the
// helper's core is stopped and Hop's own runs again.
type TunService struct {
	helper    HelperControl
	install   HelperInstall
	routes    RouteTable
	runner    TunRunner
	configs   TunConfigs
	validator TunValidator
	// binary is Hop's current core, which every enable refreshes into the
	// helper; geodata is the directory the app ships its databases in; tool
	// is the helper binary the app ships, what an install copies in.
	binary  string
	geodata string
	tool    string
	// state is whether TUN is on, shared with the services that build and
	// apply configs.
	state *TunFlag
	// controller is Hop's client of the controller, whose idle connections
	// are closed before the helper's core stops.
	controller IdleCloser
}

// NewTunService returns a TUN service whose helper talks on the helper
// client's socket, running binary as the core, seeding geodata from
// geodataDir and installing the helper from toolSource when the user asks
// for one. state is the flag the service sets and the rest of Hop reads.
func NewTunService(helperClient HelperControl, installer HelperInstall, routes RouteTable, runner TunRunner, configs TunConfigs, validator TunValidator, binary string, geodataDir string, toolSource string, state *TunFlag, controller IdleCloser) *TunService {
	return &TunService{
		helper:     helperClient,
		install:    installer,
		routes:     routes,
		runner:     runner,
		configs:    configs,
		validator:  validator,
		binary:     binary,
		geodata:    geodataDir,
		tool:       toolSource,
		state:      state,
		controller: controller,
	}
}

// Status reports what the dashboard shows: whether the helper is installed
// and its version, whether its core runs, and which interface holds the
// default routes when another TUN does.
func (s *TunService) Status(ctx context.Context) (model.TunStatus, error) {
	table, err := s.routes.Read(ctx)
	if err != nil {
		return model.TunStatus{}, fmt.Errorf("read the routing table: %w", err)
	}
	taken, by := TunRouteConflict(table, TunDevice)
	status := model.TunStatus{Taken: taken, TakenBy: by}
	// A helper that does not answer is not installed; anything it did answer
	// is its state proper.
	version, versionErr := s.helper.Version(ctx)
	core, coreErr := s.helper.Status(ctx)
	if versionErr == nil {
		status.HelperInstalled, status.HelperVersion = true, version
		status.HelperOutdated = version != helper.Version
	}
	if coreErr == nil {
		status.CoreRunning = core.Running
	}
	if s.state.On() != status.CoreRunning {
		slog.Warn("TUN's remembered state and the helper's core disagree", "remembered", s.state.On(), "running", status.CoreRunning)
	}
	return status, nil
}

// Enable turns TUN on from the user's click: it installs the helper when
// there is none yet (which asks for the administrator password once),
// refreshes the core and geodata in it, validates the TUN config with the
// core itself, stops Hop's own core and starts the helper's, keeping the
// outbound mode the running core is in. When the helper's core does not come
// up, Hop's own is started again and the error says so.
func (s *TunService) Enable(ctx context.Context) error {
	if err := s.refuseTaken(ctx); err != nil {
		return err
	}
	if err := s.ensureHelper(ctx); err != nil {
		return err
	}
	config, plain, err := s.liveConfigs(ctx)
	if err != nil {
		return err
	}
	return s.switchCore(ctx, config, plain)
}

// ensureHelper installs the helper when none answers, and replaces one that
// speaks another version: an old daemon does not know what the new app asks
// of it (the DNS takeover, among others). A replacement asks for the
// administrator password again.
func (s *TunService) ensureHelper(ctx context.Context) error {
	version, err := s.install.InstalledVersion(ctx)
	if err == nil && version == helper.Version {
		return nil
	}
	if err == nil {
		slog.Info("replacing the helper with this app's version", "installed", version, "want", helper.Version)
	}
	if err := s.install.Install(ctx, s.tool, s.binary, s.geodata, os.Getuid()); err != nil {
		return fmt.Errorf("install the helper: %w", err)
	}
	// launchd starts the daemon after the install script returns; the first
	// request would otherwise race it.
	if err := s.waitHelper(ctx); err != nil {
		return fmt.Errorf("install the helper: %w", err)
	}
	return nil
}

// enableCold turns TUN on at start, before any core of Hop's runs: the
// config comes from the saved startup mode, and a failure has no proxy to
// put back — the caller starts Hop's own core.
func (s *TunService) enableCold(ctx context.Context) error {
	if err := s.refuseTaken(ctx); err != nil {
		return err
	}
	config, err := s.configs.StartupConfigTun(true)
	if err != nil {
		return fmt.Errorf("build the TUN config: %w", err)
	}
	return s.switchCore(ctx, config, nil)
}

// switchCore hands the traffic to the helper's core: it refreshes the core
// and geodata in the helper, stops Hop's own and starts the helper's on
// config. rollbackConfig, when not nil, is what Hop's own core is started
// on again when the helper's does not come up.
func (s *TunService) switchCore(ctx context.Context, config []byte, rollbackConfig []byte) error {
	if err := s.validator.Validate(ctx, config); err != nil {
		return fmt.Errorf("the core's config test rejected the TUN config: %w", err)
	}
	if err := s.helper.InstallCore(ctx, s.binary, s.geodata); err != nil {
		return fmt.Errorf("refresh the core in the helper: %w", err)
	}
	if err := s.runner.Stop(); err != nil {
		return fmt.Errorf("stop Hop's own core: %w", err)
	}
	if err := s.helper.StartCore(ctx, config); err != nil {
		startErr := fmt.Errorf("start the core under the helper: %w", err)
		if rollbackConfig == nil {
			return startErr
		}
		if err := s.runner.Start(rollbackConfig); err != nil {
			return errors.Join(startErr, fmt.Errorf("start Hop's own core again: %w", err))
		}
		return startErr
	}
	s.state.Set(true)
	return nil
}

// liveConfigs builds the TUN config and its plain counterpart, both on the
// outbound mode the core running now reports.
func (s *TunService) liveConfigs(ctx context.Context) (tun []byte, plain []byte, err error) {
	tun, err = s.configs.LiveConfigTun(ctx, true)
	if err != nil {
		return nil, nil, fmt.Errorf("build the TUN config: %w", err)
	}
	plain, err = s.configs.LiveConfigTun(ctx, false)
	if err != nil {
		return nil, nil, fmt.Errorf("build the config without TUN: %w", err)
	}
	return tun, plain, nil
}

// refuseTaken returns ErrTunTaken when another client's TUN holds the
// default routes.
func (s *TunService) refuseTaken(ctx context.Context) error {
	taken, err := s.taken(ctx)
	if err != nil {
		return err
	}
	if taken {
		return ErrTunTaken
	}
	return nil
}

// Disable turns TUN off: the helper's core is stopped and Hop's own runs
// again on the config with TUN off, keeping the mode the core runs in.
func (s *TunService) Disable(ctx context.Context) error {
	// The config is built while a core still answers the mode read.
	config, err := s.configs.LiveConfigTun(ctx, false)
	if err != nil {
		return fmt.Errorf("build the config without TUN: %w", err)
	}
	// Hop closes its connections to the root core first: closed by the core,
	// they would hold the controller's port from Hop's own core for 30 s.
	s.controller.CloseIdleConnections()
	if err := s.helper.StopCore(ctx); err != nil {
		return fmt.Errorf("stop the helper's core: %w", err)
	}
	s.state.Set(false)
	if err := s.runner.Start(config); err != nil {
		return fmt.Errorf("start Hop's own core: %w", err)
	}
	return nil
}

// Startup brings TUN to the state the saved setting asks for, before Hop's
// own core starts. A helper core left running by a Hop that crashed is
// adopted when the setting says TUN on, and stopped when it does not.
// Turning TUN on never installs anything here: an administrator password is
// asked for only on the user's click. It returns whether TUN is on now.
func (s *TunService) Startup(ctx context.Context, wanted bool) (bool, error) {
	version, versionErr := s.install.InstalledVersion(ctx)
	installed := versionErr == nil
	if installed && version != helper.Version {
		// The daemon speaks another protocol, so it cannot be trusted with
		// TUN. Its core may hold the ports and the routes: stop it and leave
		// TUN off; the user updates the helper by turning TUN on.
		slog.Warn("the installed helper is another version; leaving TUN off until it is updated", "installed", version, "want", helper.Version)
		if err := s.helper.StopCore(ctx); err != nil {
			return false, fmt.Errorf("stop the helper's core before updating it: %w", err)
		}
		return false, nil
	}
	core, err := s.helper.Status(ctx)
	if err != nil && installed {
		return false, fmt.Errorf("read the helper's state: %w", err)
	}
	if installed && core.Running {
		if wanted {
			slog.Info("adopting the helper's core left running at start")
			s.state.Set(true)
			return true, nil
		}
		slog.Info("stopping the helper's core at start: TUN is off in the settings")
		if err := s.helper.StopCore(ctx); err != nil {
			return false, fmt.Errorf("stop the helper's leftover core: %w", err)
		}
		return false, nil
	}
	if !wanted || !installed {
		return false, nil
	}
	// The refusal rules of the pill apply at start too: another TUN holding
	// the routes leaves TUN off, and so does an enable that fails.
	taken, err := s.taken(ctx)
	if err != nil {
		return false, err
	}
	if taken {
		slog.Warn("another TUN holds the default routes; leaving TUN off at start")
		return false, nil
	}
	// Cold start: Hop's own core is not running yet, so a failure has no
	// proxy to put back — the caller starts its own core.
	if err := s.enableCold(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// Release stops the helper's core at quit, when TUN is on: the TUN device
// goes away with it and apps fall back to their direct routes.
func (s *TunService) Release(ctx context.Context) error {
	if !s.state.On() {
		return nil
	}
	if err := s.helper.StopCore(ctx); err != nil {
		return fmt.Errorf("stop the helper's core: %w", err)
	}
	s.state.Set(false)
	return nil
}

// Uninstall removes the helper entirely. A TUN that is on is turned off
// first, so Hop's own core carries the traffic again before the helper goes.
func (s *TunService) Uninstall(ctx context.Context) error {
	if s.state.On() {
		if err := s.Disable(ctx); err != nil {
			return fmt.Errorf("turn TUN off before uninstalling: %w", err)
		}
	}
	if err := s.install.Uninstall(ctx); err != nil {
		return fmt.Errorf("uninstall the helper: %w", err)
	}
	s.state.Set(false)
	return nil
}

// taken says whether another client's TUN holds the default routes now.
func (s *TunService) taken(ctx context.Context) (bool, error) {
	table, err := s.routes.Read(ctx)
	if err != nil {
		return false, fmt.Errorf("read the routing table: %w", err)
	}
	taken, _ := TunRouteConflict(table, TunDevice)
	return taken, nil
}

// waitHelper waits for a freshly installed helper to answer, retrying with a
// warning for each attempt and returning the last error when it never does.
func (s *TunService) waitHelper(ctx context.Context) error {
	var last error
	for attempt := 1; attempt <= helperReadyAttempts; attempt++ {
		if _, err := s.helper.Version(ctx); err == nil {
			return nil
		} else {
			last = err
			slog.Warn("the helper is not answering yet", "attempt", attempt, "max_attempts", helperReadyAttempts, "error", err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for the helper: %w", ctx.Err())
		case <-time.After(helperReadyInterval):
		}
	}
	return fmt.Errorf("the helper did not answer after %d attempts: %w", helperReadyAttempts, last)
}

// defaultRouteKeys are the destinations a TUN's auto-route takes over to
// capture the default path: the plain default plus the half-space split and
// the subnet-chain splits of it that macOS TUN clients use.
var defaultRouteKeys = map[string]struct{}{
	"default": {}, "0/1": {}, "128.0/1": {},
	"1": {}, "2/7": {}, "4/6": {}, "8/5": {}, "16/4": {}, "32/3": {}, "64/2": {},
}

// TunRouteConflict reads the routing table netstat printed (netstat -rn -f
// inet) and says whether a TUN interface other than ownDevice holds any
// default-ish route: another client's TUN is up, and Hop never takes the
// routes over it. The interface that holds them names the conflict.
func TunRouteConflict(netstatOut string, ownDevice string) (bool, string) {
	for _, line := range strings.Split(netstatOut, "\n") {
		fields := strings.Fields(line)
		// A route line is destination, gateway, flags, interface, and an
		// optional expire column; anything shorter is a header or a note.
		if len(fields) < 4 {
			continue
		}
		if _, ok := defaultRouteKeys[fields[0]]; !ok {
			continue
		}
		iface := fields[3]
		if strings.HasPrefix(iface, "utun") && iface != ownDevice {
			return true, iface
		}
	}
	return false, ""
}
