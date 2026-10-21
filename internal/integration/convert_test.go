package integration

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"hop/internal/model"
	"hop/internal/repository/mihomo"
)

// ssLink is a SIP002 shadowsocks link to a documentation address.
func ssLink(host string, name string) string {
	return "ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:x")) + "@" + host + ":8388#" + name
}

// linkListBody is a V2Ray-style subscription: base64 of one node link per
// line, with a flag name, a bare region code, a city, and the account note
// providers add as a fake node.
var linkListBody = base64.StdEncoding.EncodeToString([]byte(strings.Join([]string{
	ssLink("192.0.2.21", "%F0%9F%87%AF%F0%9F%87%B5%20Tokyo%2001"),
	ssLink("192.0.2.22", "HK%2002"),
	"trojan://x@192.0.2.23:443?sni=example.com#Los%20Angeles%2003",
	ssLink("192.0.2.24", "%E5%89%A9%E4%BD%99%E6%B5%81%E9%87%8F%EF%BC%9A100GB"),
}, "\n")))

// seedGeodata puts the geodata that ships with Hop where the core and its
// config checks read it, as main does: Hop's template has GEOSITE rules.
func seedGeodata(t *testing.T, h *harness) {
	t.Helper()
	src := filepath.Join("..", "..", "resources", "geodata")
	if _, err := os.Stat(filepath.Join(src, "GeoSite.dat")); err != nil {
		t.Fatalf("no bundled geodata (run scripts/fetch-mihomo.sh darwin-arm64): %v", err)
	}
	for _, dir := range []string{h.cfg.HomeDir, filepath.Join(h.cfg.AppDir, "validate")} {
		if err := mihomo.SeedGeodata(src, dir); err != nil {
			t.Fatal(err)
		}
	}
}

// TestHopRulesTemplate imports a link list, which runs with Hop's template,
// then switches a Clash profile from its own rules to Hop's.
func TestHopRulesTemplate(t *testing.T) {
	h := newHarness(t, t.TempDir())
	seedGeodata(t, h)
	p := newProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	coreCtx, stopCore := context.WithCancel(ctx)
	core := h.startCore(t, coreCtx)
	defer func() {
		stopCore()
		if err := core.Wait(); err != nil {
			t.Errorf("stop core: %v", err)
		}
	}()

	// A link list has no rules of its own: asked for the provider's, it
	// runs with Hop's template.
	if err := h.service.Add(ctx, model.SubscriptionInput{Rules: model.RulesProvider, URL: p.link("/links")}); err != nil {
		t.Fatalf("add link list: %v", err)
	}
	view, err := h.service.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := view.Items[0].Rules; got != model.RulesHop {
		t.Fatalf("link list runs with rules %q, want %q", got, model.RulesHop)
	}
	snapshot, err := h.client.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	options := map[string][]string{}
	for i, g := range snapshot.Groups {
		options[g.Name] = g.Options
		if i == 0 && g.Name != "节点选择" {
			t.Errorf("first group = %q, want 节点选择 (the dashboard's main group)", g.Name)
		}
	}
	want := map[string][]string{
		"自动选择":     {"🇯🇵 Tokyo 01", "HK 02", "Los Angeles 03"},
		"🇭🇰 香港":    {"HK 02"},
		"🇯🇵 日本":    {"🇯🇵 Tokyo 01"},
		"🇺🇸 美国":    {"Los Angeles 03"},
		"AI":       {"🇺🇸 美国", "🇯🇵 日本", "节点选择"},
		"苹果":       {"DIRECT", "节点选择"},
		"Telegram": {"节点选择", "🇭🇰 香港", "🇯🇵 日本", "🇺🇸 美国"},
	}
	for group, wantOptions := range want {
		if got := options[group]; !slices.Equal(got, wantOptions) {
			t.Errorf("group %s = %v, want %v", group, got, wantOptions)
		}
	}
	if _, ok := options["其他地区"]; ok {
		t.Error("every node has a region, yet 其他地区 was made")
	}
	if config := readCoreConfig(t, h); !strings.Contains(config, "GEOSITE,gfw,节点选择") || !strings.Contains(config, "GEOIP,CN,DIRECT") || !strings.Contains(config, "MATCH,节点选择") {
		t.Errorf("Hop's template rules missing from the running config:\n%s", config)
	} else if github, microsoft := strings.Index(config, "GEOSITE,github,节点选择"), strings.Index(config, "GEOSITE,microsoft,"); github < 0 || github > microsoft {
		// geosite's microsoft list holds GitHub, which direct cannot reach.
		t.Errorf("GitHub must go through a node before the microsoft rule sends it direct:\n%s", config)
	}

	// A Clash profile switched to Hop's template keeps its nodes and loses
	// its own groups.
	if err := h.service.Add(ctx, model.SubscriptionInput{Rules: model.RulesProvider, URL: p.link("/a")}); err != nil {
		t.Fatalf("add clash profile: %v", err)
	}
	view, err = h.service.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var clashID string
	for _, item := range view.Items {
		if item.Rules == model.RulesProvider {
			clashID = item.ID
		}
	}
	if err := h.service.Use(ctx, clashID); err != nil {
		t.Fatalf("use clash profile: %v", err)
	}
	if _, ok := groupCounts(ctx, t, h.client)["Proxy"]; !ok {
		t.Fatal("the Clash profile does not run its own groups")
	}
	source, err := h.service.Source(ctx, clashID)
	if err != nil {
		t.Fatal(err)
	}
	source.Rules = model.RulesHop
	if err := h.service.Edit(ctx, clashID, source); err != nil {
		t.Fatalf("switch to Hop's rules: %v", err)
	}
	counts := groupCounts(ctx, t, h.client)
	if _, ok := counts["Proxy"]; ok {
		t.Error("the provider's groups still run under Hop's rules")
	}
	if counts["节点选择"] == 0 {
		t.Errorf("Hop's groups do not run: %v", counts)
	}
	if config := readCoreConfig(t, h); !strings.Contains(config, "MATCH,节点选择") {
		t.Errorf("Hop's template rules missing from the running config:\n%s", config)
	}
}

// readCoreConfig is the config file the core runs.
func readCoreConfig(t *testing.T, h *harness) string {
	t.Helper()
	raw, err := os.ReadFile(mihomo.ConfigPath(h.cfg.HomeDir))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
