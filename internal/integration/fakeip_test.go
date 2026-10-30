package integration

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"hop/internal/model"
	"hop/internal/repository/mihomo"
	"hop/internal/repository/profile"
	"hop/internal/service"
)

// TestFakeIPExceptionsReachTunResolver 验证三种配置的例外合并及内核校验 / Verify exception merging and core validation for all three profile builders.
// 普通解析不带过滤器，TUN 保留内置条目且去重 / Plain DNS omits the filter; TUN retains built-in entries without duplicates.
func TestFakeIPExceptionsReachTunResolver(t *testing.T) {
	binary := os.Getenv("HOP_MIHOMO_BINARY")
	if binary == "" {
		t.Skip("HOP_MIHOMO_BINARY is not set")
	}
	validateDir := t.TempDir()
	if err := mihomo.SeedGeodata(filepath.Join("..", "..", "resources", "geodata"), validateDir); err != nil {
		t.Fatal(err)
	}
	validator := mihomo.NewConfigValidator(binary, validateDir, 30*time.Second)
	// 重复的内置域名只能保留一次 / Keep a repeated built-in domain only once.
	exceptions := []string{"+.example.com", "*.lan", "host.example.net"}
	builders := []struct {
		name  string
		build func(service.RuntimeSettings) ([]byte, error)
	}{
		{name: "bootstrap", build: service.BootstrapConfig},
		{name: "provider", build: func(settings service.RuntimeSettings) ([]byte, error) {
			return service.RuntimeConfig([]byte(profileA), model.RulesProvider, nil, settings)
		}},
		{name: "hop", build: func(settings service.RuntimeSettings) ([]byte, error) {
			return service.RuntimeConfig([]byte(profileA), model.RulesHop, nil, settings)
		}},
	}
	for _, builder := range builders {
		t.Run(builder.name, func(t *testing.T) {
			for _, mode := range []struct {
				name string
				tun  bool
			}{{name: "plain"}, {name: "tun", tun: true}} {
				t.Run(mode.name, func(t *testing.T) {
					config, err := builder.build(service.RuntimeSettings{
						MixedPort: testMixedPort, ControllerAddr: testController, ControllerSecret: testSecret,
						BindAddress: "127.0.0.1", LogLevel: "warning", BootstrapGroupName: "Hop",
						Mode: model.ModeRule, Tun: mode.tun, FakeIPExceptions: exceptions,
					})
					if err != nil {
						t.Fatal(err)
					}
					var got struct {
						DNS struct {
							FakeIPFilter []string `yaml:"fake-ip-filter"`
						} `yaml:"dns"`
					}
					if err := yaml.Unmarshal(config, &got); err != nil {
						t.Fatal(err)
					}
					filter := got.DNS.FakeIPFilter
					switch {
					case !mode.tun && len(filter) != 0:
						t.Errorf("the plain resolver has a fake-ip filter %v", filter)
					case mode.tun && (!slices.Contains(filter, "+.example.com") || !slices.Contains(filter, "host.example.net")):
						t.Errorf("the exceptions are missing from the fake-ip filter %v", filter)
					case mode.tun && !slices.Contains(filter, "captive.apple.com"):
						t.Errorf("the built-in names are missing from the fake-ip filter %v", filter)
					case mode.tun && countOf(filter, "*.lan") != 1:
						t.Errorf("a built-in name is written twice in %v", filter)
					}
					if err := validator.Validate(t.Context(), config); err != nil {
						t.Errorf("the core rejected the config: %v", err)
					}
				})
			}
		})
	}
}

// TestNormalizeFakeIPExceptions checks what the list takes in: the three
// forms, trimmed, lower case and each once; and what it refuses before the
// core is asked.
func TestNormalizeFakeIPExceptions(t *testing.T) {
	got, err := service.NormalizeFakeIPExceptions([]string{" Example.COM ", "*.example.com", "+.example.com", "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"example.com", "*.example.com", "+.example.com"}; !slices.Equal(got, want) {
		t.Errorf("normalized %v, want %v", got, want)
	}
	for _, bad := range []string{"", "localhost", "*.com.", "a b.com", "*.*.example.com", "*.+.example.com", "https://example.com", "exa_mple.com"} {
		if _, err := service.NormalizeFakeIPExceptions([]string{bad}); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

// countOf is how many times name appears in names.
func countOf(names []string, name string) int {
	count := 0
	for _, n := range names {
		if n == name {
			count++
		}
	}
	return count
}

// TestFakeIPExceptionsSurviveARejectedChange checks that a list is kept on
// disk across a restart, and that a change the core does not take leaves the
// list the user had, in the store and in the state the configs are built
// from. The core is never started: under TUN the change is applied to it, and
// a test never turns TUN on, so the change fails as it does when the core is
// down.
func TestFakeIPExceptionsSurviveARejectedChange(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, dir)
	exceptions := service.NewFakeIPExceptionsService(h.fakeIP, h.store, h.service, h.tun)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// With TUN off nothing is applied, so the change is kept.
	if err := exceptions.Save(ctx, []string{"a.example.com"}); err != nil {
		t.Fatal(err)
	}
	// The change would carry TUN to a core that answers, so none may.
	if conn, err := net.DialTimeout("tcp", testController, time.Second); err == nil {
		conn.Close()
		t.Fatalf("something answers on %s; a TUN config must not reach it", testController)
	}
	h.tun.Set(true)
	if err := exceptions.Save(ctx, []string{"b.example.com"}); err == nil {
		t.Fatal("a change the core did not take was kept")
	}
	if got := exceptions.List(); !slices.Equal(got, []string{"a.example.com"}) {
		t.Errorf("after the failed change the list is %v, want the previous one", got)
	}

	// A new start reads what the store holds.
	reopened, err := profile.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	next := service.NewFakeIPExceptionsState()
	if err := service.StartupFakeIPExceptions(reopened, next); err != nil {
		t.Fatal(err)
	}
	if got := next.Get(); !slices.Equal(got, []string{"a.example.com"}) {
		t.Errorf("the next start reads %v, want the previous list", got)
	}
}
