package integration

import (
	"context"
	"errors"
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

// TestFakeIPExceptionsReachTunResolver checks that the user's names join the
// built-in fake-ip filter under TUN, once each, that the plain resolver has no
// filter at all, and that the real core accepts the result.
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
	// The second name is a built-in one, which must not be written twice.
	exceptions := []string{"+.example.com", "*.lan", "host.example.net"}
	for _, tun := range []bool{false, true} {
		config, err := service.RuntimeConfig([]byte(profileA), model.RulesProvider, nil, service.RuntimeSettings{
			MixedPort: testMixedPort, ControllerAddr: testController, ControllerSecret: testSecret,
			BindAddress: "127.0.0.1", LogLevel: "warning", BootstrapGroupName: "Hop",
			Mode: model.ModeRule, Tun: tun, FakeIPExceptions: exceptions,
		})
		if err != nil {
			t.Fatalf("tun=%v: %v", tun, err)
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
		case !tun && len(filter) != 0:
			t.Errorf("tun=false: the plain resolver has a fake-ip filter %v", filter)
		case tun && (!slices.Contains(filter, "+.example.com") || !slices.Contains(filter, "host.example.net")):
			t.Errorf("tun=true: the exceptions are missing from the fake-ip filter %v", filter)
		case tun && !slices.Contains(filter, "captive.apple.com"):
			t.Errorf("tun=true: the built-in names are missing from the fake-ip filter %v", filter)
		case tun && countOf(filter, "*.lan") != 1:
			t.Errorf("tun=true: a built-in name is written twice in %v", filter)
		}
		if err := validator.Validate(context.Background(), config); err != nil {
			t.Errorf("tun=%v: the core rejected the config: %v", tun, err)
		}
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
	for _, bad := range []string{"", "localhost", "*.com.", "a b.com", "*.*.example.com", "https://example.com", "exa_mple.com"} {
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

// failingReloader is a core that rejects every config it is handed.
type failingReloader struct{}

func (failingReloader) ReloadRuntime(context.Context) error { return errors.New("config rejected") }

// TestFakeIPExceptionsSurviveARejectedChange checks that a list is kept on
// disk across a restart, and that a config the core rejects leaves the list
// the user had, in the store and in the state the configs are built from.
func TestFakeIPExceptionsSurviveARejectedChange(t *testing.T) {
	dir := t.TempDir()
	store, err := profile.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	state := service.NewFakeIPExceptionsState()
	tun := service.NewTunFlag()
	exceptions := service.NewFakeIPExceptionsService(state, store, failingReloader{}, tun)

	// With TUN off nothing is reloaded, so the change is kept.
	if err := exceptions.Save(context.Background(), []string{"a.example.com"}); err != nil {
		t.Fatal(err)
	}
	tun.Set(true)
	if err := exceptions.Save(context.Background(), []string{"b.example.com"}); err == nil {
		t.Fatal("a config the core rejected was kept")
	}
	if got := exceptions.List(); !slices.Equal(got, []string{"a.example.com"}) {
		t.Errorf("after the rejected change the list is %v, want the previous one", got)
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
