package integration

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"

	"hop/internal/repository/mihomo"
	"hop/internal/service"
)

// testMovedPort is where the proxy port is moved to; it is free of every
// other listener the tests use.
const testMovedPort = 17930

// followRecorder stands in for the system proxy, which a test never touches:
// it records the ports the service asked it to follow from.
type followRecorder struct{ from []int }

func (f *followRecorder) Follow(_ context.Context, previous int) error {
	f.from = append(f.from, previous)
	return nil
}

// unreachableProbe reports nothing listening on one port, as a core that
// accepted the config but could not bind it would leave it. The core really
// did bind it, so the test sees the service close it again.
type unreachableProbe struct {
	mihomo.PortProbe
	port int
}

func (p unreachableProbe) Listening(ctx context.Context, port int) error {
	if port == p.port {
		return errors.New("nothing listens here")
	}
	return p.PortProbe.Listening(ctx, port)
}

// TestMixedPortChange moves the proxy port under a real core: the core
// listens on the new port and not the old, a later config built by the
// service keeps the new port, the choice is kept for the next start, and a
// port something else holds is refused with everything left as it was.
func TestMixedPortChange(t *testing.T) {
	h := newHarness(t, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	startup, err := h.service.StartupConfig()
	if err != nil {
		t.Fatalf("startup config: %v", err)
	}
	runnerCtx, stop := context.WithCancel(ctx)
	runner := mihomo.NewRunner(runnerCtx, h.cfg, h.client)
	if err := runner.Start(startup); err != nil {
		t.Fatalf("start core: %v", err)
	}
	defer func() {
		stop()
		if err := runner.Stop(); err != nil {
			t.Errorf("stop core: %v", err)
		}
	}()

	proxy := &followRecorder{}
	ports := service.NewMixedPortService(h.port, h.store, mihomo.PortProbe{}, h.service, proxy, runner, nil)
	probe := mihomo.PortProbe{}

	if err := probe.Listening(ctx, testMixedPort); err != nil {
		t.Fatalf("the core does not listen on the starting port: %v", err)
	}
	if err := ports.Change(ctx, testMovedPort); err != nil {
		t.Fatalf("change the proxy port: %v", err)
	}
	if got := ports.Current(); got != testMovedPort {
		t.Fatalf("current port = %d, want %d", got, testMovedPort)
	}
	if err := probe.Listening(ctx, testMovedPort); err != nil {
		t.Errorf("the core does not listen on the new port: %v", err)
	}
	if err := probe.Listening(ctx, testMixedPort); err == nil {
		t.Errorf("the core still listens on the old port %d", testMixedPort)
	}
	if len(proxy.from) != 1 || proxy.from[0] != testMixedPort {
		t.Errorf("the system proxy followed from %v, want [%d]", proxy.from, testMixedPort)
	}
	saved, found, err := h.store.LoadMixedPort()
	if err != nil || !found || saved != testMovedPort {
		t.Errorf("saved port = %d (found=%v err=%v), want %d", saved, found, err, testMovedPort)
	}

	// A config built afterwards, such as a refresh's or a TUN switch's, must
	// not put the old port back.
	if err := h.service.ApplyCurrent(ctx); err != nil {
		t.Fatalf("apply the current config: %v", err)
	}
	if err := probe.Listening(ctx, testMovedPort); err != nil {
		t.Errorf("a later config moved the core off the new port: %v", err)
	}
	if err := probe.Listening(ctx, testMixedPort); err == nil {
		t.Errorf("a later config put the old port %d back", testMixedPort)
	}

	// A port another program holds is refused before anything moves.
	taken, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(testMovedPort+1)))
	if err != nil {
		t.Fatalf("hold a port: %v", err)
	}
	defer taken.Close()
	if err := ports.Change(ctx, testMovedPort+1); err == nil {
		t.Errorf("a port another program holds was accepted")
	}
	if got := ports.Current(); got != testMovedPort {
		t.Errorf("current port = %d after a refused change, want %d", got, testMovedPort)
	}
	if err := probe.Listening(ctx, testMovedPort); err != nil {
		t.Errorf("a refused change moved the core off its port: %v", err)
	}

	// The core accepts a config whose port it cannot bind, and drops the old
	// listener on the way: when the new port does not answer, the service must
	// put the old one back.
	moved := testMovedPort + 2
	unreachable := service.NewMixedPortService(h.port, h.store, unreachableProbe{port: moved}, h.service, proxy, runner, nil)
	if err := unreachable.Change(ctx, moved); err == nil {
		t.Errorf("a port that did not answer was accepted")
	}
	if got := ports.Current(); got != testMovedPort {
		t.Errorf("current port = %d after a failed change, want %d", got, testMovedPort)
	}
	if err := probe.Listening(ctx, testMovedPort); err != nil {
		t.Errorf("the failed change did not put the port back: %v", err)
	}
	if err := probe.Listening(ctx, moved); err == nil {
		t.Errorf("the core still listens on %d after the failed change", moved)
	}
	if saved, _, err := h.store.LoadMixedPort(); err != nil || saved != testMovedPort {
		t.Errorf("saved port = %d (err=%v) after a failed change, want %d", saved, err, testMovedPort)
	}

	// Ports outside the range, or Hop's own, are refused too.
	for _, port := range []int{80, 70000, 19091} {
		err := service.NewMixedPortService(h.port, h.store, probe, h.service, proxy, runner, []int{19091}).Change(ctx, port)
		if err == nil {
			t.Errorf("port %d was accepted", port)
		}
	}
}
