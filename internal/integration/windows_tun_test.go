//go:build windows

package integration

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hop/internal/model"
	"hop/internal/repository/helper"
	"hop/internal/repository/routetable"
	"hop/internal/service"
)

// windowsTunDevice is the TUN device Hop's configs name.
const windowsTunDevice = "utun199"

// TestWindowsTunEndToEnd drives Enhanced mode on Windows through the real
// helper service: install it (the caller is already elevated, as a CI
// runner is, so no UAC prompt), run a core with TUN on in direct mode, and
// check the adapter, its routes, DNS answered by the core, traffic carried
// by it and Hop's own route-conflict reading; then stop and uninstall and
// check nothing is left. It changes the machine's routes and services, so
// it runs only where HOP_WINDOWS_TUN_E2E=1 says the machine is disposable.
func TestWindowsTunEndToEnd(t *testing.T) {
	if os.Getenv("HOP_WINDOWS_TUN_E2E") != "1" {
		t.Skip("HOP_WINDOWS_TUN_E2E is not 1: this test installs a service and takes the routes over")
	}
	binary := os.Getenv("HOP_MIHOMO_BINARY")
	if binary == "" {
		t.Fatal("HOP_MIHOMO_BINARY is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	tool := filepath.Join(t.TempDir(), "hop-helper.exe")
	build := exec.CommandContext(ctx, "go", "build", "-o", tool, "./cmd/hop-helper")
	build.Dir = filepath.Join("..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the helper: %v\n%s", err, out)
	}
	sid, err := helper.CurrentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	geodata, err := filepath.Abs(filepath.Join("..", "..", "resources", "geodata"))
	if err != nil {
		t.Fatal(err)
	}

	runTool(ctx, t, tool, "install", "-core", binary, "-geodata", geodata, "-sid", sid)
	// The uninstall runs from the built copy, not the installed one, which
	// could not delete itself.
	t.Cleanup(func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 2*time.Minute)
		defer done()
		uninstall := exec.CommandContext(cleanupCtx, tool, "uninstall", "-sid", sid)
		if out, err := uninstall.CombinedOutput(); err != nil {
			t.Errorf("uninstall the helper: %v\n%s\n%s", err, out, installLog())
		}
	})

	client := helper.NewClient(helper.SocketPath, time.Minute)
	waitFor(ctx, t, "the helper to answer on its socket", func() error {
		_, err := client.Version(ctx)
		return err
	})
	version, err := client.Version(ctx)
	if err != nil || version != helper.Version {
		t.Fatalf("helper version %q (err %v), want %q", version, err, helper.Version)
	}

	subscription := []byte(`proxies:
  - name: "node"
    type: socks5
    server: 192.0.2.1
    port: 1080
rules:
  - MATCH,DIRECT
`)
	config, err := service.RuntimeConfig(subscription, model.RulesProvider, nil, service.RuntimeSettings{
		MixedPort: testMixedPort, ControllerAddr: testController, ControllerSecret: testSecret,
		BindAddress: "127.0.0.1", LogLevel: "info", BootstrapGroupName: "Hop",
		Mode: model.ModeRule, Tun: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.StartCore(ctx, config); err != nil {
		t.Fatalf("start the TUN core: %v\n%s", err, helperLog())
	}
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			_ = client.StopCore(context.Background())
		}
	})

	waitFor(ctx, t, "the TUN adapter to come up", func() error {
		status := powershell(ctx, t, `(Get-NetAdapter -Name '`+windowsTunDevice+`' -ErrorAction Stop).Status`)
		if status != "Up" {
			return fmt.Errorf("adapter status %q", status)
		}
		return nil
	})
	t.Logf("TUN adapter routes:\n%s", powershell(ctx, t,
		`Get-NetRoute -InterfaceAlias '`+windowsTunDevice+`' -AddressFamily IPv4 | Sort-Object DestinationPrefix | Format-Table -AutoSize DestinationPrefix,RouteMetric | Out-String -Width 200`))
	t.Logf("TUN adapter DNS: %s", powershell(ctx, t,
		`(Get-DnsClientServerAddress -InterfaceAlias '`+windowsTunDevice+`' -AddressFamily IPv4).ServerAddresses -join ','`))

	// Hop's own reading of the routes: the TUN covers the default space,
	// it is not a conflict for itself, and it would be for anyone else.
	table, err := routetable.Reader{}.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if coverage := routetable.DefaultCoverage(table, windowsTunDevice); coverage < 0.5 {
		t.Errorf("Hop reads the TUN as covering %.3f of IPv4, want at least 0.5", coverage)
	}
	if conflict, iface := service.TunRouteConflict(table, windowsTunDevice); conflict {
		t.Errorf("Hop's own TUN reads as a conflict with %s", iface)
	}
	if conflict, _ := service.TunRouteConflict(table, "utun-other"); !conflict {
		t.Error("another client's view of this TUN does not read as a conflict")
	}

	// DNS: a name resolves through the system to a fake IP from the core.
	waitFor(ctx, t, "a fake IP for example.com", func() error {
		addr := powershell(ctx, t, `Clear-DnsClientCache; (Resolve-DnsName example.com -Type A -DnsOnly -ErrorAction Stop | Where-Object Type -eq 'A' | Select-Object -First 1).IPAddress`)
		if !strings.HasPrefix(addr, "198.18.") {
			return fmt.Errorf("example.com resolved to %q, not a fake IP", addr)
		}
		return nil
	})

	// Traffic: a plain request, no proxy set, is carried by the core.
	before := coreTotals(ctx, t)
	resp, err := http.Get("http://example.com/")
	if err != nil {
		t.Fatalf("plain request through the TUN: %v", err)
	}
	resp.Body.Close()
	after := coreTotals(ctx, t)
	if after.DownloadTotal <= before.DownloadTotal {
		t.Errorf("the core carried nothing for a plain request (download %d -> %d)", before.DownloadTotal, after.DownloadTotal)
	}

	if err := client.StopCore(ctx); err != nil {
		t.Fatalf("stop the TUN core: %v", err)
	}
	stopped = true
	waitFor(ctx, t, "the TUN adapter to go away", func() error {
		if out := powershell(ctx, t, `if (Get-NetAdapter -Name '`+windowsTunDevice+`' -ErrorAction SilentlyContinue) { 'present' }`); out != "" {
			return fmt.Errorf("adapter still %s", out)
		}
		return nil
	})
}

// coreTotalsMessage is what the controller's /connections reports in sum.
type coreTotalsMessage struct {
	UploadTotal   int64 `json:"uploadTotal"`
	DownloadTotal int64 `json:"downloadTotal"`
}

// coreTotals reads the core's traffic totals from its controller.
func coreTotals(ctx context.Context, t *testing.T) coreTotalsMessage {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+testController+"/connections", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testSecret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("read the core's totals: %v", err)
	}
	defer resp.Body.Close()
	var totals coreTotalsMessage
	if err := json.UnmarshalRead(resp.Body, &totals); err != nil {
		t.Fatalf("decode the core's totals: %v", err)
	}
	return totals
}

// runTool runs the built helper with args and fails with its install log.
func runTool(ctx context.Context, t *testing.T, tool string, args ...string) {
	t.Helper()
	if out, err := exec.CommandContext(ctx, tool, args...).CombinedOutput(); err != nil {
		t.Fatalf("hop-helper %s: %v\n%s\n%s", args[0], err, out, installLog())
	}
}

// powershell runs script and returns its trimmed output.
func powershell(ctx context.Context, t *testing.T, script string) string {
	t.Helper()
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return "error: " + strings.TrimSpace(string(out))
	}
	return strings.TrimSpace(string(out))
}

// waitFor retries check until it passes or a minute has gone.
func waitFor(ctx context.Context, t *testing.T, what string, check func() error) {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for {
		err := check()
		if err == nil {
			return
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			t.Fatalf("waiting for %s: %v\n%s", what, err, helperLog())
		}
		time.Sleep(time.Second)
	}
}

// installLog and helperLog are the helper's own records, for failures.
func installLog() string {
	data, err := os.ReadFile(helper.InstallLogPath())
	if err != nil {
		return "install log: " + err.Error()
	}
	return "install log:\n" + string(data)
}

func helperLog() string {
	data, err := os.ReadFile(filepath.Join(helper.HomeDir, "hop-helper.log"))
	if err != nil {
		return "helper log: " + err.Error()
	}
	text := string(data)
	if len(text) > 8000 {
		text = text[len(text)-8000:]
	}
	return "helper log (tail):\n" + text
}
