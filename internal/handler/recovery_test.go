package handler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo"

	"hop/internal/model"
	"hop/internal/service"
)

type syncUpdater struct {
	mu sync.Mutex
}

func (u *syncUpdater) Update(fn func()) {
	u.mu.Lock()
	defer u.mu.Unlock()
	fn()
}

type fakeRecoveryProxy struct {
	mu      sync.Mutex
	flushes int
	probes  int
	done    chan struct{}
}

func (f *fakeRecoveryProxy) View(context.Context, int) (model.ProxyView, error) {
	return model.ProxyView{Mode: model.ModeRule}, nil
}
func (f *fakeRecoveryProxy) Select(context.Context, string, string) error { return nil }
func (f *fakeRecoveryProxy) Reset(context.Context, string) error          { return nil }
func (f *fakeRecoveryProxy) FlushDNSCache(context.Context) error {
	f.mu.Lock()
	f.flushes++
	f.mu.Unlock()
	return nil
}
func (f *fakeRecoveryProxy) TestDelay(context.Context, string, string, time.Duration) error {
	return nil
}
func (f *fakeRecoveryProxy) TestNode(context.Context, string, string, time.Duration) (int, error) {
	return 0, nil
}
func (f *fakeRecoveryProxy) TestAutoGroups(context.Context, string, time.Duration) error {
	f.mu.Lock()
	f.probes++
	done := f.done
	f.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		default:
			close(done)
		}
	}
	return nil
}
func (f *fakeRecoveryProxy) WatchTraffic(context.Context, func(model.Traffic)) error { return nil }

func TestNetworkRecoveryDebounceAndOfflineCancel(t *testing.T) {
	proxy := &fakeRecoveryProxy{done: make(chan struct{})}
	updater := &syncUpdater{}
	d := &Dashboard{
		services: Services{Proxies: proxy},
		status:   statusConnected,
		settings: newSettingsState(service.DefaultSettings()),
		opCtx:    t.Context(),
		opWin:    updater,
	}
	d.recovery.delay = 25 * time.Millisecond
	d.unlock.services.lastRun = time.Now()

	// Offline right after wake cancels the pending wake recovery.
	updater.Update(func() {
		d.HandlePowerResume()
		d.HandleNetworkChange(mygo.NetworkStatus{Online: false})
	})
	time.Sleep(50 * time.Millisecond)

	proxy.mu.Lock()
	if proxy.flushes != 0 || proxy.probes != 0 {
		t.Fatalf("ran recovery while offline: flushes=%d probes=%d", proxy.flushes, proxy.probes)
	}
	proxy.mu.Unlock()

	// Burst of online/resume events coalesces into a single recovery run.
	updater.Update(func() {
		d.HandleNetworkChange(mygo.NetworkStatus{Online: true})
		d.HandlePowerResume()
	})
	select {
	case <-proxy.done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for recovery probe")
	}
	// Let the background goroutine finish cleanly before checking counts.
	time.Sleep(30 * time.Millisecond)

	updater.mu.Lock()
	defer updater.mu.Unlock()
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	if proxy.flushes != 1 || proxy.probes != 1 {
		t.Fatalf("got flushes=%d probes=%d, want 1 and 1", proxy.flushes, proxy.probes)
	}
	if !d.unlock.services.lastRun.IsZero() {
		t.Fatal("expected background service check timestamp to be invalidated for next open")
	}
	if d.coreEpoch != 1 {
		t.Fatalf("coreEpoch = %d, want 1", d.coreEpoch)
	}
}
