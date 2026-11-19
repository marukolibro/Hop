package integration

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hop/internal/model"
	"hop/internal/repository/mihomo"
	"hop/internal/service"
)

// routeProfile puts an automatic group before the manual selector, while
// the default rule reaches the selector through another group.
const routeProfile = `
proxies:
  - {name: "HK · PCCW", type: http, server: 127.0.0.1, port: 1}
  - {name: "Japan · konan", type: http, server: 127.0.0.1, port: 1}
proxy-groups:
  - name: Auto
    type: url-test
    proxies: ["HK · PCCW"]
    url: http://127.0.0.1:1
    interval: 86400
    lazy: true
  - {name: Proxy, type: select, proxies: ["HK · PCCW", "Japan · konan", Auto]}
  - {name: Media, type: select, proxies: ["HK · PCCW", "Japan · konan"]}
  - {name: CatchAll, type: select, proxies: [Proxy, DIRECT]}
rules:
  - DOMAIN,media.invalid,Media
  - MATCH,CatchAll
`

// ruleReads counts the controller's /rules requests while forwarding every
// request unchanged.
type ruleReads struct {
	http.RoundTripper
	count atomic.Int32
}

func (r *ruleReads) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Path == "/rules" {
		r.count.Add(1)
	}
	return r.RoundTripper.RoundTrip(req)
}

// TestDefaultRouteFollowsCore checks the route the window and the menu bar
// show against the core's default rule, whatever order the profile declares
// its groups in.
func TestDefaultRouteFollowsCore(t *testing.T) {
	h := newHarness(t, t.TempDir())
	reads := &ruleReads{RoundTripper: http.DefaultTransport.(*http.Transport).Clone()}
	h.client = mihomo.NewClient(testController, testSecret, mihomo.ConfigPath(h.cfg.HomeDir), &http.Client{Transport: reads}, 5*time.Second)
	// The last scenario loads 60k rules.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	settings := service.RuntimeSettings{
		MixedPort: testMixedPort, ControllerAddr: testController, ControllerSecret: testSecret,
		BindAddress: "127.0.0.1", LogLevel: "warning", BootstrapGroupName: "Hop", Mode: model.ModeRule,
	}
	build := func(t *testing.T, profile string) []byte {
		t.Helper()
		config, err := service.RuntimeConfig([]byte(profile), model.RulesProvider, nil, settings)
		if err != nil {
			t.Fatalf("build route config: %v", err)
		}
		return config
	}
	runnerCtx, stop := context.WithCancel(ctx)
	runner := mihomo.NewRunner(runnerCtx, h.cfg, h.client)
	if err := runner.Start(build(t, routeProfile)); err != nil {
		stop()
		t.Fatalf("start core: %v", err)
	}
	defer func() {
		stop()
		if err := runner.Stop(); err != nil {
			t.Errorf("stop core: %v", err)
		}
	}()
	proxies := service.NewProxyService(h.client)
	// epoch stands for the dashboard's runtime epoch, which every selection,
	// mode change and config apply moves.
	epoch := 0
	selectNode := func(t *testing.T, group, node string) {
		t.Helper()
		epoch++
		if err := proxies.Select(ctx, group, node); err != nil {
			t.Fatalf("select %q in %q: %v", node, group, err)
		}
	}
	assertRoute := func(t *testing.T, want ...string) model.ProxyView {
		t.Helper()
		view, err := proxies.View(ctx, epoch)
		if err != nil {
			t.Fatalf("read proxy view: %v", err)
		}
		var names []string
		for _, hop := range view.Route {
			names = append(names, hop.Name)
		}
		if got := strings.Join(names, " > "); got != strings.Join(want, " > ") {
			t.Errorf("default route = %q, want %q", got, strings.Join(want, " > "))
		}
		if len(view.Route) > 0 {
			exit := view.Route[len(view.Route)-1]
			if exit.Name == "DIRECT" {
				if exit.Node.Via == "" {
					t.Error("built-in exit has no description")
				}
			} else if exit.Name != "REJECT" && (exit.Node.Protocol == "" || exit.Node.Title != exit.Name) {
				t.Errorf("exit lost its node details: %+v", exit.Node)
			}
		}
		return view
	}

	t.Run("manual default differs from the first automatic group", func(t *testing.T) {
		selectNode(t, "Proxy", "Japan · konan")
		view := assertRoute(t, "CatchAll", "Proxy", "Japan · konan")
		for _, group := range view.Groups {
			if group.Name == "Auto" && group.Kind != model.GroupKindAuto {
				t.Errorf("automatic group kind = %q, want %q", group.Kind, model.GroupKindAuto)
			}
			if group.Name == "Proxy" && group.Kind != model.GroupKindMain {
				t.Errorf("manual group kind = %q, want %q", group.Kind, model.GroupKindMain)
			}
		}
	})
	t.Run("changing a service route preserves the default exit", func(t *testing.T) {
		selectNode(t, "Media", "HK · PCCW")
		assertRoute(t, "CatchAll", "Proxy", "Japan · konan")
	})
	t.Run("switching the default node refreshes the exit", func(t *testing.T) {
		selectNode(t, "Proxy", "HK · PCCW")
		assertRoute(t, "CatchAll", "Proxy", "HK · PCCW")
		selectNode(t, "Proxy", "Auto")
		assertRoute(t, "CatchAll", "Proxy", "Auto", "HK · PCCW")
	})
	t.Run("global and direct modes use their own route", func(t *testing.T) {
		selectNode(t, "GLOBAL", "Japan · konan")
		for _, mode := range []string{model.ModeGlobal, model.ModeDirect} {
			epoch++
			if err := h.client.SetMode(ctx, mode); err != nil {
				t.Fatalf("set mode %q: %v", mode, err)
			}
			if mode == model.ModeGlobal {
				assertRoute(t, "GLOBAL", "Japan · konan")
			} else {
				assertRoute(t, "DIRECT")
			}
		}
		epoch++
		if err := h.client.SetMode(ctx, model.ModeRule); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("the rules are read once an epoch", func(t *testing.T) {
		epoch++
		before := reads.count.Load()
		for range 3 {
			assertRoute(t, "CatchAll", "Proxy", "Auto", "HK · PCCW")
		}
		if got := reads.count.Load() - before; got != 1 {
			t.Errorf("/rules requests in one epoch = %d, want 1", got)
		}
		selectNode(t, "Proxy", "Japan · konan")
		assertRoute(t, "CatchAll", "Proxy", "Japan · konan")
		if got := reads.count.Load() - before; got != 2 {
			t.Errorf("/rules requests over two epochs = %d, want 2", got)
		}
	})

	var manyRules strings.Builder
	manyRules.WriteString(strings.Split(routeProfile, "rules:\n")[0])
	manyRules.WriteString("rules:\n")
	for i := range 60000 {
		fmt.Fprintf(&manyRules, "  - DOMAIN,inline-rule-%05d.example.invalid,DIRECT\n", i)
	}
	manyRules.WriteString("  - MATCH,Japan · konan\n")
	for _, scenario := range []struct {
		name    string
		profile string
		want    []string
	}{
		{"default rule names a node", strings.ReplaceAll(routeProfile, "MATCH,CatchAll", "MATCH,Japan · konan"), []string{"Japan · konan"}},
		{"default rule names DIRECT", strings.ReplaceAll(routeProfile, "MATCH,CatchAll", "MATCH,DIRECT"), []string{"DIRECT"}},
		{"default rule names REJECT", strings.ReplaceAll(routeProfile, "MATCH,CatchAll", "MATCH,REJECT"), []string{"REJECT"}},
		{"no default rule means DIRECT", strings.ReplaceAll(routeProfile, "  - MATCH,CatchAll\n", ""), []string{"DIRECT"}},
		{"first default rule wins", strings.ReplaceAll(routeProfile, "MATCH,CatchAll", "MATCH,Japan · konan\n  - MATCH,DIRECT"), []string{"Japan · konan"}},
		{"default rule names GLOBAL", strings.ReplaceAll(routeProfile, "MATCH,CatchAll", "MATCH,GLOBAL"), []string{"GLOBAL", "Japan · konan"}},
		{"custom GLOBAL omits the default group", strings.ReplaceAll(routeProfile, "proxy-groups:\n", "proxy-groups:\n  - {name: GLOBAL, type: select, proxies: [DIRECT]}\n"), []string{"CatchAll", "Proxy", "Japan · konan"}},
		{"many inline rules", manyRules.String(), []string{"Japan · konan"}},
		{"a node needs no groups", "proxies:\n  - {name: \"Japan · konan\", type: http, server: 127.0.0.1, port: 1}\nrules:\n  - MATCH,Japan · konan\n", []string{"Japan · konan"}},
		{"DIRECT needs no groups", "proxies:\n  - {name: \"Japan · konan\", type: http, server: 127.0.0.1, port: 1}\nrules:\n  - MATCH,DIRECT\n", []string{"DIRECT"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			epoch++
			if err := h.client.ApplyConfig(ctx, build(t, scenario.profile)); err != nil {
				t.Fatalf("apply route config: %v", err)
			}
			if err := h.client.SetMode(ctx, model.ModeRule); err != nil {
				t.Fatalf("restore rule mode: %v", err)
			}
			if scenario.name == "default rule names GLOBAL" {
				selectNode(t, "GLOBAL", "Japan · konan")
			}
			if scenario.name == "custom GLOBAL omits the default group" {
				selectNode(t, "Proxy", "Japan · konan")
			}
			assertRoute(t, scenario.want...)
		})
	}
}
