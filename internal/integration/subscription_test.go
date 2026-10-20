// Package integration runs Hop's layers against a real mihomo core and a
// local subscription server.
package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hop/config"
	"hop/internal/model"
	"hop/internal/repository/mihomo"
	"hop/internal/repository/profile"
	"hop/internal/repository/subscription"
	"hop/internal/service"
)

const (
	testMixedPort  = 17891
	testController = "127.0.0.1:19091"
	testSecret     = "integration-secret"
	linkToken      = "super-secret-token"

	// The subscription carries settings that would change how the core
	// touches the machine; Hop must drop all of them.
	hostileSubscription = `
port: 7890
socks-port: 7891
mixed-port: 7897
allow-lan: true
external-controller: 0.0.0.0:9090
secret: not-hops-secret
tun:
  enable: true
  auto-route: true
dns:
  enable: true
  listen: 0.0.0.0:53
listeners:
  - name: extra
    type: mixed
    port: 7999
proxies:
  - {name: "🇯🇵 Tokyo 01", type: ss, server: 192.0.2.1, port: 8388, cipher: aes-128-gcm, password: x}
  - {name: "🇯🇵 Tokyo 02", type: ss, server: 192.0.2.2, port: 8388, cipher: aes-128-gcm, password: x}
  - {name: "🇸🇬 Singapore 01", type: ss, server: 192.0.2.3, port: 8388, cipher: aes-128-gcm, password: x}
  - {name: "香港 IPLC 03", type: ss, server: 192.0.2.4, port: 8388, cipher: aes-128-gcm, password: x}
proxy-groups:
  - {name: Proxy, type: select, proxies: ["🇯🇵 Tokyo 01", "🇯🇵 Tokyo 02", "🇸🇬 Singapore 01", "香港 IPLC 03", DIRECT]}
  - {name: Streaming, type: select, proxies: ["🇸🇬 Singapore 01", "🇯🇵 Tokyo 01"]}
rules:
  - MATCH,Proxy
`
)

type controllerConfig struct {
	MixedPort int  `json:"mixed-port"`
	Port      int  `json:"port"`
	SocksPort int  `json:"socks-port"`
	AllowLAN  bool `json:"allow-lan"`
	Tun       struct {
		Enable bool `json:"enable"`
	} `json:"tun"`
}

func TestImportSubscriptionEndToEnd(t *testing.T) {
	binary := os.Getenv("HOP_MIHOMO_BINARY")
	if binary == "" {
		t.Skip("HOP_MIHOMO_BINARY is not set")
	}

	var hits int
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sub" || r.URL.Query().Get("token") != linkToken {
			http.Error(w, "unknown subscription", http.StatusNotFound)
			return
		}
		hits++
		w.Header().Set("Subscription-Userinfo", "upload=1073741824; download=32212254720; total=214748364800; expire=1796918400")
		_, _ = io.WriteString(w, hostileSubscription)
	}))
	defer provider.Close()

	appDir := t.TempDir()
	cfg := config.Config{
		MihomoBinary:   binary,
		AppDir:         appDir,
		HomeDir:        filepath.Join(appDir, "mihomo"),
		MixedPort:      testMixedPort,
		ControllerAddr: testController,
		Secret:         testSecret,
	}
	settings := service.RuntimeSettings{
		MixedPort: testMixedPort, ControllerAddr: testController, ControllerSecret: testSecret,
		BindAddress: "127.0.0.1", LogLevel: "warning", BootstrapGroupName: "Hop", Mode: model.ModeRule,
	}
	client := mihomo.NewClient(testController, testSecret, mihomo.ConfigPath(cfg.HomeDir), &http.Client{}, 5*time.Second)
	store := profile.NewStore(appDir)
	profiles := service.NewProfileService(subscription.NewFetcher(&http.Client{Timeout: 10 * time.Second}), store, client, settings)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	startCore := func(coreCtx context.Context) *mihomo.Process {
		t.Helper()
		startup, err := profiles.StartupConfig()
		if err != nil {
			t.Fatalf("startup config: %v", err)
		}
		core, err := mihomo.Start(coreCtx, cfg, startup)
		if err != nil {
			t.Fatalf("start core: %v", err)
		}
		if err := mihomo.WaitReady(coreCtx, client); err != nil {
			t.Fatalf("core not ready: %v", err)
		}
		return core
	}

	// First run: bootstrap profile, then import.
	firstCtx, stopFirst := context.WithCancel(ctx)
	first := startCore(firstCtx)

	bootstrap, err := client.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot bootstrap profile: %v", err)
	}
	if !hasGroup(bootstrap.Groups, "Hop") {
		t.Fatalf("bootstrap groups = %+v, want the Hop group", bootstrap.Groups)
	}

	link := provider.URL + "/sub?token=" + linkToken
	if err := profiles.Import(ctx, link); err != nil {
		t.Fatalf("import: %v", err)
	}
	assertImportedGroups(ctx, t, client)
	assertCoreSettings(t)

	summary, found, err := profiles.Summary()
	if err != nil || !found || summary.Host != strings.TrimPrefix(provider.URL, "http://") {
		t.Fatalf("Summary() = %+v, %v, %v; want the bare host", summary, found, err)
	}
	if !summary.HasUsage || summary.Usage.Total != 200<<30 || summary.Usage.Download != 30<<30 {
		t.Fatalf("Summary().Usage = %+v; want the provider's usage header", summary.Usage)
	}

	// A bad link fails without leaking the token and without touching the core.
	badErr := profiles.Import(ctx, provider.URL+"/missing?token="+linkToken)
	if badErr == nil {
		t.Fatal("importing an unknown subscription succeeded")
	}
	if strings.Contains(badErr.Error(), linkToken) {
		t.Fatalf("error leaks the link token: %v", badErr)
	}
	assertImportedGroups(ctx, t, client)

	// Second run: the stored subscription is what the core starts with.
	stopFirst()
	if err := first.Wait(); err != nil {
		t.Fatalf("stop first core: %v", err)
	}
	hitsBefore := hits
	secondCtx, stopSecond := context.WithCancel(ctx)
	second := startCore(secondCtx)
	defer func() {
		stopSecond()
		if err := second.Wait(); err != nil {
			t.Errorf("stop second core: %v", err)
		}
	}()
	assertImportedGroups(ctx, t, client)
	if hits != hitsBefore {
		t.Fatalf("restarting downloaded the subscription again (%d extra hits)", hits-hitsBefore)
	}
}

func hasGroup(groups []model.ProxyGroup, name string) bool {
	for _, g := range groups {
		if g.Name == name {
			return true
		}
	}
	return false
}

func assertImportedGroups(ctx context.Context, t *testing.T, client *mihomo.Client) {
	t.Helper()
	snapshot, err := client.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	counts := map[string]int{}
	for _, g := range snapshot.Groups {
		counts[g.Name] = len(g.Options)
	}
	if counts["Proxy"] != 5 || counts["Streaming"] != 2 {
		t.Fatalf("group sizes = %v, want Proxy=5 and Streaming=2", counts)
	}
	if _, stale := counts["Hop"]; stale {
		t.Fatalf("bootstrap group survived the import: %v", counts)
	}
}

// assertCoreSettings reads the running core's own view of its config.
func assertCoreSettings(t *testing.T) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+testController+"/configs", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testSecret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("read core config: %v", err)
	}
	defer resp.Body.Close()
	var got controllerConfig
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode core config: %v", err)
	}
	if got.MixedPort != testMixedPort || got.Port != 0 || got.SocksPort != 0 || got.AllowLAN || got.Tun.Enable {
		t.Fatalf("core config = %+v; the subscription's settings leaked through", got)
	}
}
