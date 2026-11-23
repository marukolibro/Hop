package service

import (
	"context"
	"errors"
	"slices"
	"testing"

	"hop/internal/model"
)

// recordingProxy stands in for the system proxy so that the test never
// changes the real one; it records every change asked of it.
type recordingProxy struct {
	state      model.SystemProxyState
	sets       int
	clears     int
	lastBypass []string
	readErr    error
	setErr     error
}

func (p *recordingProxy) Current(context.Context) (model.SystemProxyState, error) {
	return p.state, p.readErr
}

func (p *recordingProxy) Set(_ context.Context, _ string, _ int, bypass []string) error {
	p.sets++
	p.lastBypass = slices.Clone(bypass)
	return p.setErr
}

func (p *recordingProxy) Clear(context.Context) error {
	p.clears++
	return nil
}

type memoryBypassStore struct {
	entries []string
}

func (m *memoryBypassStore) LoadProxyBypass() ([]string, error) {
	return slices.Clone(m.entries), nil
}

func (m *memoryBypassStore) SaveProxyBypass(entries []string) error {
	m.entries = slices.Clone(entries)
	return nil
}

func endpointAt(host string, port int) model.ProxyEndpoint {
	return model.ProxyEndpoint{Enabled: true, Host: host, Port: port}
}

// TestSystemProxyNeverTouchesAnotherApps guards the rule that Hop changes
// the system proxy only when no other app holds it, and clears only its own.
func TestSystemProxyNeverTouchesAnotherApps(t *testing.T) {
	verge := endpointAt("127.0.0.1", 7897)
	hop := endpointAt("127.0.0.1", 17890)
	cases := []struct {
		name               string
		state              model.SystemProxyState
		wantOwner          model.ProxyOwner
		wantEnableErr      error
		wantSets, wantClrs int
	}{
		{name: "none", state: model.SystemProxyState{}, wantOwner: model.ProxyOwnerNone, wantSets: 1},
		{name: "hop", state: model.SystemProxyState{HTTP: hop, HTTPS: hop, SOCKS: hop}, wantOwner: model.ProxyOwnerHop, wantSets: 1, wantClrs: 1},
		{name: "hop by localhost", state: model.SystemProxyState{HTTP: endpointAt("localhost", 17890)}, wantOwner: model.ProxyOwnerHop, wantSets: 1, wantClrs: 1},
		{name: "other app", state: model.SystemProxyState{HTTP: verge, HTTPS: verge, SOCKS: verge}, wantOwner: model.ProxyOwnerOther, wantEnableErr: ErrSystemProxyTaken},
		{name: "partly other", state: model.SystemProxyState{HTTP: hop, SOCKS: verge}, wantOwner: model.ProxyOwnerOther, wantEnableErr: ErrSystemProxyTaken},
		{name: "pac", state: model.SystemProxyState{AutoConfig: true}, wantOwner: model.ProxyOwnerOther, wantEnableErr: ErrSystemProxyTaken},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proxy := &recordingProxy{state: tc.state}
			s := NewSystemProxyService(proxy, nil, "127.0.0.1", NewMixedPortState(17890))
			status, err := s.Status(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if status.Owner != tc.wantOwner {
				t.Fatalf("owner = %s, want %s", status.Owner, tc.wantOwner)
			}
			if err := s.Enable(t.Context()); !errors.Is(err, tc.wantEnableErr) {
				t.Fatalf("Enable error = %v, want %v", err, tc.wantEnableErr)
			}
			if err := s.Disable(t.Context()); err != nil {
				t.Fatalf("Disable: %v", err)
			}
			if proxy.sets != tc.wantSets || proxy.clears != tc.wantClrs {
				t.Fatalf("sets=%d clears=%d, want sets=%d clears=%d", proxy.sets, proxy.clears, tc.wantSets, tc.wantClrs)
			}
			if tc.wantSets > 0 && !slices.Contains(proxy.lastBypass, "localhost") {
				t.Fatalf("expected default bypass entries to be passed, got %v", proxy.lastBypass)
			}
		})
	}

	t.Run("unreadable", func(t *testing.T) {
		proxy := &recordingProxy{readErr: errors.New("scutil failed")}
		s := NewSystemProxyService(proxy, nil, "127.0.0.1", NewMixedPortState(17890))
		if err := s.Enable(t.Context()); err == nil {
			t.Fatal("Enable succeeded without reading the system proxy")
		}
		if err := s.Disable(t.Context()); err == nil {
			t.Fatal("Disable succeeded without reading the system proxy")
		}
		if proxy.sets != 0 || proxy.clears != 0 {
			t.Fatalf("changed a system proxy it could not read: sets=%d clears=%d", proxy.sets, proxy.clears)
		}
	})
}

func TestSystemProxyBypass(t *testing.T) {
	hop := endpointAt("127.0.0.1", 17890)
	verge := endpointAt("127.0.0.1", 7897)

	t.Run("applies when hop owns proxy", func(t *testing.T) {
		proxy := &recordingProxy{state: model.SystemProxyState{HTTP: hop, HTTPS: hop, SOCKS: hop}}
		store := &memoryBypassStore{}
		s := NewSystemProxyService(proxy, store, "127.0.0.1", NewMixedPortState(17890))
		if err := s.SaveBypass(t.Context(), []string{"*.Corp.Internal", "10.20.0.5/16", "192.168.*", "localhost"}); err != nil {
			t.Fatalf("SaveBypass: %v", err)
		}
		if proxy.sets != 1 {
			t.Fatalf("sets = %d, want 1", proxy.sets)
		}
		if !slices.Contains(proxy.lastBypass, "*.corp.internal") || !slices.Contains(proxy.lastBypass, "10.20.0.0/16") {
			t.Fatalf("lastBypass = %v, missing normalized entries", proxy.lastBypass)
		}
		wantStored := []string{"*.corp.internal", "10.20.0.0/16", "192.168.*", "localhost"}
		if !slices.Equal(store.entries, wantStored) {
			t.Fatalf("stored = %v, want %v", store.entries, wantStored)
		}
	})

	t.Run("never touches another app proxy", func(t *testing.T) {
		proxy := &recordingProxy{state: model.SystemProxyState{HTTP: verge}}
		store := &memoryBypassStore{}
		s := NewSystemProxyService(proxy, store, "127.0.0.1", NewMixedPortState(17890))
		if err := s.SaveBypass(t.Context(), []string{"*.corp.internal"}); err != nil {
			t.Fatalf("SaveBypass: %v", err)
		}
		if proxy.sets != 0 {
			t.Fatalf("sets = %d, want 0 when another app owns proxy", proxy.sets)
		}
	})

	t.Run("rejects bad entry", func(t *testing.T) {
		proxy := &recordingProxy{}
		s := NewSystemProxyService(proxy, &memoryBypassStore{}, "127.0.0.1", NewMixedPortState(17890))
		if err := s.SaveBypass(t.Context(), []string{"bad host"}); err == nil {
			t.Fatal("expected error for entry with whitespace")
		}
	})
}
