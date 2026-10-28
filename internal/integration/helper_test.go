package integration

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"hop/internal/model"
	"hop/internal/repository/corerelease"
	"hop/internal/repository/helper"
	"hop/internal/repository/mihomo"
	"hop/internal/service"
)

// bundledTag is the core release mihomo.version pins, what the build script
// stamps into the helper.
func bundledTag(t *testing.T, repoRoot string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, "mihomo.version"))
	if err != nil {
		t.Fatalf("read mihomo.version: %v", err)
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		if tag, ok := strings.CutPrefix(line, "MIHOMO_VERSION="); ok {
			return strings.TrimSpace(tag)
		}
	}
	t.Fatal("mihomo.version names no MIHOMO_VERSION")
	return ""
}

// TestHelperServesItsCore builds the helper, runs it unprivileged in a temp
// directory (every path from its flags) and drives it through Hop's client:
// version, the core install refusals and a real install verified against
// GitHub, a TUN-off core on the test ports, status, stop. A helper serving
// only another uid then refuses every request.
func TestHelperServesItsCore(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the helper's peer-credential check is darwin's")
	}
	binary := os.Getenv("HOP_MIHOMO_BINARY")
	if binary == "" {
		t.Skip("HOP_MIHOMO_BINARY is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	dir := t.TempDir()
	helperBin := filepath.Join(dir, "hop-helper")
	home := filepath.Join(dir, "helper-home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	core := filepath.Join(dir, "helper-core")
	sock := filepath.Join(dir, "helper.sock")

	// The helper is built from the module beside this test, never installed,
	// stamped with the bundled core's release as scripts/build-helper.sh does.
	repoRoot := filepath.Join("..", "..")
	tag := bundledTag(t, repoRoot)
	build := exec.CommandContext(ctx, "go", "build", "-ldflags", "-X main.bundledCoreVersion="+tag, "-o", helperBin, "hop/cmd/hop-helper")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the helper: %v\n%s", err, out)
	}

	client := helper.NewClient(sock, 20*time.Second)
	coreClient := mihomo.NewClient(testController, testSecret, mihomo.ConfigPath(home), &http.Client{}, 5*time.Second)

	// runHelper starts a helper serving uid and hands its process back; the
	// test stops every process it started, by its pid.
	runHelper := func(t *testing.T, uid int) *exec.Cmd {
		t.Helper()
		cmd := exec.CommandContext(ctx, helperBin,
			"-socket", sock, "-home", home, "-core", core, "-allow-uid", strconv.Itoa(uid))
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatalf("start the helper: %v", err)
		}
		return cmd
	}
	// waitServing blocks until the socket answers anything, allowed or
	// refused: a 403 still proves the helper is up.
	waitServing := func(t *testing.T) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for {
			_, err := client.Version(ctx)
			var apiErr *helper.APIError
			if err == nil || errors.As(err, &apiErr) {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("the helper never answered: %v", err)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	stopHelper := func(t *testing.T, cmd *exec.Cmd) {
		t.Helper()
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
			t.Fatalf("stop the helper (pid=%d): %v", cmd.Process.Pid, err)
		}
		if err := cmd.Wait(); err != nil {
			t.Errorf("the helper did not exit cleanly: %v", err)
		}
	}

	cmd := runHelper(t, os.Getuid())
	defer func() {
		if cmd.Process != nil {
			cmd.Process.Signal(syscall.SIGTERM)
			cmd.Wait()
		}
	}()
	waitServing(t)

	// The administrator's install puts a core in place itself; the helper
	// reports its release.
	coreBytes, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(core, coreBytes, 0o755); err != nil {
		t.Fatal(err)
	}
	installedTag, err := mihomo.BinaryVersion(ctx, binary)
	if err != nil {
		t.Fatal(err)
	}

	version, err := client.Version(ctx)
	if err != nil {
		t.Fatalf("read the helper's version: %v", err)
	}
	if version != helper.Version {
		t.Errorf("helper version = %q, want %q", version, helper.Version)
	}
	status, err := client.Status(ctx)
	if err != nil {
		t.Fatalf("read the helper's status: %v", err)
	}
	if status.Running {
		t.Error("the helper reports a running core before one was started")
	}
	if status.CoreVersion != installedTag {
		t.Errorf("the helper reports core %q, want %q", status.CoreVersion, installedTag)
	}

	// A core reaches the helper only as a GitHub release archive the helper
	// verifies itself: anything else is refused and the core stays as it was.
	var apiErr *helper.APIError
	fake := filepath.Join(dir, "fake-archive")
	if err := os.WriteFile(fake, gzipBytes(t, []byte("#!/bin/sh\necho mihomo v9.9.9\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(dir, "linked-archive")
	if err := os.Symlink(fake, linked); err != nil {
		t.Fatal(err)
	}
	for name, req := range map[string]struct {
		archive model.CoreArchive
		proxy   string
	}{
		"an archive that is not the release's": {model.CoreArchive{Tag: tag, Path: fake}, ""},
		"a release older than the bundled one": {model.CoreArchive{Tag: "v1.0.0", Path: fake}, ""},
		"a tag that is no plain version":       {model.CoreArchive{Tag: "v1.19.33/../../x", Path: fake}, ""},
		"an archive behind a symbolic link":    {model.CoreArchive{Tag: tag, Path: linked}, ""},
		"a proxy that is not on the loopback":  {model.CoreArchive{Tag: tag, Path: fake}, "http://192.0.2.1:7890"},
	} {
		if err := client.InstallCore(ctx, req.archive, req.proxy); !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
			t.Errorf("install with %s = %v, want a 400", name, err)
		}
	}
	if got, err := os.ReadFile(core); err != nil || !bytes.Equal(got, coreBytes) {
		t.Fatalf("a refused install changed the helper's core (err %v)", err)
	}

	// The real release archive of the bundled core, straight from GitHub, is
	// installed: the helper checks it against the digest GitHub publishes.
	releases := corerelease.NewSource(&http.Client{}, corerelease.ReleasesAPI)
	release, err := releases.Release(ctx, tag)
	if err != nil {
		t.Fatalf("ask GitHub for release %s: %v", tag, err)
	}
	archive := filepath.Join(dir, "release-archive")
	if err := releases.DownloadArchive(ctx, release, archive, func(int64, int64) {}); err != nil {
		t.Fatalf("download release %s: %v", tag, err)
	}
	if err := client.InstallCore(ctx, model.CoreArchive{Tag: tag, Path: archive}, ""); err != nil {
		t.Fatalf("install the release archive into the helper: %v", err)
	}
	info, err := os.Stat(core)
	if err != nil {
		t.Fatalf("the helper's core is not there: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Error("the helper's core is not executable")
	}
	if got, err := mihomo.BinaryVersion(ctx, core); err != nil || got != tag {
		t.Errorf("the installed core reports %q (err %v), want %q", got, err, tag)
	}
	if status, err := client.Status(ctx); err != nil || status.CoreVersion != tag {
		t.Errorf("the helper reports core %q (err %v) after the install, want %q", status.CoreVersion, err, tag)
	}

	// A TUN-off config, on the ports the integration tests own.
	settings := service.RuntimeSettings{
		MixedPort: testMixedPort, ControllerAddr: testController, ControllerSecret: testSecret,
		BindAddress: "127.0.0.1", LogLevel: "warning", BootstrapGroupName: "Hop", Mode: "rule",
	}
	startup, err := service.BootstrapConfig(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.StartCore(ctx, startup); err != nil {
		t.Fatalf("start the core under the helper: %v", err)
	}
	// StartCore answers once the controller does; asking it proves the same.
	if _, err := coreClient.Version(ctx); err != nil {
		t.Fatalf("the helper's core does not answer its controller: %v", err)
	}
	status, err = client.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Running || status.Pid <= 0 {
		t.Fatalf("status after start = running:%v pid:%d, want a running core with a pid", status.Running, status.Pid)
	}
	if status.DNSManaged {
		t.Error("the helper reports managed DNS although the test started it without -manage-dns")
	}
	pidFile, err := os.ReadFile(filepath.Join(home, "mihomo.pid"))
	if err != nil {
		t.Fatalf("the helper wrote no pid file in its home: %v", err)
	}
	if got, _ := strconv.Atoi(string(pidFile)); got != status.Pid {
		t.Errorf("status pid %d, pid file %d", status.Pid, got)
	}

	// A second config applied through the helper replaces the running one:
	// the controller reports the new mode.
	next := settings
	next.Mode = model.ModeGlobal
	second, err := service.BootstrapConfig(next)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ApplyConfig(ctx, second); err != nil {
		t.Fatalf("apply a second config through the helper: %v", err)
	}
	if mode, err := coreClient.Mode(ctx); err != nil {
		t.Fatalf("read the mode after the apply: %v", err)
	} else if mode != model.ModeGlobal {
		t.Errorf("mode after the apply = %q, want %q", mode, model.ModeGlobal)
	}

	// A config a root-run core must never run is refused, and the core keeps
	// the config it had.
	allowsLAN := bytes.Replace(second, []byte("allow-lan: false"), []byte("allow-lan: true"), 1)
	if bytes.Equal(allowsLAN, second) {
		t.Fatal("the test config does not carry allow-lan: false")
	}
	if err := client.ApplyConfig(ctx, allowsLAN); !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
		t.Fatalf("apply with allow-lan = %v, want a 400", err)
	}
	offLoopback := bytes.Replace(second, []byte("external-controller: 127.0.0.1:19091"), []byte("external-controller: 0.0.0.0:19091"), 1)
	if bytes.Equal(offLoopback, second) {
		t.Fatal("the test config does not carry the test controller")
	}
	if err := client.ApplyConfig(ctx, offLoopback); !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
		t.Fatalf("apply with an off-loopback controller = %v, want a 400", err)
	}
	if mode, err := coreClient.Mode(ctx); err != nil || mode != model.ModeGlobal {
		t.Errorf("mode after the refused applies = %q (err %v), want %q", mode, err, model.ModeGlobal)
	}

	// LAN access is accepted only inside the private networks: the config the
	// app writes for it is applied, anything wider or any listener off the
	// loopback is a 400, and the core keeps what it had.
	withLAN := next
	withLAN.LAN = service.LANSettings{Enabled: true, Account: model.LANCredential{Username: "hop", Password: "s3cretpw"}}
	lanConfig, err := service.BootstrapConfig(withLAN)
	if err != nil {
		t.Fatal(err)
	}
	everyAddress := bytes.Replace(lanConfig, []byte("127.0.0.0/8"), []byte("0.0.0.0/0"), 1)
	noSources := bytes.Replace(lanConfig, []byte("lan-allowed-ips"), []byte("lan-allowed-ips-unused"), 1)
	offLoopbackListener := append(append([]byte{}, lanConfig...), []byte("listeners:\n  - {name: wide, type: http, listen: 0.0.0.0, port: 17999, proxy: DIRECT}\n")...)
	for name, refused := range map[string][]byte{
		"a source range reaching the internet": everyAddress,
		"no source range at all":               noSources,
		"a listener off the loopback":          offLoopbackListener,
	} {
		if bytes.Equal(refused, lanConfig) {
			t.Fatalf("the test config for %s did not change", name)
		}
		if err := client.ApplyConfig(ctx, refused); !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
			t.Fatalf("apply with %s = %v, want a 400", name, err)
		}
	}
	if err := client.ApplyConfig(ctx, lanConfig); err != nil {
		t.Fatalf("apply the LAN access the app writes: %v", err)
	}
	if mode, err := coreClient.Mode(ctx); err != nil || mode != model.ModeGlobal {
		t.Errorf("mode after the LAN config = %q (err %v), want %q", mode, err, model.ModeGlobal)
	}
	if err := client.ApplyConfig(ctx, second); err != nil {
		t.Fatalf("apply the config without LAN access again: %v", err)
	}

	// New geodata replaces the helper's copies and restarts the running core
	// on them, because the core reads the databases once, at start. The
	// controller answers again afterwards and the config it ran stays.
	geodataDir := filepath.Join(dir, "geodata")
	if err := os.MkdirAll(geodataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"GeoSite.dat": "new-site", "geoip.metadb": "new-ip"} {
		if err := os.WriteFile(filepath.Join(geodataDir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	geodataClient := helper.NewClient(sock, 60*time.Second)
	if err := geodataClient.InstallGeodata(ctx, geodataDir); err != nil {
		t.Fatalf("install geodata through the helper: %v", err)
	}
	for name, want := range map[string]string{"GeoSite.dat": "new-site", "geoip.metadb": "new-ip"} {
		got, err := os.ReadFile(filepath.Join(home, name))
		if err != nil {
			t.Fatalf("the helper did not copy %s: %v", name, err)
		}
		if string(got) != want {
			t.Errorf("the helper's %s = %q, want %q", name, got, want)
		}
	}
	if _, err := coreClient.Version(ctx); err != nil {
		t.Fatalf("the helper's core does not answer after the geodata restart: %v", err)
	}
	if mode, err := coreClient.Mode(ctx); err != nil || mode != model.ModeGlobal {
		t.Errorf("mode after the geodata restart = %q (err %v), want %q", mode, err, model.ModeGlobal)
	}

	// Geodata behind a symbolic link is refused: the helper never copies a
	// file the caller could not read into its world-readable home.
	secret := filepath.Join(dir, "secret")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkedGeodata := filepath.Join(dir, "linked-geodata")
	if err := os.MkdirAll(linkedGeodata, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(linkedGeodata, "GeoSite.dat")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linkedGeodata, "geoip.metadb"), []byte("ip"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := geodataClient.InstallGeodata(ctx, linkedGeodata); err == nil {
		t.Error("geodata behind a symbolic link was installed")
	}
	if got, err := os.ReadFile(filepath.Join(home, "GeoSite.dat")); err != nil || string(got) != "new-site" {
		t.Errorf("the helper's GeoSite.dat = %q (err %v) after a refused install, want %q", got, err, "new-site")
	}

	if err := client.StopCore(ctx); err != nil {
		t.Fatalf("stop the helper's core: %v", err)
	}
	status, err = client.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.Running {
		t.Error("status after stop still reports a running core")
	}
	if _, err := coreClient.Version(ctx); err == nil {
		t.Error("the controller still answers after the core was stopped")
	}

	// A helper that serves only another uid refuses this test's requests.
	stopHelper(t, cmd)
	cmd2 := runHelper(t, os.Getuid()+1)
	defer func() {
		if cmd2.Process != nil {
			cmd2.Process.Signal(syscall.SIGTERM)
			cmd2.Wait()
		}
	}()
	waitServing(t)
	_, err = client.Version(ctx)
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden {
		t.Fatalf("version from a helper serving another uid = %v, want a 403", err)
	}
	if err := client.StopCore(ctx); err == nil {
		t.Error("stop from a helper serving another uid succeeded")
	}
	stopHelper(t, cmd2)
}

// TestTunConfigAcceptedByCore has the real core parse the config Hop builds
// with TUN on. The core's own -t reads the config only: it never creates the
// TUN device or touches a route.
func TestTunConfigAcceptedByCore(t *testing.T) {
	binary := os.Getenv("HOP_MIHOMO_BINARY")
	if binary == "" {
		t.Skip("HOP_MIHOMO_BINARY is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	subscription := []byte(`proxies:
  - name: "node"
    type: socks5
    server: 1.2.3.4
    port: 1080
rules:
  - GEOIP,CN,DIRECT
  - MATCH,DIRECT
`)
	settings := service.RuntimeSettings{
		MixedPort: testMixedPort, ControllerAddr: testController, ControllerSecret: testSecret,
		BindAddress: "127.0.0.1", LogLevel: "warning", BootstrapGroupName: "Hop",
		Mode: "rule", Tun: true,
	}
	config, err := service.RuntimeConfig(subscription, model.RulesProvider, nil, settings)
	if err != nil {
		t.Fatal(err)
	}
	validateDir := t.TempDir()
	// The config test loads GEOIP's database, as the running core would; the
	// validator refuses to download one, so seed what the app ships.
	if err := mihomo.SeedGeodata(filepath.Join("..", "..", "resources", "geodata"), validateDir); err != nil {
		t.Fatal(err)
	}
	validator := mihomo.NewConfigValidator(binary, validateDir, 30*time.Second)
	if err := validator.Validate(ctx, config); err != nil {
		t.Fatalf("the core rejected the TUN config: %v", err)
	}
}
