package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
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

// ruleCounter counts actual controller requests while forwarding them unchanged.
type ruleCounter struct {
	http.RoundTripper
	reads         atomic.Int32
	failNext      atomic.Bool
	blockNext     atomic.Bool
	blocked       chan struct{}
	release       chan struct{}
	lateApply     atomic.Bool
	configPath    string
	applyRelease  chan struct{}
	applyFinished chan error
}

func (c *ruleCounter) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodPut && req.URL.Path == "/configs" && c.lateApply.Swap(false) {
		return nil, c.delayConfig(req)
	}
	isRules := req.URL.Path == "/rules"
	if isRules {
		c.reads.Add(1)
		if c.failNext.Swap(false) {
			return nil, errors.New("controller request failed in test")
		}
	}
	response, err := c.RoundTripper.RoundTrip(req)
	if err != nil || !isRules || !c.blockNext.Swap(false) {
		return response, err
	}
	// Capture the real old response before allowing a concurrent config reload.
	body, readErr := io.ReadAll(response.Body)
	if err := errors.Join(readErr, response.Body.Close()); err != nil {
		return nil, err
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	close(c.blocked)
	select {
	case <-c.release:
		return response, nil
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
}

// delayConfig models a core that parsed the new file before the caller lost
// its reply, then applies those captured bytes after the caller has returned.
func (c *ruleCounter) delayConfig(req *http.Request) error {
	config, err := os.ReadFile(c.configPath)
	if err != nil {
		return err
	}
	body, err := json.Marshal(struct {
		Payload string `json:"payload"`
	}{string(config)})
	if err != nil {
		return err
	}
	if err := req.Body.Close(); err != nil {
		return err
	}
	delayed := req.Clone(context.WithoutCancel(req.Context()))
	delayed.Body = io.NopCloser(bytes.NewReader(body))
	delayed.ContentLength = int64(len(body))
	go func() {
		ctx, cancel := context.WithTimeout(delayed.Context(), 5*time.Second)
		defer cancel()
		select {
		case <-c.applyRelease:
		case <-ctx.Done():
			c.applyFinished <- ctx.Err()
			return
		}
		response, err := c.RoundTripper.RoundTrip(delayed.WithContext(ctx))
		if response != nil {
			if response.StatusCode != http.StatusNoContent {
				err = errors.Join(err, fmt.Errorf("delayed config answered %d", response.StatusCode))
			}
			err = errors.Join(err, response.Body.Close())
		}
		c.applyFinished <- err
	}()
	return context.DeadlineExceeded
}

// TestDefaultRouteFollowsCore checks the shared window/menu-bar view against
// the core's default rule, independently of profile order and page selection.
func TestDefaultRouteFollowsCore(t *testing.T) {
	h := newHarness(t, t.TempDir())
	counter := &ruleCounter{RoundTripper: http.DefaultTransport.(*http.Transport).Clone()}
	h.client = mihomo.NewClient(testController, testSecret, mihomo.ConfigPath(h.cfg.HomeDir), &http.Client{Transport: counter}, 5*time.Second)
	// A lost reply waits out the re-read interval and the last scenario loads
	// 60k rules, so the budget is wider than the slowest run needs.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
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
	selectNode := func(t *testing.T, group, node string) {
		t.Helper()
		if err := proxies.Select(ctx, group, node); err != nil {
			t.Fatalf("select %q in %q: %v", node, group, err)
		}
	}
	assertRoute := func(t *testing.T, want ...string) model.ProxyView {
		t.Helper()
		view, err := proxies.View(ctx)
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
		selectNode(t, "Proxy", "Japan · konan")
		selectNode(t, "Media", "Japan · konan")
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
			if err := h.client.SetMode(ctx, mode); err != nil {
				t.Fatalf("set mode %q: %v", mode, err)
			}
			if mode == model.ModeGlobal {
				assertRoute(t, "GLOBAL", "Japan · konan")
			} else {
				assertRoute(t, "DIRECT")
			}
		}
	})
	t.Run("views node choices and delay tests reuse the default rule", func(t *testing.T) {
		if err := h.client.ApplyConfig(ctx, build(t, routeProfile)); err != nil {
			t.Fatal(err)
		}
		if err := h.client.SetMode(ctx, model.ModeRule); err != nil {
			t.Fatal(err)
		}
		before := counter.reads.Load()
		selectNode(t, "Proxy", "Japan · konan")
		assertRoute(t, "CatchAll", "Proxy", "Japan · konan")
		selectNode(t, "Proxy", "HK · PCCW")
		for range 3 {
			assertRoute(t, "CatchAll", "Proxy", "HK · PCCW")
		}
		if err := proxies.TestDelay(ctx, "Proxy", time.Millisecond); err != nil {
			t.Fatalf("test node delays: %v", err)
		}
		assertRoute(t, "CatchAll", "Proxy", "HK · PCCW")
		if got := counter.reads.Load() - before; got != 1 {
			t.Errorf("/rules requests across views, choices and delay tests = %d, want 1", got)
		}
	})
	t.Run("concurrent first reads share one rules request", func(t *testing.T) {
		if err := h.client.ApplyConfig(ctx, build(t, routeProfile)); err != nil {
			t.Fatal(err)
		}
		before := counter.reads.Load()
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				target, err := h.client.CatchAll(ctx)
				if err != nil || target != "CatchAll" {
					t.Errorf("concurrent default target = %q (error %v), want CatchAll", target, err)
				}
			})
		}
		wg.Wait()
		if got := counter.reads.Load() - before; got != 1 {
			t.Errorf("concurrent /rules requests = %d, want 1", got)
		}
	})
	t.Run("a restarted core refreshes the default rule", func(t *testing.T) {
		assertRoute(t, "CatchAll", "Proxy", "HK · PCCW")
		before := counter.reads.Load()
		if err := runner.RestartCurrent(ctx); err != nil {
			t.Fatal(err)
		}
		selectNode(t, "Proxy", "HK · PCCW")
		assertRoute(t, "CatchAll", "Proxy", "HK · PCCW")
		assertRoute(t, "CatchAll", "Proxy", "HK · PCCW")
		if got := counter.reads.Load() - before; got != 1 {
			t.Errorf("/rules requests after restart = %d, want 1", got)
		}
	})
	t.Run("failed rule reads can be retried", func(t *testing.T) {
		if err := h.client.ApplyConfig(ctx, build(t, routeProfile)); err != nil {
			t.Fatal(err)
		}
		before := counter.reads.Load()
		counter.failNext.Store(true)
		if _, err := h.client.CatchAll(ctx); err == nil {
			t.Fatal("a failed rules request returned a cached target")
		}
		for range 2 {
			if target, err := h.client.CatchAll(ctx); err != nil || target != "CatchAll" {
				t.Fatalf("retried default target = %q (error %v), want CatchAll", target, err)
			}
		}
		if got := counter.reads.Load() - before; got != 2 {
			t.Errorf("/rules requests for failure, retry and cached read = %d, want 2", got)
		}
	})
	t.Run("a pre-reload response cannot fill the new cache", func(t *testing.T) {
		if err := h.client.ApplyConfig(ctx, build(t, routeProfile)); err != nil {
			t.Fatal(err)
		}
		readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		counter.blocked, counter.release = make(chan struct{}), make(chan struct{})
		counter.blockNext.Store(true)
		before := counter.reads.Load()
		result := make(chan error, 1)
		go func() {
			target, err := h.client.CatchAll(readCtx)
			if err == nil && target != "DIRECT" {
				err = fmt.Errorf("old in-flight target = %q, want the reloaded DIRECT target", target)
			}
			result <- err
		}()
		select {
		case <-counter.blocked:
		case <-readCtx.Done():
			t.Fatal("rules request never reached the response gate")
		}
		reloaded := strings.ReplaceAll(routeProfile, "MATCH,CatchAll", "MATCH,DIRECT")
		err := h.client.ApplyConfig(ctx, build(t, reloaded))
		close(counter.release)
		if err != nil {
			t.Fatal(err)
		}
		if err := <-result; err != nil {
			t.Fatal(err)
		}
		if target, err := h.client.CatchAll(ctx); err != nil || target != "DIRECT" {
			t.Fatalf("new cached target = %q (error %v), want DIRECT", target, err)
		}
		if got := counter.reads.Load() - before; got != 2 {
			t.Errorf("/rules requests across the in-flight reload = %d, want 2", got)
		}
	})
	t.Run("a lost reload reply cannot freeze the previous target", func(t *testing.T) {
		if err := h.client.ApplyConfig(ctx, build(t, routeProfile)); err != nil {
			t.Fatal(err)
		}
		counter.configPath = mihomo.ConfigPath(h.cfg.HomeDir)
		counter.applyRelease, counter.applyFinished = make(chan struct{}), make(chan error, 1)
		counter.lateApply.Store(true)
		reloaded := build(t, strings.ReplaceAll(routeProfile, "MATCH,CatchAll", "MATCH,DIRECT"))
		if err := h.client.ApplyConfig(ctx, reloaded); err == nil {
			t.Fatal("the simulated lost reload reply returned no error")
		}
		before := counter.reads.Load()
		target, readErr := h.client.CatchAll(ctx)
		// Proxies remain readable while the parsed config is waiting to apply;
		// a successful snapshot is not a reload-completion barrier.
		_, snapshotErr := h.client.Snapshot(ctx)
		for range 3 {
			if _, err := h.client.CatchAll(ctx); err != nil {
				t.Fatal(err)
			}
		}
		if got := counter.reads.Load() - before; got != 1 {
			t.Errorf("/rules requests immediately after a lost reply = %d, want 1", got)
		}
		// A different rejected reload cannot prove that the earlier lost one
		// has finished. It must not restore permanent caching of the old target.
		rejectErr := h.client.ApplyConfig(ctx, []byte("proxy-groups: ["))
		var apiErr *mihomo.APIError
		if !errors.As(rejectErr, &apiErr) || apiErr.Status != http.StatusBadRequest {
			close(counter.applyRelease)
			t.Fatalf("invalid reload error = %v, want HTTP 400", rejectErr)
		}
		target, readErr = h.client.CatchAll(ctx)
		close(counter.applyRelease)
		if snapshotErr != nil {
			t.Fatalf("snapshot while the reload is pending: %v", snapshotErr)
		}
		if readErr != nil || target != "CatchAll" {
			t.Fatalf("target while reload is pending = %q (error %v), want CatchAll", target, readErr)
		}
		if err := <-counter.applyFinished; err != nil {
			t.Fatal(err)
		}
		// Browsing alone must eventually see the delayed target, without an
		// additional apply or restart, while repeated refreshes reuse the cache.
		// The wait has to outlast the interval the doubt caches for.
		deadline := time.Now().Add(25 * time.Second)
		for {
			target, err := h.client.CatchAll(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if target == "DIRECT" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("target did not recover after delayed reload: %q, want DIRECT", target)
			}
			time.Sleep(50 * time.Millisecond)
		}
		if err := h.client.ApplyConfig(ctx, reloaded); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if target, err := h.client.CatchAll(ctx); err != nil || target != "DIRECT" {
				t.Fatalf("target after acknowledged reload = %q (error %v), want DIRECT", target, err)
			}
		}
		if got := counter.reads.Load() - before; got != 4 {
			t.Errorf("/rules requests across the lost, rejected and acknowledged reloads = %d, want 4", got)
		}
		// A new process cannot finish a request from the old one. Only then
		// is indefinite caching safe again, including past the short expiry.
		if err := runner.RestartCurrent(ctx); err != nil {
			t.Fatal(err)
		}
		before = counter.reads.Load()
		if target, err := h.client.CatchAll(ctx); err != nil || target != "DIRECT" {
			t.Fatalf("target after core replacement = %q (error %v), want DIRECT", target, err)
		}
		// A replacement ends the doubt, so the read is kept rather than read
		// again after the interval. Whether it stays kept past that interval is
		// TestCatchAllDoubtIsBounded's check: waiting it out here would add the
		// whole interval to this suite.
		if target, err := h.client.CatchAll(ctx); err != nil || target != "DIRECT" {
			t.Fatalf("cached target after core replacement = %q (error %v), want DIRECT", target, err)
		}
		if got := counter.reads.Load() - before; got != 1 {
			t.Errorf("/rules requests after core replacement = %d, want 1", got)
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
			before := counter.reads.Load()
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
			assertRoute(t, scenario.want...)
			if got := counter.reads.Load() - before; got != 1 {
				t.Errorf("/rules requests after config reload = %d, want 1", got)
			}
		})
	}
}
