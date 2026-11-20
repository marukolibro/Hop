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
	"hop/internal/repository/routetable"
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
	InstallCore(ctx context.Context, archive model.CoreArchive, proxy string) error
	InstallGeodata(ctx context.Context, geodataDir string) error
	StartCore(ctx context.Context, config []byte) error
	ApplyConfig(ctx context.Context, config []byte) error
	StopCore(ctx context.Context) error
}

// HelperInstall installs and removes the helper's LaunchDaemon on macOS or
// its Windows service.
type HelperInstall interface {
	InstalledVersion(ctx context.Context) (string, error)
	Install(ctx context.Context, toolSource string, coreSource string, geodataDir string, uid int) error
	Uninstall(ctx context.Context) error
}

// TunCores says which release Hop chose to run and hands over its archive,
// the one form of a core the helper accepts.
type TunCores interface {
	ChosenVersion(ctx context.Context) (string, error)
	HelperArchive(ctx context.Context) (model.CoreArchive, error)
}

// RouteTable reads the system's IPv4 routing table.
type RouteTable interface {
	Read(ctx context.Context) (routetable.Table, error)
}

// ControllerConns closes the connections Hop holds to the controller: its
// keep-alive connections and its streams. Hop closes them itself before the
// helper's core stops: a connection the root core ends first holds the
// controller's port from Hop's own core for TIME_WAIT, 30 s on macOS.
type ControllerConns interface {
	CloseIdleConnections()
	CloseStreams()
}

// TunRunner is Hop's own core, which stops for good once the helper runs
// the core. ReapLeftover stops one an earlier Hop left running, which would
// hold the ports the helper's core needs.
type TunRunner interface {
	Start(config []byte) error
	Stop() error
	ReapLeftover() error
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

// HelperCoreFlag is whether Hop's core runs under the privileged helper, as
// root. Once the helper is installed it always does, TUN on or off, so
// turning TUN on or off reloads that one core and its ports never pass
// between root and the user: a port root's core closed connections on stays
// from a core the user runs for TIME_WAIT, 30 s on macOS. The TUN service
// sets it, and everything that reaches the running core reads it.
type HelperCoreFlag struct{ on atomic.Bool }

// NewHelperCoreFlag returns a flag that says Hop runs its own core.
func NewHelperCoreFlag() *HelperCoreFlag { return &HelperCoreFlag{} }

// On says whether the core runs under the helper.
func (f *HelperCoreFlag) On() bool { return f.on.Load() }

// Set records where the core runs; the TUN service is the only writer.
func (f *HelperCoreFlag) Set(on bool) { f.on.Store(on) }

// TunService runs the core under the privileged helper and turns TUN on and
// off in it. Once the helper is installed, the core runs there as root, TUN
// on or off, on the same controller address, so the dashboard reaches it as
// it would Hop's own; TUN on, it takes the default routes over its own TUN
// device. Without the helper, Hop runs its own core and TUN is off; the first
// time TUN is turned on, the helper is installed and the core moves into it.
type TunService struct {
	helper    HelperControl
	install   HelperInstall
	routes    RouteTable
	runner    TunRunner
	configs   TunConfigs
	validator TunValidator
	cores     TunCores
	// port is Hop's mixed port, the proxy the helper asks GitHub through.
	port *MixedPortState
	// bundled is the core that ships with the app, what an install of the
	// helper copies in with the administrator's approval; geodata is the
	// directory the app ships its databases in; tool is the helper binary
	// the app ships, what an install copies in.
	bundled string
	geodata string
	tool    string
	// state is whether TUN is on, shared with the services that build and
	// apply configs; placement is whether the core runs under the helper,
	// shared with the services that reach the running core.
	state     *TunFlag
	placement *HelperCoreFlag
	// controller is Hop's client of the controller, whose idle connections
	// are closed before the helper's core stops.
	controller ControllerConns
	// disagree is whether the last Status found the remembered state and the
	// helper's core apart, so a lasting disagreement is logged once.
	disagree atomic.Bool
}

// NewTunService returns a TUN service whose helper talks on the helper
// client's socket, running the core cores reports, seeding geodata from
// geodataDir and installing the helper from toolSource with bundledCore when
// the user asks for one. state and placement are the flags the service sets
// and the rest of Hop reads.
func NewTunService(helperClient HelperControl, installer HelperInstall, routes RouteTable, runner TunRunner, configs TunConfigs, validator TunValidator, cores TunCores, port *MixedPortState, bundledCore string, geodataDir string, toolSource string, state *TunFlag, placement *HelperCoreFlag, controller ControllerConns) *TunService {
	return &TunService{
		helper:     helperClient,
		install:    installer,
		routes:     routes,
		runner:     runner,
		configs:    configs,
		validator:  validator,
		cores:      cores,
		port:       port,
		bundled:    bundledCore,
		geodata:    geodataDir,
		tool:       toolSource,
		state:      state,
		placement:  placement,
		controller: controller,
	}
}

// Status reports what the dashboard shows: whether the helper is installed
// and its version, whether its core runs TUN, and which interface holds the
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
		status.On = core.Running && core.Tun
		status.CoreRestarts = core.Restarts
		if core.LastRestart != nil {
			status.LastCoreRestart = *core.LastRestart
		}
	}
	s.noteAgreement(s.state.On(), status.On)
	return status, nil
}

// noteAgreement logs when TUN's remembered state and the helper's core start
// or stop disagreeing. Status runs every few seconds, so a disagreement that
// lasts is logged once, not on every poll.
func (s *TunService) noteAgreement(remembered bool, running bool) {
	disagree := remembered != running
	if s.disagree.Swap(disagree) == disagree {
		return
	}
	if disagree {
		slog.Warn("TUN's remembered state and the helper's core disagree", "remembered", remembered, "helper_tun", running)
		return
	}
	slog.Info("TUN's remembered state and the helper's core agree again", "on", remembered)
}

// Enable turns TUN on from the user's click. With the core under the helper
// that is one reload of it (reloadTun). Otherwise it installs the helper when
// there is none yet, or replaces one of another version (asking for
// administrator approval once — a password on macOS, a UAC prompt on
// Windows), puts the release Hop runs into it when the helper has another,
// and moves the core into the helper with TUN on, keeping the outbound mode
// the running core is in. When the helper's core does not come up, Hop's own
// is started again and the error says so.
func (s *TunService) Enable(ctx context.Context) error {
	if err := s.refuseTaken(ctx); err != nil {
		return err
	}
	if s.placement.On() {
		return s.reloadTun(ctx, true)
	}
	if err := s.ensureHelper(ctx); err != nil {
		return err
	}
	config, plain, err := s.liveConfigs(ctx)
	if err != nil {
		return err
	}
	if err := s.refreshCore(ctx); err != nil {
		return err
	}
	return s.moveToHelper(ctx, config, plain)
}

// reloadTun turns TUN on or off in the core the helper runs: one reload of
// that core, which keeps its ports, its connections and the outbound mode it
// runs in, while the helper moves the system DNS with it.
func (s *TunService) reloadTun(ctx context.Context, on bool) error {
	config, err := s.configs.LiveConfigTun(ctx, on)
	if err != nil {
		return fmt.Errorf("build the config (tun=%t): %w", on, err)
	}
	if err := s.validator.Validate(ctx, config); err != nil {
		return fmt.Errorf("the core's config test rejected the config (tun=%t): %w", on, err)
	}
	if err := s.helper.ApplyConfig(ctx, config); err != nil {
		return fmt.Errorf("reload the helper's core (tun=%t): %w", on, err)
	}
	s.state.Set(on)
	return nil
}

// CoreBehind says whether the core runs under the helper on another release
// than the one Hop chose, as after an app update that ships a newer core: the
// helper keeps its own copy, which only an install replaces (FollowCore).
func (s *TunService) CoreBehind(ctx context.Context) (bool, error) {
	if !s.placement.On() {
		return false, nil
	}
	chosen, installed, err := s.coreVersions(ctx)
	if err != nil {
		return false, err
	}
	return chosen != installed, nil
}

// FollowCore has the helper run the release Hop chose, restarting its core
// on it (refreshCore).
func (s *TunService) FollowCore(ctx context.Context) error {
	return s.refreshCore(ctx)
}

// refreshCore has the helper run the release Hop chose (bundled or
// upgraded): when the helper has another release, it gets this one's GitHub
// archive, which it verifies against GitHub itself before installing, asking
// through Hop's own proxy; a core it runs is restarted on it. The proxy must
// answer, so this runs while a core does: Hop's own before the core moves
// into the helper, or the helper's.
func (s *TunService) refreshCore(ctx context.Context) error {
	chosen, installed, err := s.coreVersions(ctx)
	if err != nil {
		return err
	}
	if chosen == installed {
		return nil
	}
	archive, err := s.cores.HelperArchive(ctx)
	if err != nil {
		return fmt.Errorf("get the release archive of core %s for the helper: %w", chosen, err)
	}
	if err := s.helper.InstallCore(ctx, archive, s.port.ProxyURL()); err != nil {
		return fmt.Errorf("install core %s into the helper (had %q): %w", archive.Tag, installed, err)
	}
	slog.Info("installed the chosen core into the helper", "tag", archive.Tag, "previous", installed)
	return nil
}

// coreVersions returns the release Hop chose and the one the helper has
// installed.
func (s *TunService) coreVersions(ctx context.Context) (chosen string, installed string, err error) {
	chosen, err = s.cores.ChosenVersion(ctx)
	if err != nil {
		return "", "", fmt.Errorf("read the version of the core Hop chose: %w", err)
	}
	status, err := s.helper.Status(ctx)
	if err != nil {
		return "", "", fmt.Errorf("read the helper's core version: %w", err)
	}
	return chosen, status.CoreVersion, nil
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
	if err := s.install.Install(ctx, s.tool, s.bundled, s.geodata, os.Getuid()); err != nil {
		return fmt.Errorf("install the helper: %w", err)
	}
	// launchd starts the daemon after the install script returns; the first
	// request would otherwise race it.
	if err := s.waitHelper(ctx); err != nil {
		return fmt.Errorf("install the helper: %w", err)
	}
	return nil
}

// moveToHelper hands the core over to the helper with TUN on, the one move
// from a core the user runs to one root runs: root may bind the ports Hop's
// own core just closed connections on at once. It stops Hop's own core and
// starts the helper's on config; when that does not come up, Hop's own is
// started again on rollbackConfig.
func (s *TunService) moveToHelper(ctx context.Context, config []byte, rollbackConfig []byte) error {
	if err := s.prepareHelperCore(ctx, config); err != nil {
		return err
	}
	if err := s.runner.Stop(); err != nil {
		return fmt.Errorf("stop Hop's own core: %w", err)
	}
	if err := s.helper.StartCore(ctx, config); err != nil {
		startErr := fmt.Errorf("start the core under the helper: %w", err)
		if err := s.runner.Start(rollbackConfig); err != nil {
			return errors.Join(startErr, fmt.Errorf("start Hop's own core again: %w", err))
		}
		return startErr
	}
	s.placement.Set(true)
	s.state.Set(true)
	return nil
}

// prepareHelperCore gets the helper ready to start a core on config: the core
// itself tests the config, and the helper's geodata is replaced with Hop's
// current copy, which an app update may have replaced since.
func (s *TunService) prepareHelperCore(ctx context.Context, config []byte) error {
	if err := s.validator.Validate(ctx, config); err != nil {
		return fmt.Errorf("the core's config test rejected the config for the helper: %w", err)
	}
	if err := s.helper.InstallGeodata(ctx, s.geodata); err != nil {
		return fmt.Errorf("refresh the geodata in the helper: %w", err)
	}
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

// Disable turns TUN off from the user's click: one reload of the core the
// helper runs, which goes on running there (reloadTun). TUN runs only there,
// so with Hop's own core running there is nothing to turn off.
func (s *TunService) Disable(ctx context.Context) error {
	if !s.placement.On() {
		s.state.Set(false)
		return nil
	}
	return s.reloadTun(ctx, false)
}

// Startup settles where the core runs before Hop starts one, and returns
// whether it runs under the helper; when it does not, the caller starts Hop's
// own. With the helper installed in this app's version the core runs there,
// with TUN on when the saved setting asks for it and no other client's TUN
// holds the routes: a core a crashed Hop left running there is adopted as it
// is, and otherwise one is started. A helper of another version speaks
// another protocol and cannot be trusted with the core: its core, which may
// hold the ports and the routes, is stopped, and the user updates the helper
// by turning TUN on. Nothing is installed here: an administrator password is
// asked for only on the user's click.
func (s *TunService) Startup(ctx context.Context, wanted bool) (bool, error) {
	version, err := s.install.InstalledVersion(ctx)
	if err != nil {
		return false, nil
	}
	if version != helper.Version {
		slog.Warn("the installed helper is another version; running Hop's own core until it is updated", "installed", version, "want", helper.Version)
		if err := s.stopHelperCore(ctx); err != nil {
			return false, fmt.Errorf("stop the helper's core before updating it: %w", err)
		}
		return false, nil
	}
	core, err := s.helper.Status(ctx)
	if err != nil {
		return false, fmt.Errorf("read the helper's state: %w", err)
	}
	if core.Running {
		slog.Info("adopting the helper's core left running at start", "tun", core.Tun)
		s.placement.Set(true)
		s.state.Set(core.Tun)
		return true, nil
	}
	tun, err := s.startupTun(ctx, wanted)
	if err != nil {
		return false, err
	}
	config, err := s.configs.StartupConfigTun(tun)
	if err != nil {
		return false, fmt.Errorf("build the startup config (tun=%t): %w", tun, err)
	}
	if err := s.prepareHelperCore(ctx, config); err != nil {
		return false, err
	}
	// A core an earlier Hop ran itself would hold the ports.
	if err := s.runner.ReapLeftover(); err != nil {
		return false, err
	}
	if err := s.helper.StartCore(ctx, config); err != nil {
		return false, fmt.Errorf("start the core under the helper (tun=%t): %w", tun, err)
	}
	s.placement.Set(true)
	s.state.Set(tun)
	return true, nil
}

// startupTun says whether the core starts with TUN on: when the saved
// setting asks for it, unless another client's TUN holds the default routes,
// the refusal the switch applies too.
func (s *TunService) startupTun(ctx context.Context, wanted bool) (bool, error) {
	if !wanted {
		return false, nil
	}
	taken, err := s.taken(ctx)
	if err != nil {
		return false, err
	}
	if taken {
		slog.Warn("another TUN holds the default routes; starting with TUN off")
		return false, nil
	}
	return true, nil
}

// Release stops the helper's core at quit: a TUN device goes away with it,
// and apps fall back to their direct routes.
func (s *TunService) Release(ctx context.Context) error {
	if !s.placement.On() {
		return nil
	}
	if err := s.stopHelperCore(ctx); err != nil {
		return fmt.Errorf("stop the helper's core: %w", err)
	}
	s.placement.Set(false)
	s.state.Set(false)
	return nil
}

// stopHelperCore stops the helper's core after Hop closed its own
// connections to it, streams included: closed by the root core, they would
// hold the controller's port from Hop's own core, and from the next Hop
// after a quit or an update, for TIME_WAIT, 30 s on macOS.
func (s *TunService) stopHelperCore(ctx context.Context) error {
	s.controller.CloseStreams()
	s.controller.CloseIdleConnections()
	return s.helper.StopCore(ctx)
}

// Uninstall removes the helper entirely. The core moves out of it first, TUN
// off, so Hop's own core carries the traffic again before the helper goes.
func (s *TunService) Uninstall(ctx context.Context) error {
	if s.placement.On() {
		if err := s.moveToOwn(ctx); err != nil {
			return fmt.Errorf("move the core out of the helper before uninstalling it: %w", err)
		}
	}
	if err := s.install.Uninstall(ctx); err != nil {
		return fmt.Errorf("uninstall the helper: %w", err)
	}
	return nil
}

// moveToOwn hands the core back to Hop's own process with TUN off, keeping
// the mode it runs in: the one move from root to the user. Connections root's
// core closed hold its ports from a core the user runs for TIME_WAIT, so the
// start can wait up to 30 s (two minutes on Windows).
func (s *TunService) moveToOwn(ctx context.Context) error {
	// The config is built while a core still answers the mode read.
	config, err := s.configs.LiveConfigTun(ctx, false)
	if err != nil {
		return fmt.Errorf("build the config without TUN: %w", err)
	}
	if err := s.stopHelperCore(ctx); err != nil {
		return fmt.Errorf("stop the helper's core: %w", err)
	}
	s.placement.Set(false)
	s.state.Set(false)
	if err := s.runner.Start(config); err != nil {
		return fmt.Errorf("start Hop's own core: %w", err)
	}
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

// defaultRouteCoverage is the share of IPv4 a tunnel must route before it
// counts as holding the default path. A TUN with the private ranges
// excluded still covers most of it; a split-tunnel VPN's single subnet
// covers a sliver.
const defaultRouteCoverage = 0.5

// TunRouteConflict reads the routing table and says whether a TUN interface
// other than ownDevice takes the default path over: another client's TUN is
// up, and Hop never takes the routes over it. The interface that holds them
// names the conflict. Coverage rather than a single 0.0.0.0/0 route is the
// test because TUN clients exclude the private ranges, splitting the
// default route into many prefixes.
func TunRouteConflict(table routetable.Table, ownDevice string) (bool, string) {
	seen := make(map[string]struct{})
	for _, route := range table {
		if !route.Tunnel || strings.EqualFold(route.Interface, ownDevice) {
			continue
		}
		if _, ok := seen[route.Interface]; ok {
			continue
		}
		seen[route.Interface] = struct{}{}
		if routetable.DefaultCoverage(table, route.Interface) >= defaultRouteCoverage {
			return true, route.Interface
		}
	}
	return false, ""
}
