package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"go.yaml.in/yaml/v3"

	"hop/config"
	"hop/internal/model"
	"hop/internal/repository/helper"
	"hop/internal/repository/mihomo"
	"hop/internal/repository/netservice"
	"hop/internal/repository/sysdns"
)

const (
	// helperVersion is this helper's protocol version, reported by
	// GET /version; it moves with the socket protocol only.
	helperVersion = helper.Version

	// controllerTimeout bounds one request to the core's controller.
	controllerTimeout = 5 * time.Second
	// maxRequestBytes bounds one request body: the config Hop sends is YAML,
	// a few hundred kilobytes at most.
	maxRequestBytes = 4 << 20
	// shutdownTimeout bounds draining the socket server at shutdown, before
	// the core is stopped.
	shutdownTimeout = 5 * time.Second
	// coreCheckTimeout bounds `core -v` while installing a core.
	coreCheckTimeout = 15 * time.Second

	// dnsBackupFileName is the record of the DNS the helper replaced, under
	// the helper's home; dnsBackupPerm keeps it owner-only.
	dnsBackupFileName = "dns-backup.json"
	dnsBackupPerm     = 0o600
	// dnsOpTimeout bounds one takeover or restore.
	dnsOpTimeout = 30 * time.Second
)

// managedResolvers are the public resolvers the helper points the system
// DNS at while TUN is on. Their packets route into the TUN, where the core's
// dns-hijack answers with fake IPs; they are real resolvers, so anything ever
// left behind still resolves. None is on Chrome's list of resolvers it
// upgrades to DNS over HTTPS, which would bypass the hijack.
var managedResolvers = []string{"223.5.5.5", "119.29.29.29", "114.114.114.114"}

// serverOptions are the helper's flags: every path the helper uses, so tests
// can run it unprivileged in temp directories.
type serverOptions struct {
	socket   string
	home     string
	core     string
	allowUID uint32
	// manageDNS lets the helper hold the system DNS while a TUN core runs.
	// Production sets it; tests never do, so a test can never touch the
	// real DNS.
	manageDNS bool
}

// server is the helper: an HTTP server on a unix socket that serves only the
// allowed peer, and at most one core at a time, run through the same runner
// Hop's own core uses (crash supervision included).
type server struct {
	opts serverOptions

	// mu serializes the core's lifecycle: at most one core runs.
	mu     sync.Mutex
	runner *mihomo.Runner
	// client is the running core's controller client, kept with the
	// controller keys it was built from so a reload goes to the right core.
	client       *mihomo.Client
	ctrlAddr     string
	ctrlSecret   string
	lastRestart  *model.CoreRestart
	parentCtx    context.Context
	parentCancel context.CancelFunc

	// dnsMu serializes the DNS takeover and restore; dnsManaged says whether
	// the helper holds the system DNS now.
	dnsMu      sync.Mutex
	dnsManaged atomic.Bool
}

func newServer(opts serverOptions) *server {
	return &server{opts: opts}
}

// serve prepares the socket and serves until SIGTERM or SIGINT, then stops
// the core and removes the socket.
func (s *server) serve(parent context.Context) error {
	if err := prepareSocket(s.opts.socket); err != nil {
		return err
	}
	listener, err := net.Listen("unix", s.opts.socket)
	if err != nil {
		return fmt.Errorf("listen on the helper socket (path=%q): %w", s.opts.socket, err)
	}
	if err := os.Chmod(s.opts.socket, 0o666); err != nil {
		listener.Close()
		return fmt.Errorf("open the helper socket to the installing user (path=%q): %w", s.opts.socket, err)
	}
	defer os.Remove(s.opts.socket)

	// A backup left by a helper or a Mac that died mid-takeover is the only
	// record of the user's own servers: put them back before serving, so a
	// crash never leaves the system on Hop's resolvers. A helper that cannot
	// restore still serves — refusing to would leave nothing able to.
	if s.opts.manageDNS {
		restoreCtx, restoreDone := context.WithTimeout(context.Background(), dnsOpTimeout)
		if err := s.restoreDNS(restoreCtx); err != nil {
			slog.Error("could not restore the system DNS left over from an earlier run", "error", err)
		}
		restoreDone()
	}

	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	s.mu.Lock()
	s.parentCtx, s.parentCancel = ctx, cancel
	s.mu.Unlock()

	httpSrv := &http.Server{
		Handler: s.allowPeer(http.HandlerFunc(s.route)),
		// ConnContext carries each request's connection to the credential
		// check, the only way a handler sees its connection.
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			return context.WithValue(ctx, connKey{}, c)
		},
		ReadHeaderTimeout: shutdownTimeout,
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- httpSrv.Serve(listener) }()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)

	select {
	case sig := <-signals:
		slog.Info("hop-helper stopping", "signal", sig.String())
	case <-parent.Done():
		slog.Info("hop-helper stopping", "cause", "context cancelled")
	}
	cancel()
	drainCtx, drainDone := context.WithTimeout(context.Background(), shutdownTimeout)
	defer drainDone()
	if err := httpSrv.Shutdown(drainCtx); err != nil {
		slog.Warn("the helper's socket did not drain cleanly", "error", err)
	}
	stopErr := s.stopCore()
	serveError := <-serveErr
	if errors.Is(serveError, http.ErrServerClosed) {
		// Shutdown was asked for; that the server closed is the clean path.
		serveError = nil
	}
	return errors.Join(stopErr, serveError)
}

// route dispatches one allowed request.
func (s *server) route(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/version":
		writeJSON(w, helper.VersionResponse{Version: helperVersion})
	case r.Method == http.MethodGet && r.URL.Path == "/status":
		writeJSON(w, s.status())
	case r.Method == http.MethodPost && r.URL.Path == "/core/install":
		s.handleInstall(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/core/start":
		s.handleStart(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/core/apply":
		s.handleApply(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/core/stop":
		s.handleStop(w, r)
	default:
		writeError(w, http.StatusNotFound, fmt.Errorf("no such endpoint: %s %s", r.Method, r.URL.Path))
	}
}

// status reports the core's state under mu.
func (s *server) status() helper.Status {
	status := helper.Status{DNSManaged: s.dnsManaged.Load()}
	s.mu.Lock()
	defer s.mu.Unlock()
	status.LastRestart = s.lastRestart
	if s.runner == nil {
		return status
	}
	pid, err := readPidFile(filepath.Join(s.opts.home, "mihomo.pid"))
	if err != nil {
		slog.Warn("could not read the core's pid file for the status", "error", err)
	}
	status.Running, status.Pid = true, pid
	return status
}

// handleInstall copies the mihomo binary at the request's path over the
// helper's own core, after the binary itself says it is a mihomo, and seeds
// the geodata databases when a directory to seed from came along. A running
// core is never replaced.
func (s *server) handleInstall(w http.ResponseWriter, r *http.Request) {
	var req helper.InstallRequest
	if !readJSON(w, r, &req) {
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, errors.New("path is required"))
		return
	}
	s.mu.Lock()
	running := s.runner != nil
	s.mu.Unlock()
	if running {
		writeError(w, http.StatusConflict, errors.New("the core is running; stop it before installing another"))
		return
	}
	// The file is copied first and then checked as copied: what -v approves
	// and what is renamed into place is the very same bytes, so the source
	// can change in between without swapping in an unchecked binary.
	staged, err := stageCore(req.Path, s.opts.home)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer os.Remove(staged)
	if err := checkMihomo(r.Context(), staged); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := os.Rename(staged, s.opts.core); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("put the installed core in place (path=%q): %w", s.opts.core, err))
		return
	}
	if req.Geodata != "" {
		// Geodata reaches the core's home before it starts, so a config with
		// GEOIP or GEOSITE rules never waits for a download.
		if err := mihomo.SeedGeodata(req.Geodata, s.opts.home); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	slog.Info("installed a core into the helper", "source", req.Path, "core", s.opts.core, "geodata", req.Geodata)
}

// handleStart writes the request's config as the core's config and starts
// the core on it, returning once its controller answers.
func (s *server) handleStart(w http.ResponseWriter, r *http.Request) {
	var req helper.StartRequest
	if !readJSON(w, r, &req) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runner != nil {
		writeError(w, http.StatusConflict, errors.New("the core is already running; stop it first"))
		return
	}
	if s.parentCtx == nil {
		writeError(w, http.StatusInternalServerError, errors.New("the helper is not serving yet"))
		return
	}
	addr, secret, err := controllerKeys(req.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	// The runner the helper reuses from Hop: config, core and pid file all
	// under the helper's own home, crash supervision with it.
	cfg := config.Config{
		MihomoBinary:   s.opts.core,
		BundledBinary:  s.opts.core,
		CoreOrigin:     model.CoreBundled,
		AppDir:         s.opts.home,
		HomeDir:        s.opts.home,
		ControllerAddr: addr,
		Secret:         secret,
	}
	client := mihomo.NewClient(addr, secret, mihomo.ConfigPath(s.opts.home), &http.Client{}, controllerTimeout)
	runner := mihomo.NewRunner(s.parentCtx, cfg, client)
	runner.OnRestart(func(restart model.CoreRestart) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.lastRestart = &restart
		if !restart.GaveUp || s.runner != runner {
			return
		}
		// Supervision stopped for good and no core runs: a later start is
		// not a second core, so forget the dead runner. The TUN routes went
		// away with the core, so the system DNS must go back to the user's
		// servers now, not at the next stop; mu keeps a new start from
		// taking the DNS over under this restore.
		s.clearCore()
		restoreCtx, restoreDone := context.WithTimeout(context.Background(), dnsOpTimeout)
		defer restoreDone()
		if err := s.restoreDNS(restoreCtx); err != nil {
			slog.Error("could not restore the system DNS after the core gave up", "error", err)
		}
	})
	if err := runner.Start([]byte(req.Config)); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.runner = runner
	s.client, s.ctrlAddr, s.ctrlSecret = client, addr, secret
	if s.opts.manageDNS {
		tunOn, err := tunEnabled(req.Config)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			s.stopStartedCore(runner)
			return
		}
		if tunOn {
			if err := s.takeoverDNS(r.Context()); err != nil {
				// The core is up but the DNS is not Hop's: stop it again, so
				// a failed start leaves neither a TUN nor a half-managed DNS.
				s.stopStartedCore(runner)
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		}
	}
}

// stopStartedCore undoes a start that failed after the core came up. mu must
// be held.
func (s *server) stopStartedCore(runner *mihomo.Runner) {
	s.clearCore()
	if err := runner.Stop(); err != nil {
		slog.Error("could not stop the core after a failed start", "error", err)
	}
}

// handleApply writes the request's config as the running core's config and
// makes it reload, the way Hop's own core reloads: the new file is kept only
// when the core accepts it. The controller address and secret cannot change
// under a running core, or the helper could no longer reach it.
func (s *server) handleApply(w http.ResponseWriter, r *http.Request) {
	var req helper.ApplyRequest
	if !readJSON(w, r, &req) {
		return
	}
	addr, secret, err := controllerKeys(req.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runner == nil || s.client == nil {
		writeError(w, http.StatusConflict, errors.New("no core is running; start one first"))
		return
	}
	if addr != s.ctrlAddr || secret != s.ctrlSecret {
		writeError(w, http.StatusBadRequest, errors.New("the controller cannot change while the core runs; stop and start it instead"))
		return
	}
	if err := s.client.ApplyConfig(r.Context(), []byte(req.Config)); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
}

// handleStop stops the core and waits for it. Stopping when none runs is a
// stopped status, not an error.
func (s *server) handleStop(w http.ResponseWriter, r *http.Request) {
	if err := s.stopCore(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, s.status())
}

// stopCore ends the core, if one runs, waits for it, and returns the system
// DNS to what the backup kept. Holding mu throughout keeps a new start from
// taking the DNS over between the stop and the restore; the restore gets its
// own bounded context, so a request that went away cannot abort it.
func (s *server) stopCore() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	runner, client := s.runner, s.client
	s.clearCore()
	if client != nil {
		// Closed by the stopping core instead, these would keep the
		// controller's port from Hop's own core for 30 s (TIME_WAIT).
		client.CloseIdleConnections()
	}
	var stopErr error
	if runner != nil {
		stopErr = runner.Stop()
	}
	if !s.opts.manageDNS {
		return stopErr
	}
	restoreCtx, restoreDone := context.WithTimeout(context.Background(), dnsOpTimeout)
	defer restoreDone()
	return errors.Join(stopErr, s.restoreDNS(restoreCtx))
}

// clearCore forgets the core's runner and its controller client. mu must be
// held.
func (s *server) clearCore() {
	s.runner = nil
	s.client, s.ctrlAddr, s.ctrlSecret = nil, "", ""
}

// takeoverDNS points every enabled service's DNS at Hop's resolvers and
// flushes the cache, after keeping what was set in the backup file: that
// file is written before anything changes, so a crash can always undo the
// change. On an error nothing is left managed — what was changed is put
// back, and the error says whether that worked. A caller may hold mu (the
// lock order is mu, then dnsMu); it runs networksetup.
func (s *server) takeoverDNS(ctx context.Context) error {
	if !s.opts.manageDNS {
		return nil
	}
	s.dnsMu.Lock()
	defer s.dnsMu.Unlock()
	backupPath := s.dnsBackupPath()
	if _, err := os.Stat(backupPath); err == nil {
		// A leftover backup means an earlier restore did not finish, and what
		// is set now may already be Hop's. Keep the record: it is the only
		// copy of the user's own servers.
		slog.Warn("keeping the DNS backup of an earlier takeover", "path", backupPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check for a DNS backup (path=%q): %w", backupPath, err)
	} else {
		backup, err := readDNSBackup(ctx)
		if err != nil {
			return err
		}
		if err := writeDNSBackup(backupPath, backup); err != nil {
			return err
		}
	}
	services, err := netservice.List(ctx)
	if err != nil {
		return err
	}
	resolvers := managedResolvers
	for _, service := range services {
		if err := sysdns.Write(ctx, service, resolvers); err != nil {
			return errors.Join(fmt.Errorf("point the DNS of %q at Hop's resolvers: %w", service, err), s.restoreFresh())
		}
	}
	if err := sysdns.FlushCache(ctx); err != nil {
		return errors.Join(err, s.restoreFresh())
	}
	s.dnsManaged.Store(true)
	return nil
}

// restoreDNS returns every service to the DNS the backup kept, removes the
// backup once every service is back, and flushes the cache. It is safe when
// no takeover happened and does nothing unless the helper manages DNS.
func (s *server) restoreDNS(ctx context.Context) error {
	if !s.opts.manageDNS {
		return nil
	}
	s.dnsMu.Lock()
	defer s.dnsMu.Unlock()
	return s.restoreLocked(ctx)
}

// restoreFresh restores with a bounded context of its own: the caller's may
// be exactly what failed, and a cancelled request must never abort the
// restore. dnsMu must be held.
func (s *server) restoreFresh() error {
	restoreCtx, restoreDone := context.WithTimeout(context.Background(), dnsOpTimeout)
	defer restoreDone()
	return s.restoreLocked(restoreCtx)
}

// restoreLocked is restoreDNS for a caller already holding dnsMu.
func (s *server) restoreLocked(ctx context.Context) error {
	backupPath := s.dnsBackupPath()
	raw, err := os.ReadFile(backupPath)
	if errors.Is(err, os.ErrNotExist) {
		s.dnsManaged.Store(false)
		return nil
	}
	if err != nil {
		// A backup that cannot be read still marks a takeover that was not
		// undone: report DNS as managed rather than claim it is not.
		s.dnsManaged.Store(true)
		return fmt.Errorf("read the DNS backup (path=%q): %w", backupPath, err)
	}
	backup, err := sysdns.DecodeBackup(raw)
	if err != nil {
		s.dnsManaged.Store(true)
		return fmt.Errorf("read the DNS backup (path=%q): %w", backupPath, err)
	}
	services, err := netservice.List(ctx)
	if err != nil {
		s.dnsManaged.Store(true)
		return err
	}
	enabled := make(map[string]struct{}, len(services))
	for _, service := range services {
		enabled[service] = struct{}{}
	}
	var failures []error
	for _, service := range sortedServiceNames(backup.Services) {
		if _, ok := enabled[service]; !ok {
			slog.Warn("a network service in the DNS backup no longer exists; skipping it", "service", service)
			continue
		}
		if err := sysdns.Write(ctx, service, backup.Services[service]); err != nil {
			failures = append(failures, fmt.Errorf("restore the DNS of %q: %w", service, err))
		}
	}
	if len(failures) > 0 {
		// The backup is the only record left; keep it for the next attempt.
		s.dnsManaged.Store(true)
		return errors.Join(failures...)
	}
	// Every service is back on its own servers; the file has done its job.
	if err := os.Remove(backupPath); err != nil {
		s.dnsManaged.Store(true)
		return fmt.Errorf("remove the DNS backup (path=%q): %w", backupPath, err)
	}
	s.dnsManaged.Store(false)
	return sysdns.FlushCache(ctx)
}

// dnsBackupPath is the backup file under the helper's own home.
func (s *server) dnsBackupPath() string {
	return filepath.Join(s.opts.home, dnsBackupFileName)
}

// readDNSBackup reads the DNS of every enabled service before anything
// changes it.
func readDNSBackup(ctx context.Context) (sysdns.Backup, error) {
	services, err := netservice.List(ctx)
	if err != nil {
		return sysdns.Backup{}, err
	}
	backup := sysdns.Backup{Services: make(map[string][]string, len(services))}
	for _, service := range services {
		servers, err := sysdns.Read(ctx, service)
		if err != nil {
			return sysdns.Backup{}, fmt.Errorf("read the DNS of %q: %w", service, err)
		}
		backup.Services[service] = servers
	}
	return backup, nil
}

// writeDNSBackup stores the backup through a temporary file, so a crash
// mid-write never leaves a half file as the only record of the user's
// servers.
func writeDNSBackup(path string, backup sysdns.Backup) error {
	raw, err := backup.Encode()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".dns-backup-*")
	if err != nil {
		return fmt.Errorf("stage the DNS backup (dir=%q): %w", dir, err)
	}
	name := tmp.Name()
	_, writeErr := tmp.Write(raw)
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(name)
		return fmt.Errorf("write the staged DNS backup (path=%q): %w", name, errors.Join(writeErr, closeErr))
	}
	if err := os.Chmod(name, dnsBackupPerm); err != nil {
		os.Remove(name)
		return fmt.Errorf("set the DNS backup's mode (path=%q): %w", name, err)
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return fmt.Errorf("put the DNS backup in place (path=%q): %w", path, err)
	}
	return nil
}

// sortedServiceNames orders a backup's services, so a restore runs the same
// way every time.
func sortedServiceNames(services map[string][]string) []string {
	names := make([]string, 0, len(services))
	for name := range services {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// tunEnabled says whether config turns the TUN listener on.
func tunEnabled(configYAML string) (bool, error) {
	var doc struct {
		Tun struct {
			Enable bool `yaml:"enable"`
		} `yaml:"tun"`
	}
	if err := yaml.Unmarshal([]byte(configYAML), &doc); err != nil {
		return false, fmt.Errorf("the config is not YAML: %w", err)
	}
	return doc.Tun.Enable, nil
}

// allowPeer serves a request only when its connection's peer is the allowed
// user or root; anything else is refused and logged.
func (s *server) allowPeer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := r.Context().Value(connKey{}).(net.Conn)
		uid, err := peerUID(conn)
		if err != nil {
			slog.Warn("refused a helper request whose peer could not be identified", "path", r.URL.Path, "error", err)
			writeError(w, http.StatusForbidden, errors.New("the helper could not identify the caller"))
			return
		}
		if uid != 0 && uid != s.opts.allowUID {
			slog.Warn("refused a helper request from an unallowed user", "uid", uid, "allowed", s.opts.allowUID, "path", r.URL.Path)
			writeError(w, http.StatusForbidden, fmt.Errorf("uid %d is not allowed to talk to the helper", uid))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// connKey carries each request's connection from ConnContext to the
// credential check.
type connKey struct{}

// prepareSocket makes the socket path free for a listener: a live helper
// there is an error, never displaced; a socket file nothing answers on is
// the leftover of a helper that was killed, and is removed.
func prepareSocket(path string) error {
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err == nil {
		conn.Close()
		return fmt.Errorf("another helper is already listening (path=%q)", path)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove the stale socket file (path=%q): %w", path, err)
	}
	return nil
}

// controllerKeys pulls the external controller address and its secret out of
// a runtime config, and refuses what a core run as root must never do: bind
// the controller off loopback, or serve the LAN.
func controllerKeys(configYAML string) (string, string, error) {
	var doc struct {
		ExternalController string `yaml:"external-controller"`
		Secret             string `yaml:"secret"`
		AllowLAN           bool   `yaml:"allow-lan"`
	}
	if err := yaml.Unmarshal([]byte(configYAML), &doc); err != nil {
		return "", "", fmt.Errorf("the config is not YAML: %w", err)
	}
	if doc.ExternalController == "" {
		return "", "", errors.New("the config sets no external-controller")
	}
	host, _, err := net.SplitHostPort(doc.ExternalController)
	if err != nil {
		return "", "", fmt.Errorf("the config's external-controller is not host:port (%q): %w", doc.ExternalController, err)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return "", "", fmt.Errorf("the config's external-controller %q is not on loopback", doc.ExternalController)
	}
	if doc.AllowLAN {
		return "", "", errors.New("the config turns allow-lan on, which a root-run core never does")
	}
	return doc.ExternalController, doc.Secret, nil
}

// checkMihomo runs `binary -v` and requires the answer to say mihomo, so a
// wrong file is never installed as the core.
func checkMihomo(ctx context.Context, binary string) error {
	checkCtx, cancel := context.WithTimeout(ctx, coreCheckTimeout)
	defer cancel()
	out, err := exec.CommandContext(checkCtx, binary, "-v").CombinedOutput()
	if err != nil {
		return fmt.Errorf("the file at %s does not run as the core (-v): %w", binary, err)
	}
	if !strings.Contains(strings.ToLower(string(out)), "mihomo") {
		return fmt.Errorf("the file at %s is not a mihomo core (-v says %q)", binary, strings.TrimSpace(string(out)))
	}
	return nil
}

// stageCore copies src into dir under a temporary name and makes the copy
// executable: what -v approves and what is later renamed into place are the
// very same bytes, so the source cannot change between the check and the
// install.
func stageCore(src string, dir string) (string, error) {
	data, err := os.ReadFile(src)
	if err != nil {
		return "", fmt.Errorf("read the file to install (path=%q): %w", src, err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create the helper's home (dir=%q): %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".core-*")
	if err != nil {
		return "", fmt.Errorf("stage the core in the helper's home (dir=%q): %w", dir, err)
	}
	name := tmp.Name()
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(name)
		return "", fmt.Errorf("write the staged core (path=%q): %w", name, errors.Join(writeErr, closeErr))
	}
	if err := os.Chmod(name, 0o755); err != nil {
		os.Remove(name)
		return "", fmt.Errorf("make the staged core executable (path=%q): %w", name, err)
	}
	return name, nil
}

// readPidFile reads the pid Hop's own process code records for the core.
func readPidFile(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read the core's pid file (path=%q): %w", path, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return 0, fmt.Errorf("the core's pid file does not hold a pid (path=%q content=%q): %w", path, raw, err)
	}
	return pid, nil
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("could not encode the helper's answer", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(helper.ErrorResponse{Error: err.Error()}); err != nil {
		slog.Error("could not encode the helper's error", "error", err)
	}
}

// readJSON decodes one request body into body; on a bad body it answers the
// request itself and reports false.
func readJSON(w http.ResponseWriter, r *http.Request, body any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	decodeErr := json.NewDecoder(r.Body).Decode(body)
	if decodeErr == nil {
		return true
	}
	writeError(w, http.StatusBadRequest, fmt.Errorf("the request body does not decode: %v", decodeErr))
	return false
}
