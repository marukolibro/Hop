// Package integration runs Hop's layers against a real mihomo core and a
// local subscription server.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

	validateTimeout = 30 * time.Second
)

// profileA carries settings that would change how the core touches the machine;
// Hop must drop all of them.
const profileA = `
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

// profileB is a second provider's line, told apart by its group names.
const profileB = `
port: 7890
mixed-port: 7897
proxies:
  - {name: "🇺🇸 US 01", type: ss, server: 192.0.2.11, port: 8388, cipher: aes-128-gcm, password: x}
  - {name: "🇺🇸 US 02", type: ss, server: 192.0.2.12, port: 8388, cipher: aes-128-gcm, password: x}
proxy-groups:
  - {name: Backup, type: select, proxies: ["🇺🇸 US 01", "🇺🇸 US 02", DIRECT]}
rules:
  - MATCH,Backup
`

// profileBroken fails the core's own config test.
const profileBroken = `
proxies:
  - {name: Broken, type: no-such-type, server: 192.0.2.1, port: 8388}
rules:
  - MATCH,Broken
`

// legacyMetaBody is what the old store kept about the last download.
const legacyMetaBody = `{"userInfo":"upload=1073741824; download=32212254720; total=214748364800; expire=1796918400","updatedAt":"2026-01-02T15:04:05Z"}`

var profileByPath = map[string]string{
	"/a":      profileA,
	"/b":      profileB,
	"/broken": profileBroken,
	"/links":  linkListBody,
}

type controllerConfig struct {
	MixedPort int    `json:"mixed-port"`
	Port      int    `json:"port"`
	SocksPort int    `json:"socks-port"`
	AllowLAN  bool   `json:"allow-lan"`
	Mode      string `json:"mode"`
	Tun       struct {
		Enable bool `json:"enable"`
	} `json:"tun"`
}

// provider serves every test subscription from one local server, counts hits per
// path and can fail paths on demand; all links share one token.
type provider struct {
	server  *httptest.Server
	mu      sync.Mutex
	hits    map[string]int
	failing map[string]bool
}

func newProvider(t *testing.T) *provider {
	t.Helper()
	p := &provider{hits: map[string]int{}, failing: map[string]bool{}}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		failing := p.failing[r.URL.Path]
		if !failing {
			p.hits[r.URL.Path]++
		}
		p.mu.Unlock()
		if failing {
			// The error body echoes the request's credential: exactly what the
			// scrubbing must catch before it leaves the repository.
			http.Error(w, "provider exploding: token="+r.URL.Query().Get("token"), http.StatusInternalServerError)
			return
		}
		body, known := profileByPath[r.URL.Path]
		if !known {
			http.Error(w, "unknown subscription", http.StatusNotFound)
			return
		}
		w.Header().Set("Subscription-Userinfo", "upload=1073741824; download=32212254720; total=214748364800; expire=1796918400")
		if _, err := io.WriteString(w, body); err != nil {
			t.Errorf("write subscription body (path=%s): %v", r.URL.Path, err)
		}
	}))
	t.Cleanup(p.server.Close)
	return p
}

func (p *provider) link(path string) string {
	return p.server.URL + path + "?token=" + linkToken
}

func (p *provider) hit(path string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hits[path]
}

func (p *provider) fail(path string, on bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failing[path] = on
}

// harness wires the layers under test to a real core, with all data under
// appDir.
type harness struct {
	cfg     config.Config
	client  *mihomo.Client
	store   *profile.Store
	service *service.SubscriptionService
	// lan is the LAN access every config the service builds carries.
	lan *service.LANState
	// port is the proxy port every config the service builds carries.
	port *service.MixedPortState
}

// newHarness builds the harness around appDir, opening the store there.
func newHarness(t *testing.T, appDir string) *harness {
	t.Helper()
	binary := os.Getenv("HOP_MIHOMO_BINARY")
	if binary == "" {
		t.Skip("HOP_MIHOMO_BINARY is not set")
	}
	cfg := config.Config{
		MihomoBinary:   binary,
		AppDir:         appDir,
		HomeDir:        filepath.Join(appDir, "mihomo"),
		MixedPort:      testMixedPort,
		ControllerAddr: testController,
		Secret:         testSecret,
	}
	store, err := profile.NewStore(appDir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	client := mihomo.NewClient(testController, testSecret, mihomo.ConfigPath(cfg.HomeDir), &http.Client{}, 5*time.Second)
	settings := service.RuntimeSettings{
		MixedPort: testMixedPort, ControllerAddr: testController, ControllerSecret: testSecret,
		BindAddress: "127.0.0.1", LogLevel: "warning", BootstrapGroupName: "Hop", Mode: model.ModeRule,
	}
	lanState := service.NewLANState()
	portState := service.NewMixedPortState(testMixedPort)
	return &harness{
		cfg: cfg, client: client, store: store, lan: lanState, port: portState,
		service: service.NewSubscriptionService(
			subscription.NewFetcher(&http.Client{Timeout: 10 * time.Second}),
			store,
			store,
			client,
			mihomo.NewConfigValidator(binary, filepath.Join(appDir, "validate"), validateTimeout),
			client,
			settings,
			service.NewTunFlag(),
			lanState,
			portState,
		),
	}
}

// startCore starts the core on whatever the store says, and waits for its
// controller.
func (h *harness) startCore(t *testing.T, ctx context.Context) *mihomo.Process {
	t.Helper()
	startup, err := h.service.StartupConfig()
	if err != nil {
		t.Fatalf("startup config: %v", err)
	}
	core, err := mihomo.Start(ctx, h.cfg, startup)
	if err != nil {
		t.Fatalf("start core: %v", err)
	}
	if err := mihomo.WaitReady(ctx, h.client); err != nil {
		t.Fatalf("core not ready: %v", err)
	}
	return core
}

func TestSubscriptionsEndToEnd(t *testing.T) {
	h := newHarness(t, t.TempDir())
	p := newProvider(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	coreCtx, stopCore := context.WithCancel(ctx)
	core := h.startCore(t, coreCtx)

	// With no subscription the core runs the built-in bootstrap only.
	assertGroups(ctx, t, h.client, map[string]int{"Hop": 2})

	// The first subscription: adding it also runs it.
	if err := h.service.Add(ctx, model.SubscriptionInput{Source: model.SourceRemote, Rules: model.RulesProvider, URL: p.link("/a")}); err != nil {
		t.Fatalf("add first: %v", err)
	}
	assertGroups(ctx, t, h.client, map[string]int{"Proxy": 5, "Streaming": 2})
	view := listView(ctx, t, h.service)
	if len(view.Items) != 1 || view.ActiveID != view.Items[0].ID {
		t.Fatalf("view = %+v, want one item in use", view)
	}
	if view.Items[0].Host == "" || view.Items[0].Name == "" || !view.Items[0].HasUsage {
		t.Fatalf("first summary = %+v, want host, name and usage", view.Items[0])
	}
	if usage := view.Items[0].Usage; usage.Total != 200<<30 || usage.Download != 30<<30 {
		t.Fatalf("first summary usage = %+v, want the provider's usage header", usage)
	}
	aID := view.Items[0].ID

	// A second subscription: stored only, the running line never switches.
	if err := h.service.Add(ctx, model.SubscriptionInput{Source: model.SourceRemote, Rules: model.RulesProvider, URL: p.link("/b")}); err != nil {
		t.Fatalf("add second: %v", err)
	}
	assertGroups(ctx, t, h.client, map[string]int{"Proxy": 5, "Streaming": 2})
	view = listView(ctx, t, h.service)
	if len(view.Items) != 2 || view.ActiveID != aID {
		t.Fatalf("view = %+v, want two items with the first in use", view)
	}
	bID := view.Items[1].ID

	// The very same link cannot be added twice; different links on the same host
	// can (A and B share one).
	dupErr := h.service.Add(ctx, model.SubscriptionInput{Source: model.SourceRemote, Rules: model.RulesProvider, URL: p.link("/a")})
	if dupErr == nil {
		t.Fatal("adding the same link twice succeeded")
	}
	if strings.Contains(dupErr.Error(), linkToken) {
		t.Fatalf("duplicate error leaks the token: %v", dupErr)
	}
	badErr := h.service.Add(ctx, model.SubscriptionInput{Source: model.SourceRemote, Rules: model.RulesProvider, URL: p.server.URL + "/missing?token=" + linkToken})
	if badErr == nil {
		t.Fatal("adding an unknown subscription succeeded")
	}
	if strings.Contains(badErr.Error(), linkToken) {
		t.Fatalf("error leaks the link token: %v", badErr)
	}

	// The edit source echoes the row's name and link.
	source, err := h.service.Source(ctx, bID)
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	if source.URL != p.link("/b") || source.Name == "" {
		t.Fatalf("Source = %+v, want the stored link and name", source)
	}

	// A failed index commit must leave both the core and active ID on the
	// previous subscription.
	commitBlocker := filepath.Join(h.cfg.AppDir, "subscriptions", "index.json.tmp")
	if err := os.Mkdir(commitBlocker, 0o700); err != nil {
		t.Fatalf("block subscription index commit: %v", err)
	}
	switchErr := h.service.Use(ctx, bID)
	if err := os.Remove(commitBlocker); err != nil {
		t.Fatalf("remove subscription index blocker: %v", err)
	}
	if switchErr == nil {
		t.Fatal("switch with an unwritable index commit succeeded")
	}
	assertGroups(ctx, t, h.client, map[string]int{"Proxy": 5, "Streaming": 2})
	if rolledBack := listView(ctx, t, h.service); rolledBack.ActiveID != aID {
		t.Fatalf("active subscription after failed commit = %q, want %q", rolledBack.ActiveID, aID)
	}

	// Switching is offline: it runs the stored body and downloads nothing.
	hitsA, hitsB := p.hit("/a"), p.hit("/b")
	if err := h.service.Use(ctx, bID); err != nil {
		t.Fatalf("use second: %v", err)
	}
	assertGroups(ctx, t, h.client, map[string]int{"Backup": 3})
	if p.hit("/a") != hitsA || p.hit("/b") != hitsB {
		t.Fatalf("switching downloaded a subscription (%d, %d extra hits)", p.hit("/a")-hitsA, p.hit("/b")-hitsB)
	}
	if view = listView(ctx, t, h.service); view.ActiveID != bID {
		t.Fatalf("view = %+v, want the second in use", view)
	}

	// Every apply preserves the mode the core runs in.
	if err := h.client.SetMode(ctx, model.ModeGlobal); err != nil {
		t.Fatalf("set mode: %v", err)
	}
	if err := h.service.Update(ctx, bID); err != nil {
		t.Fatalf("update the running subscription: %v", err)
	}
	assertGroups(ctx, t, h.client, map[string]int{"Backup": 3})
	if mode := coreMode(ctx, t); mode != model.ModeGlobal {
		t.Fatalf("core mode = %q, want the live mode preserved", mode)
	}

	// Refreshing a stored-but-unused subscription leaves the core alone.
	if err := h.service.Update(ctx, aID); err != nil {
		t.Fatalf("update the unused subscription: %v", err)
	}
	assertGroups(ctx, t, h.client, map[string]int{"Backup": 3})
	if view = listView(ctx, t, h.service); view.ActiveID != bID {
		t.Fatalf("view = %+v, the unused refresh switched the line", view)
	}

	// One failure does not stop the rest: the failing row keeps its last success
	// and notes the cause without leaking the token.
	lastB := summaryByID(listView(ctx, t, h.service), bID)
	p.fail("/b", true)
	updateErr := h.service.UpdateAll(ctx)
	if updateErr == nil {
		t.Fatal("UpdateAll with a failing provider succeeded")
	}
	if strings.Contains(updateErr.Error(), linkToken) {
		t.Fatalf("UpdateAll error leaks the token: %v", updateErr)
	}
	view = listView(ctx, t, h.service)
	failedB := summaryByID(view, bID)
	if failedB.LastError == "" {
		t.Fatal("the failing row notes no error")
	}
	if strings.Contains(failedB.LastError, linkToken) {
		t.Fatalf("LastError leaks the token: %q", failedB.LastError)
	}
	if !strings.Contains(failedB.LastError, "500") {
		t.Fatalf("LastError = %q, want the status kept", failedB.LastError)
	}
	if !failedB.UpdatedAt.Equal(lastB.UpdatedAt) {
		t.Fatalf("failed refresh overwrote the last success: %v -> %v", lastB.UpdatedAt, failedB.UpdatedAt)
	}
	if fresh := summaryByID(view, aID); fresh.LastError != "" {
		t.Fatalf("the healthy row carries an error: %q", fresh.LastError)
	}
	p.fail("/b", false)
	if err := h.service.Update(ctx, bID); err != nil {
		t.Fatalf("update after recovery: %v", err)
	}
	if recovered := summaryByID(listView(ctx, t, h.service), bID); recovered.LastError != "" {
		t.Fatalf("a successful refresh kept the old error: %q", recovered.LastError)
	}

	// Restart: the core starts from the stored body and makes no request.
	hitsA, hitsB = p.hit("/a"), p.hit("/b")
	stopCore()
	if err := core.Wait(); err != nil {
		t.Fatalf("stop core: %v", err)
	}
	coreCtx, stopCore = context.WithCancel(ctx)
	core = h.startCore(t, coreCtx)
	assertGroups(ctx, t, h.client, map[string]int{"Backup": 3})
	if p.hit("/a") != hitsA || p.hit("/b") != hitsB {
		t.Fatalf("restarting downloaded a subscription")
	}
	assertCoreSettings(ctx, t)

	// Deleting the running subscription needs the successor named; it is used
	// first, then the entry goes.
	if err := h.service.Delete(ctx, bID, ""); err == nil {
		t.Fatal("deleting the running subscription without a replacement succeeded")
	}
	if err := h.service.Delete(ctx, bID, aID); err != nil {
		t.Fatalf("delete with replacement: %v", err)
	}
	assertGroups(ctx, t, h.client, map[string]int{"Proxy": 5, "Streaming": 2})
	if view = listView(ctx, t, h.service); len(view.Items) != 1 || view.ActiveID != aID {
		t.Fatalf("view = %+v, want the replacement in use", view)
	}

	// Deleting the last one: the core falls back to the bootstrap profile.
	if err := h.service.Delete(ctx, aID, ""); err != nil {
		t.Fatalf("delete last: %v", err)
	}
	assertGroups(ctx, t, h.client, map[string]int{"Hop": 2})
	if view = listView(ctx, t, h.service); len(view.Items) != 0 || view.ActiveID != "" {
		t.Fatalf("view = %+v, want an empty store", view)
	}

	// A subscription the core's own test rejects is refused at add time, and the
	// core stays as it is.
	if err := h.service.Add(ctx, model.SubscriptionInput{Source: model.SourceRemote, Rules: model.RulesProvider, URL: p.link("/broken")}); err == nil {
		t.Fatal("adding a subscription the core rejects succeeded")
	}
	if view = listView(ctx, t, h.service); len(view.Items) != 0 {
		t.Fatalf("view = %+v, the rejected add was stored", view)
	}
	assertGroups(ctx, t, h.client, map[string]int{"Hop": 2})

	stopCore()
	if err := core.Wait(); err != nil {
		t.Errorf("stop core: %v", err)
	}
}

// TestLocalProfilesEndToEnd stores a Clash profile from pasted text and a link
// list from a paste, runs both, and checks that a local profile is never
// refreshed, recorded as attempted or given a usage line.
func TestLocalProfilesEndToEnd(t *testing.T) {
	h := newHarness(t, t.TempDir())
	seedGeodata(t, h)
	p := newProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	coreCtx, stopCore := context.WithCancel(ctx)
	core := h.startCore(t, coreCtx)

	// A local Clash profile with no name takes the local label and runs at
	// once, as the first subscription does.
	if err := h.service.Add(ctx, model.SubscriptionInput{
		Source: model.SourceLocal, Rules: model.RulesProvider, Content: []byte(profileB),
	}); err != nil {
		t.Fatalf("add local clash profile: %v", err)
	}
	assertGroups(ctx, t, h.client, map[string]int{"Backup": 3})
	view := listView(ctx, t, h.service)
	if len(view.Items) != 1 || view.ActiveID != view.Items[0].ID {
		t.Fatalf("view = %+v, want the local profile in use", view)
	}
	clashID := view.Items[0].ID
	if got := view.Items[0]; got.Source != model.SourceLocal || got.Host != "" || got.HasUsage || got.Name != service.LocalProfileName {
		t.Fatalf("local summary = %+v, want local, no host or usage, and the local label", got)
	}
	clashEntry, found, err := h.store.Get(clashID)
	if err != nil || !found {
		t.Fatalf("get local clash profile: %v, %v", found, err)
	}
	if clashEntry.URL != "" || clashEntry.UserInfo != "" || !clashEntry.AttemptedAt.IsZero() || string(clashEntry.Raw) != profileB {
		t.Fatalf("stored local profile = %+v, want the body, no link, usage or attempt", clashEntry)
	}

	// A remote subscription beside it is stored only: the running local profile
	// stays.
	if err := h.service.Add(ctx, model.SubscriptionInput{Source: model.SourceRemote, Rules: model.RulesProvider, URL: p.link("/a")}); err != nil {
		t.Fatalf("add remote beside local: %v", err)
	}
	view = listView(ctx, t, h.service)
	if len(view.Items) != 2 || view.ActiveID != clashID {
		t.Fatalf("view = %+v, want the local profile still in use", view)
	}
	remoteID := view.Items[1].ID
	assertGroups(ctx, t, h.client, map[string]int{"Backup": 3})

	// A pasted link list is stored too; having no rules of its own, it runs
	// Hop's template.
	if err := h.service.Add(ctx, model.SubscriptionInput{
		Source: model.SourceLocal, Name: "粘贴的节点", Rules: model.RulesProvider, Content: []byte(linkListBody),
	}); err != nil {
		t.Fatalf("add pasted link list: %v", err)
	}
	view = listView(ctx, t, h.service)
	if len(view.Items) != 3 {
		t.Fatalf("view = %+v, want three profiles", view)
	}
	linksID := view.Items[2].ID
	if got := summaryByID(view, linksID); got.Source != model.SourceLocal || got.Rules != model.RulesHop || got.Host != "" || got.Name != "粘贴的节点" {
		t.Fatalf("pasted link list summary = %+v, want local, Hop's rules and its name", got)
	}
	assertGroups(ctx, t, h.client, map[string]int{"Backup": 3})

	// Switching to it is offline and runs the template.
	hitsA := p.hit("/a")
	if err := h.service.Use(ctx, linksID); err != nil {
		t.Fatalf("use pasted link list: %v", err)
	}
	if counts := groupCounts(ctx, t, h.client); counts["节点选择"] == 0 {
		t.Fatalf("groups = %v, want Hop's template over the pasted nodes", counts)
	}
	if p.hit("/a") != hitsA {
		t.Fatalf("switching to a local profile downloaded a subscription")
	}

	// Refreshing a local profile is refused and leaves its record alone.
	before, found, err := h.store.Get(linksID)
	if err != nil || !found {
		t.Fatalf("get pasted link list: %v, %v", found, err)
	}
	if err := h.service.Update(ctx, linksID); !errors.Is(err, service.ErrLocalProfile) {
		t.Fatalf("update local profile = %v, want ErrLocalProfile", err)
	}
	after, found, err := h.store.Get(linksID)
	if err != nil || !found {
		t.Fatalf("get pasted link list after update: %v, %v", found, err)
	}
	if !after.AttemptedAt.IsZero() || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("refused local refresh changed its record: %+v -> %+v", before, after)
	}

	// UpdateAll and AutoUpdate refresh the remote one only and record no
	// attempt for the local ones.
	if err := h.service.UpdateAll(ctx); err != nil {
		t.Fatalf("update all: %v", err)
	}
	if p.hit("/a") != hitsA+1 {
		t.Fatalf("UpdateAll fetched /a %d times, want once", p.hit("/a")-hitsA)
	}
	attempted, err := h.service.AutoUpdate(ctx, time.Nanosecond)
	if err != nil || !attempted {
		t.Fatalf("AutoUpdate = %v, %v, want the remote profile attempted", attempted, err)
	}
	if p.hit("/a") != hitsA+2 {
		t.Fatalf("AutoUpdate fetched /a %d times, want one more", p.hit("/a")-hitsA-1)
	}
	for _, id := range []string{clashID, linksID} {
		entry, found, err := h.store.Get(id)
		if err != nil || !found {
			t.Fatalf("get local profile %s: %v, %v", id, found, err)
		}
		if !entry.AttemptedAt.IsZero() {
			t.Fatalf("local profile %s recorded a refresh attempt at %v", id, entry.AttemptedAt)
		}
	}

	// Restart: the local body is all the core needs and no request is made.
	hitsA = p.hit("/a")
	stopCore()
	if err := core.Wait(); err != nil {
		t.Fatalf("stop core: %v", err)
	}
	coreCtx, stopCore = context.WithCancel(ctx)
	core = h.startCore(t, coreCtx)
	if counts := groupCounts(ctx, t, h.client); counts["节点选择"] == 0 {
		t.Fatalf("groups after restart = %v, want the stored local profile", counts)
	}
	if p.hit("/a") != hitsA {
		t.Fatalf("restarting downloaded a subscription")
	}

	// The local Clash profile runs again, then its content is replaced while it
	// is in use: the new body is checked, applied and stored with no link.
	if err := h.service.Use(ctx, clashID); err != nil {
		t.Fatalf("use local clash profile: %v", err)
	}
	assertGroups(ctx, t, h.client, map[string]int{"Backup": 3})
	source, err := h.service.Source(ctx, clashID)
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	if source.Source != model.SourceLocal || source.URL != "" || source.Name != service.LocalProfileName || len(source.Content) != 0 {
		t.Fatalf("Source = %+v, want name and source only", source)
	}
	if err := h.service.Edit(ctx, clashID, model.SubscriptionInput{
		Source: model.SourceLocal, Name: "自己写的配置", Rules: model.RulesProvider, Content: []byte(profileA),
	}); err != nil {
		t.Fatalf("replace local content: %v", err)
	}
	assertGroups(ctx, t, h.client, map[string]int{"Proxy": 5, "Streaming": 2})
	edited, found, err := h.store.Get(clashID)
	if err != nil || !found {
		t.Fatalf("get edited local profile: %v, %v", found, err)
	}
	if string(edited.Raw) != profileA || edited.URL != "" || edited.Source != model.SourceLocal || edited.UserInfo != "" {
		t.Fatalf("edited local profile = %+v, want the new body and no link or usage", edited)
	}
	if got := summaryByID(listView(ctx, t, h.service), clashID); got.Name != "自己写的配置" {
		t.Fatalf("edited name = %q", got.Name)
	}

	// A name-only edit keeps the content and the runtime; a rules change
	// reloads the stored body.
	if err := h.service.Edit(ctx, clashID, model.SubscriptionInput{
		Source: model.SourceLocal, Name: "改名后", Rules: model.RulesProvider,
	}); err != nil {
		t.Fatalf("rename local profile: %v", err)
	}
	assertGroups(ctx, t, h.client, map[string]int{"Proxy": 5, "Streaming": 2})
	renamed, found, err := h.store.Get(clashID)
	if err != nil || !found {
		t.Fatalf("get renamed local profile: %v, %v", found, err)
	}
	if string(renamed.Raw) != profileA || renamed.Name != "改名后" {
		t.Fatalf("renamed local profile = %+v, want the same body and the new name", renamed)
	}
	if err := h.service.Edit(ctx, clashID, model.SubscriptionInput{
		Source: model.SourceLocal, Name: "改名后", Rules: model.RulesHop,
	}); err != nil {
		t.Fatalf("switch local rules: %v", err)
	}
	if counts := groupCounts(ctx, t, h.client); counts["节点选择"] == 0 {
		t.Fatalf("groups = %v, want Hop's template after the rule switch", counts)
	}

	// Empty, blank and broken content is refused, and so is a source change.
	if err := h.service.Add(ctx, model.SubscriptionInput{Source: model.SourceLocal, Rules: model.RulesProvider}); err == nil {
		t.Fatal("adding an empty local config succeeded")
	}
	if err := h.service.Add(ctx, model.SubscriptionInput{Source: model.SourceLocal, Rules: model.RulesProvider, Content: []byte(" \n ")}); err == nil {
		t.Fatal("adding a blank local config succeeded")
	}
	sizeErr := h.service.Add(ctx, model.SubscriptionInput{Source: model.SourceLocal, Rules: model.RulesProvider, Content: make([]byte, service.MaxProfileSize+1)})
	if sizeErr == nil || !strings.Contains(sizeErr.Error(), "larger than") {
		t.Fatalf("adding an over-limit local config = %v, want the size limit", sizeErr)
	}
	if err := h.service.Add(ctx, model.SubscriptionInput{Source: model.SourceLocal, Rules: model.RulesProvider, Content: []byte(profileBroken)}); err == nil {
		t.Fatal("adding a local config the core rejects succeeded")
	}
	if err := h.service.Edit(ctx, clashID, model.SubscriptionInput{Source: model.SourceRemote, Name: "x", Rules: model.RulesProvider, URL: p.link("/b")}); err == nil {
		t.Fatal("changing a profile's source succeeded")
	}
	if got := summaryByID(listView(ctx, t, h.service), remoteID); got.Source != model.SourceRemote || got.Host == "" || !got.HasUsage {
		t.Fatalf("remote summary = %+v, want its host and usage still", got)
	}

	stopCore()
	if err := core.Wait(); err != nil {
		t.Errorf("stop core: %v", err)
	}
}

func TestLegacyStoreMigration(t *testing.T) {
	appDir := t.TempDir()
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	link := dead.URL + "/legacy?token=" + linkToken
	dead.Close()
	if err := os.WriteFile(filepath.Join(appDir, "subscription.url"), []byte(link), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "subscription.yaml"), []byte(profileA), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "subscription.meta"), []byte(legacyMetaBody), 0o600); err != nil {
		t.Fatal(err)
	}

	h := newHarness(t, appDir)

	entries, activeID, err := h.store.Entries()
	if err != nil {
		t.Fatalf("entries after migration: %v", err)
	}
	if len(entries) != 1 || activeID != entries[0].ID {
		t.Fatalf("entries = %+v active %q, want the legacy one in use", entries, activeID)
	}
	if entries[0].Source != model.SourceRemote || entries[0].URL != link || entries[0].Name != strings.TrimPrefix(dead.URL, "http://") {
		t.Fatalf("migrated entry = %+v, want the legacy link, its host as name and a remote source", entries[0])
	}
	migrated, found, err := h.store.Get(activeID)
	if err != nil || !found {
		t.Fatalf("get migrated: %v, %v", found, err)
	}
	if string(migrated.Raw) != profileA || migrated.UserInfo == "" {
		t.Fatalf("migrated body or usage = %d bytes, %q", len(migrated.Raw), migrated.UserInfo)
	}

	// Migration is idempotent: opened again, the legacy files are gone.
	if _, err := profile.NewStore(appDir); err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	if _, err := os.Stat(filepath.Join(appDir, "subscription.yaml")); !os.IsNotExist(err) {
		t.Fatalf("legacy file survived migration: %v", err)
	}

	// An index written before local profiles existed has no source field:
	// every entry in it is a downloaded subscription.
	indexPath := filepath.Join(appDir, "subscriptions", "index.json")
	rawIndex, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(rawIndex, &doc); err != nil {
		t.Fatalf("decode index: %v", err)
	}
	stored, ok := doc["entries"].([]any)
	if !ok || len(stored) != 1 {
		t.Fatalf("index entries = %v, want one", doc["entries"])
	}
	storedEntry, ok := stored[0].(map[string]any)
	if !ok {
		t.Fatalf("index entry = %v, want an object", stored[0])
	}
	delete(storedEntry, "source")
	rawIndex, err = json.Marshal(doc)
	if err != nil {
		t.Fatalf("encode index: %v", err)
	}
	if err := os.WriteFile(indexPath, rawIndex, 0o600); err != nil {
		t.Fatalf("write index without source: %v", err)
	}
	entries, activeID, err = h.store.Entries()
	if err != nil {
		t.Fatalf("entries without source: %v", err)
	}
	if len(entries) != 1 || activeID != entries[0].ID || entries[0].Source != model.SourceRemote {
		t.Fatalf("entries = %+v active %q, want the old entry as remote", entries, activeID)
	}

	// Restart does not re-download: the link's host is dead, the core runs the
	// stored body.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	coreCtx, stopCore := context.WithCancel(ctx)
	core := h.startCore(t, coreCtx)
	defer func() {
		stopCore()
		if err := core.Wait(); err != nil {
			t.Errorf("stop core: %v", err)
		}
	}()
	assertGroups(ctx, t, h.client, map[string]int{"Proxy": 5, "Streaming": 2})
}

func TestConfigValidator(t *testing.T) {
	binary := os.Getenv("HOP_MIHOMO_BINARY")
	if binary == "" {
		t.Skip("HOP_MIHOMO_BINARY is not set")
	}
	// Only the filtered runtime config is checked, byte for byte what would be
	// handed to the core.
	settings := service.RuntimeSettings{
		MixedPort: testMixedPort, ControllerAddr: testController, ControllerSecret: testSecret,
		BindAddress: "127.0.0.1", LogLevel: "warning", BootstrapGroupName: "Hop", Mode: model.ModeRule,
	}
	valid, err := service.RuntimeConfig([]byte(profileA), model.RulesProvider, nil, settings)
	if err != nil {
		t.Fatalf("build runtime config: %v", err)
	}
	broken, err := service.RuntimeConfig([]byte(profileBroken), model.RulesProvider, nil, settings)
	if err != nil {
		t.Fatalf("build broken runtime config: %v", err)
	}
	validator := mihomo.NewConfigValidator(binary, filepath.Join(t.TempDir(), "validate"), validateTimeout)
	ctx, cancel := context.WithTimeout(context.Background(), validateTimeout)
	defer cancel()
	if err := validator.Validate(ctx, valid); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if err := validator.Validate(ctx, broken); err == nil {
		t.Fatal("broken config accepted")
	}
}

func listView(ctx context.Context, t *testing.T, svc *service.SubscriptionService) model.SubscriptionsView {
	t.Helper()
	view, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return view
}

func summaryByID(view model.SubscriptionsView, id string) model.SubscriptionSummary {
	for _, item := range view.Items {
		if item.ID == id {
			return item
		}
	}
	return model.SubscriptionSummary{}
}

func groupCounts(ctx context.Context, t *testing.T, client *mihomo.Client) map[string]int {
	t.Helper()
	snapshot, err := client.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	counts := make(map[string]int, len(snapshot.Groups))
	for _, group := range snapshot.Groups {
		counts[group.Name] = len(group.Options)
	}
	return counts
}

func assertGroups(ctx context.Context, t *testing.T, client *mihomo.Client, want map[string]int) {
	t.Helper()
	if counts := groupCounts(ctx, t, client); !maps.Equal(counts, want) {
		t.Fatalf("groups = %v, want %v", counts, want)
	}
}

func coreMode(ctx context.Context, t *testing.T) string {
	t.Helper()
	return fetchCoreConfig(ctx, t).Mode
}

// assertCoreSettings reads the running core's own view of its config.
func assertCoreSettings(ctx context.Context, t *testing.T) {
	t.Helper()
	got := fetchCoreConfig(ctx, t)
	if got.MixedPort != testMixedPort || got.Port != 0 || got.SocksPort != 0 || got.AllowLAN || got.Tun.Enable {
		t.Fatalf("core config = %+v; the subscription's settings leaked through", got)
	}
}

func fetchCoreConfig(ctx context.Context, t *testing.T) controllerConfig {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+testController+"/configs", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testSecret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("read core config: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("close core config response: %v", err)
		}
	}()
	var got controllerConfig
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode core config: %v", err)
	}
	return got
}
