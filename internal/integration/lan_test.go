package integration

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hop/config"
	"hop/internal/model"
	"hop/internal/repository/lan"
	"hop/internal/repository/mihomo"
	"hop/internal/service"
)

// TestLANAccess checks, against a real core, what other devices on the network
// can and cannot reach: with LAN access off nothing listens beyond this
// computer; on, the proxy port answers the computer's network address and asks
// for the account, except from this computer itself; the controller and the
// probe listeners stay on the loopback either way.
func TestLANAccess(t *testing.T) {
	binary := os.Getenv("HOP_MIHOMO_BINARY")
	if binary == "" {
		t.Skip("HOP_MIHOMO_BINARY is not set")
	}
	addrs, err := lan.Addresses()
	if err != nil || len(addrs) == 0 {
		t.Skipf("this computer has no private network address to reach itself at (err=%v)", err)
	}
	lanIP := addrs[0].String()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "reached") }))
	defer target.Close()

	appDir := t.TempDir()
	cfg := config.Config{
		MihomoBinary: binary, AppDir: appDir, HomeDir: filepath.Join(appDir, "mihomo"),
		MixedPort: testMixedPort, ControllerAddr: testController, Secret: testSecret, ProbePort: testProbePort,
	}
	build := func(lanSettings service.LANSettings) []byte {
		t.Helper()
		settings := service.RuntimeSettings{
			MixedPort: testMixedPort, ControllerAddr: testController, ControllerSecret: testSecret,
			BindAddress: "127.0.0.1", LogLevel: "warning", BootstrapGroupName: "Hop", Mode: model.ModeRule,
			ProbePort: testProbePort, LAN: lanSettings,
		}
		config, err := service.RuntimeConfig([]byte(profileWithoutGroups), model.RulesProvider, nil, settings)
		if err != nil {
			t.Fatalf("build config: %v", err)
		}
		return config
	}
	client := mihomo.NewClient(testController, testSecret, mihomo.ConfigPath(cfg.HomeDir), &http.Client{}, 5*time.Second)
	runnerCtx, stop := context.WithCancel(ctx)
	runner := mihomo.NewRunner(runnerCtx, cfg, client)
	if err := runner.Start(build(service.LANSettings{})); err != nil {
		t.Fatalf("start core: %v", err)
	}
	defer func() {
		stop()
		if err := runner.Stop(); err != nil {
			t.Errorf("stop core: %v", err)
		}
	}()

	// get fetches the target through the proxy port at host, with the account
	// when user is set, and returns the status.
	get := func(host string, user *url.Userinfo) (int, error) {
		proxy := &url.URL{Scheme: "http", Host: net.JoinHostPort(host, fmt.Sprint(testMixedPort)), User: user}
		resp, err := (&http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DisableKeepAlives: true, Proxy: http.ProxyURL(proxy)}}).Get(target.URL)
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		return resp.StatusCode, nil
	}
	reachable := func(addr string) bool {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			conn.Close()
		}
		return err == nil
	}
	account := model.LANCredential{Username: "hop", Password: "s3cretpw"}
	apply := func(lanSettings service.LANSettings) {
		t.Helper()
		if err := client.ApplyConfig(ctx, build(lanSettings)); err != nil {
			t.Fatalf("apply config: %v", err)
		}
	}

	if reachable(net.JoinHostPort(lanIP, fmt.Sprint(testMixedPort))) {
		t.Errorf("with LAN access off, the proxy port answers %s", lanIP)
	}
	if status, err := get("127.0.0.1", nil); err != nil || status != http.StatusOK {
		t.Errorf("on this computer with LAN access off: status %d, err %v", status, err)
	}

	apply(service.LANSettings{Enabled: true, Account: account})
	if status, err := get(lanIP, nil); err != nil || status != http.StatusProxyAuthRequired {
		t.Errorf("from the network without an account: status %d, err %v; want 407", status, err)
	}
	if status, err := get(lanIP, url.UserPassword(account.Username, "wrong")); err != nil || status == http.StatusOK {
		t.Errorf("from the network with a wrong password: status %d, err %v; want a refusal", status, err)
	}
	if status, err := get(lanIP, url.UserPassword(account.Username, account.Password)); err != nil || status != http.StatusOK {
		t.Errorf("from the network with the account: status %d, err %v", status, err)
	}
	if status, err := get("127.0.0.1", nil); err != nil || status != http.StatusOK {
		t.Errorf("on this computer, which needs no account: status %d, err %v", status, err)
	}
	for name, addr := range map[string]string{
		"controller":     net.JoinHostPort(lanIP, fmt.Sprint(19091)),
		"probe listener": net.JoinHostPort(lanIP, fmt.Sprint(testProbePort)),
	} {
		if reachable(addr) {
			t.Errorf("with LAN access on, the %s answers the network address %s", name, lanIP)
		}
	}

	apply(service.LANSettings{Enabled: true})
	if status, err := get(lanIP, nil); err != nil || status != http.StatusOK {
		t.Errorf("from the network, open access: status %d, err %v", status, err)
	}

	apply(service.LANSettings{})
	if reachable(net.JoinHostPort(lanIP, fmt.Sprint(testMixedPort))) {
		t.Errorf("after LAN access is turned off again, the proxy port still answers %s", lanIP)
	}
}

// TestLANServiceReloadsTheCore checks the whole path of the settings switch:
// the service sets the access, the subscription service rebuilds the running
// config with it, and the core answers accordingly; a reset password cuts off
// the old one.
func TestLANServiceReloadsTheCore(t *testing.T) {
	addrs, err := lan.Addresses()
	if err != nil || len(addrs) == 0 {
		t.Skipf("this computer has no private network address to reach itself at (err=%v)", err)
	}
	lanIP := addrs[0].String()
	h := newHarness(t, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	coreCtx, stopCore := context.WithCancel(ctx)
	core := h.startCore(t, coreCtx)
	defer func() {
		stopCore()
		if err := core.Wait(); err != nil {
			t.Errorf("stop core: %v", err)
		}
	}()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "reached") }))
	defer target.Close()
	if err := h.service.Add(ctx, model.SubscriptionInput{Source: model.SourceLocal, Rules: model.RulesProvider, Content: []byte(profileWithoutGroups)}); err != nil {
		t.Fatalf("add the profile: %v", err)
	}

	credentials := lan.NewCredentials(h.cfg.AppDir)
	lanService := service.NewLANService(h.lan, credentials, lan.Source{}, h.service, h.port)
	get := func(host string, user *url.Userinfo) int {
		t.Helper()
		proxy := &url.URL{Scheme: "http", Host: net.JoinHostPort(host, fmt.Sprint(testMixedPort)), User: user}
		resp, err := (&http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DisableKeepAlives: true, Proxy: http.ProxyURL(proxy)}}).Get(target.URL)
		if err != nil {
			return 0
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if got := get(lanIP, nil); got != 0 {
		t.Fatalf("before LAN access is turned on, the network address answers with %d", got)
	}
	if err := lanService.Apply(ctx, true, true); err != nil {
		t.Fatalf("turn LAN access on: %v", err)
	}
	status, err := lanService.Status()
	if err != nil || !status.Enabled || status.Account.Password == "" || len(status.Addresses) == 0 {
		t.Fatalf("status = %+v, err %v; want it on, with an account and an address", status, err)
	}
	if kept, found, err := credentials.Load(); err != nil || !found || kept != status.Account {
		t.Fatalf("the kept account = %+v (found %v, err %v), want %+v", kept, found, err, status.Account)
	}
	if got := get(lanIP, nil); got != http.StatusProxyAuthRequired {
		t.Errorf("from the network without an account: %d, want 407", got)
	}
	old := status.Account
	if got := get(lanIP, url.UserPassword(old.Username, old.Password)); got != http.StatusOK {
		t.Errorf("from the network with the account: %d, want 200", got)
	}

	if err := lanService.ResetAccount(ctx); err != nil {
		t.Fatalf("reset the password: %v", err)
	}
	status, err = lanService.Status()
	if err != nil || status.Account.Password == old.Password {
		t.Fatalf("the password did not change: %+v, err %v", status, err)
	}
	if got := get(lanIP, url.UserPassword(old.Username, old.Password)); got == http.StatusOK {
		t.Errorf("the old password still works after a reset")
	}
	if got := get(lanIP, url.UserPassword(status.Account.Username, status.Account.Password)); got != http.StatusOK {
		t.Errorf("the new password: %d, want 200", got)
	}

	if err := lanService.Apply(ctx, false, true); err != nil {
		t.Fatalf("turn LAN access off: %v", err)
	}
	if got := get(lanIP, nil); got != 0 {
		t.Errorf("after LAN access is turned off, the network address answers with %d", got)
	}
	if got := get("127.0.0.1", nil); got != http.StatusOK {
		t.Errorf("on this computer after LAN access is off: %d, want 200", got)
	}
}
