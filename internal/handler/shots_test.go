package handler

import (
	"context"
	"fmt"
	"image/png"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"hop/internal/model"
	"hop/internal/service"
)

// TestShots renders the pictures of the READMEs and the website, and the pages
// to look at while changing the UI, from the UI itself with sample data. It
// does nothing unless HOP_SHOTS_DIR names the folder to write to:
//
//	HOP_SHOTS_DIR=/tmp/shots MYGO_LOCALE=zh-Hans go test ./internal/handler -run TestShots -count=1
//	HOP_SHOTS_DIR=/tmp/shots MYGO_LOCALE=en      go test ./internal/handler -run TestShots -count=1
//
// The pictures land in <dir>/<zh|en>/<name>-<light|dark>.png at the window's
// own size at twice the scale. docs/screenshots and docs/screenshots/en take
// the names nodes, connections, stats, add-rule and menubar; activity-log is
// for looking at only. Convert them for the website with
// `cwebp -q 84 -alpha_q 90`. Nothing here talks to a core or to the system.

type shotPort struct{}

func (shotPort) Current() int                            { return 17890 }
func (shotPort) Skipped() int                            { return 0 }
func (shotPort) Change(ctx context.Context, p int) error { return nil }

// shotNames are the names in the sample data, which the pictures show in the
// language of the run.
type shotNames struct {
	lang                            string
	provider                        string
	main, auto                      string
	ai, streaming, telegram, google string
	hk, jp, sg, us                  string
}

var (
	shotZH = shotNames{"zh", "示例机场", "节点选择", "自动选择", "AI 服务", "流媒体", "Telegram", "谷歌", "香港", "日本", "新加坡", "美国"}
	shotEN = shotNames{"en", "Sample provider", "Proxy", "Auto", "AI", "Streaming", "Telegram", "Google", "Hong Kong", "Japan", "Singapore", "United States"}
)

// shotDashboard is a dashboard that carries traffic through Singapore, with
// the sample provider's groups, nodes, services and history.
func shotDashboard(n shotNames, page int) *Dashboard {
	settings := service.DefaultSettings()
	settings.Theme = model.ThemePaper
	d := &Dashboard{services: Services{Port: shotPort{}}, status: statusConnected, settings: newSettingsState(settings), page: page}
	d.core.loaded = true
	d.tun = tunState{loaded: true}
	d.sysProxy = sysProxyState{loaded: true, on: true, status: model.SystemProxyStatus{Owner: model.ProxyOwnerHop}}
	d.home = homeState{point: model.GeoPoint{Lon: 121.47, Lat: 31.23}, known: true}
	d.last = model.Traffic{Down: 2457000, Up: 88000}
	d.memory, d.memoryKnown = model.Memory{App: 92 << 20, Core: 64 << 20}, true
	d.rules.loaded = true
	d.proxyHost = "127.0.0.1"

	type region struct {
		flag, name string
		delays     []int
	}
	regions := []region{
		{"🇭🇰", n.hk, []int{58, 71, 96, 0}},
		{"🇯🇵", n.jp, []int{84, 112, 139, 220}},
		{"🇸🇬", n.sg, []int{46, 62, 90}},
		{"🇺🇸", n.us, []int{138, 151}},
	}
	snap := model.Snapshot{Delays: map[string]int{}, Facts: map[string]model.ProxyFacts{}}
	var all []string
	for _, r := range regions {
		for i, delay := range r.delays {
			suffix := "|IEPL"
			if r.name == n.hk && i == 2 {
				suffix = "|2x"
			}
			if r.name == n.jp && i > 0 {
				suffix = ""
			}
			name := fmt.Sprintf("%s %s %02d%s", r.flag, r.name, i+1, suffix)
			all = append(all, name)
			snap.Delays[name] = delay
			snap.Facts[name] = model.ProxyFacts{Protocol: "Shadowsocks", UDP: true}
		}
	}
	singapore := "🇸🇬 " + n.sg + " 01|IEPL"
	snap.Groups = []model.ProxyGroup{
		{Name: n.main, Type: model.GroupSelector, Selected: n.auto, Options: append([]string{n.auto, n.hk, n.jp, n.us, "DIRECT"}, all...)},
		{Name: n.auto, Type: model.GroupURLTest, Selected: singapore, Options: all},
		{Name: n.hk, Type: model.GroupURLTest, Selected: "🇭🇰 " + n.hk + " 01|IEPL", Options: all[:4]},
		{Name: n.jp, Type: model.GroupURLTest, Selected: "🇯🇵 " + n.jp + " 01|IEPL", Options: all[4:8]},
		{Name: n.us, Type: model.GroupURLTest, Selected: "🇺🇸 " + n.us + " 01|IEPL", Options: all[11:]},
		{Name: n.ai, Type: model.GroupSelector, Selected: n.main, Options: []string{n.main, singapore}},
		{Name: n.streaming, Type: model.GroupSelector, Selected: n.main, Options: []string{n.main}},
		{Name: n.telegram, Type: model.GroupSelector, Selected: n.main, Options: []string{n.main}},
		{Name: n.google, Type: model.GroupSelector, Selected: n.main, Options: []string{n.main}},
	}
	d.view = service.BuildView(snap, model.ModeRule)
	d.activeGroup = n.main
	d.exitIP = exitIPState{status: exitIPReady, ip: model.ExitIP{Address: netip.MustParseAddr("203.0.113.42"), Country: "SG"}}
	d.subscription = subscriptionsState{loaded: true, activeChoice: "a", view: model.SubscriptionsView{ActiveID: "a", Items: []model.SubscriptionSummary{{
		ID: "a", Name: n.provider, Source: model.SourceRemote, Host: "example.com", Rules: model.RulesHop,
		HasUsage: true, Usage: model.Usage{Download: 160 << 30, Total: 500 << 30, Expire: time.Now().AddDate(0, 0, 124)},
	}}}}
	d.unlock.services.results = map[model.UnlockService]model.UnlockResult{}
	for i, s := range model.UnlockServices {
		d.unlock.services.results[s] = model.UnlockResult{Service: s, State: model.UnlockAvailable, Region: "SG", Latency: time.Duration(120+i*20) * time.Millisecond}
	}
	d.unlock.services.lastRun = time.Now()
	d.connections.data = shotConnections(n)
	d.logs.entries = shotLogs()
	d.stats = shotStats(n)
	return d
}

// shotStats is thirty days of traffic and this month's, for the one provider.
func shotStats(n shotNames) model.TrafficView {
	now := time.Now()
	proxy := []int64{9, 10, 6, 4, 4, 6, 6, 3, 2, 5, 8, 11, 7, 3, 3, 4, 6, 4, 2, 2, 5, 10, 9, 5, 3, 3, 5, 4, 2, 1}
	direct := []int64{1, 1, 2, 4, 4, 3, 3, 4, 5, 4, 1, 1, 2, 4, 4, 3, 4, 5, 5, 3, 2, 1, 1, 3, 4, 4, 3, 2, 5, 6}
	var days []model.TrafficDayView
	for i := range proxy {
		day := now.AddDate(0, 0, i-len(proxy)+1).Format("2006-01-02")
		days = append(days, model.TrafficDayView{Date: day, Split: model.TrafficSplit{
			ProxyDown: proxy[i] << 28, ProxyUp: proxy[i] << 25, DirectDown: direct[i] << 28, DirectUp: direct[i] << 25,
		}})
	}
	return model.TrafficView{
		Today: model.TrafficSplit{ProxyDown: 700 << 20, ProxyUp: 45 << 20, DirectDown: 2 << 30, DirectUp: 80 << 20},
		Month: model.TrafficSplit{ProxyDown: 46 << 30, ProxyUp: 3 << 30, DirectDown: 27 << 30, DirectUp: 1700 << 20},
		Days:  days,
		Sources: []model.TrafficSourceView{{ID: "a", Name: n.provider, Split: model.TrafficSplit{
			ProxyDown: 46 << 30, ProxyUp: 3 << 30,
		}}},
	}
}

// shotConnections are six connections of ordinary apps, two of them direct.
func shotConnections(n shotNames) model.Connections {
	via := []string{"🇸🇬 " + n.sg + " 01"}
	row := func(id, host, process, rule string, direct bool, down, up int64) model.Connection {
		c := model.Connection{ID: id, Host: host, Port: "443", Network: "tcp", Process: process, Rule: rule, Chain: via, Download: down, Upload: up}
		if direct {
			c.Chain = []string{"DIRECT"}
		}
		return c
	}
	return model.Connections{Active: []model.Connection{
		row("1", "api.openai.com", "Google Chrome", "RuleSet(ai)", false, 4<<20, 310<<10),
		row("2", "github.com", "git", "DomainSuffix(github.com)", false, 18<<20, 2<<20),
		row("3", "api.telegram.org", "Telegram", "RuleSet(telegram)", false, 640<<10, 90<<10),
		row("4", "www.baidu.com", "Google Chrome", "GeoSite(cn)", true, 1<<20, 80<<10),
		row("5", "updates.example.org", "Slack", "Match()", false, 3<<20, 200<<10),
		row("6", "gateway.icloud.com", "", "DomainSuffix(icloud.com)", true, 120<<10, 40<<10),
	}}
}

// shotLogs are a few lines of the core's log at each level.
func shotLogs() []model.LogEntry {
	now := time.Now()
	line := func(ago time.Duration, level string, message string) model.LogEntry {
		return model.LogEntry{Time: now.Add(-ago), Level: level, Message: message}
	}
	return []model.LogEntry{
		line(9*time.Minute, model.LogInfo, "Start initial configuration in progress"),
		line(9*time.Minute, model.LogInfo, "Mixed(http+socks) proxy listening at: 127.0.0.1:17890"),
		line(8*time.Minute, model.LogInfo, "Geodata ready"),
		line(5*time.Minute, model.LogWarning, "dial tcp 203.0.113.9:443: i/o timeout"),
		line(2*time.Minute, model.LogInfo, "Sample provider refreshed"),
		line(40*time.Second, model.LogError, "check prime: the page names no territory"),
	}
}

// shotRender draws view in one appearance at the given size, twice the scale.
func shotRender(t *testing.T, path string, view func(c *ui.Context), dark bool, width, height int, titleBar bool) {
	t.Helper()
	tester := ui.NewTester(view, width, height)
	tester.SetScale(2)
	if titleBar {
		tester.SetTitleBar(ui.TitleBar{Height: 28, Left: 70})
	}
	tester.SetDark(dark)
	tester.SetVibrancy(false)
	tester.Advance(2 * time.Second)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, tester.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestShots(t *testing.T) {
	root := os.Getenv("HOP_SHOTS_DIR")
	if root == "" {
		t.Skip("HOP_SHOTS_DIR is not set")
	}
	names := shotZH
	if os.Getenv("MYGO_LOCALE") == "en" {
		names = shotEN
	}
	dir := filepath.Join(root, names.lang)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const width, height = 1040, 700
	for _, dark := range []bool{false, true} {
		scheme := "light"
		if dark {
			scheme = "dark"
		}
		window := func(name string, page int, prepare func(d *Dashboard)) {
			d := shotDashboard(names, page)
			if prepare != nil {
				prepare(d)
			}
			shotRender(t, filepath.Join(dir, name+"-"+scheme+".png"), func(c *ui.Context) { d.View(c) }, dark, width, height, true)
		}
		window("nodes", pageNodes, nil)
		window("connections", pageActivity, nil)
		window("activity-log", pageActivity, func(d *Dashboard) { d.activity = activityLog })
		window("stats", pageStats, nil)
		window("settings", pageSettings, nil)
		window("add-rule", pageActivity, func(d *Dashboard) { d.openRuleEditorFor(d.connections.data.Active[2]) })

		// The panel is as tall as its content, which it reports once drawn.
		panel := shotDashboard(names, pageNodes)
		fit := flyoutHeight
		show := func(c *ui.Context) { panel.flyoutView(c, func() {}, func() {}, func(h int) { fit = h }) }
		shotRender(t, filepath.Join(dir, "menubar-"+scheme+".png"), show, dark, flyoutWidth, flyoutHeight, false)
		shotRender(t, filepath.Join(dir, "menubar-"+scheme+".png"), show, dark, flyoutWidth, fit, false)
	}
}
