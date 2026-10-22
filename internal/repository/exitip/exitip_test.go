package exitip

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
)

func TestParseTrace(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		address string
		country string
		wantErr bool
	}{
		{
			name:    "trace",
			body:    "fl=abc h=1.1.1.1 ip=203.0.113.42 ts=1 visit_scheme=https colo=SIN loc=SG tls=TLSv1.3",
			address: "203.0.113.42",
			country: "SG",
		},
		{
			name:    "lower case country",
			body:    "ip=203.0.113.42\nloc=sg\n",
			address: "203.0.113.42",
			country: "SG",
		},
		{
			name:    "mapped address",
			body:    "ip=::ffff:203.0.113.42\nloc=SG\n",
			address: "203.0.113.42",
			country: "SG",
		},
		{name: "no ip", body: "loc=SG\n", wantErr: true},
		{name: "bad ip", body: "ip=not-an-ip\nloc=SG\n", wantErr: true},
		{name: "no country", body: "ip=203.0.113.42\n", wantErr: true},
		{name: "bad country", body: "ip=203.0.113.42\nloc=SGP\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip, err := ParseTrace([]byte(tt.body))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				if strings.Contains(err.Error(), "203.0.113.42") {
					t.Fatalf("error carries the address: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got, want := ip.Address, netip.MustParseAddr(tt.address); got != want {
				t.Fatalf("address %v, want %v", got, want)
			}
			if ip.Country != tt.country {
				t.Fatalf("country %q, want %q", ip.Country, tt.country)
			}
		})
	}
}

func TestClientLookup(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/trace" {
				t.Errorf("path %q", r.URL.Path)
			}
			_, _ = w.Write([]byte("ip=203.0.113.42\nloc=SG\n"))
		}))
		defer server.Close()
		ip, err := NewClient(server.URL+"/trace", server.Client()).Lookup(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if ip.Address.String() != "203.0.113.42" || ip.Country != "SG" {
			t.Fatalf("got %v %q", ip.Address, ip.Country)
		}
	})

	t.Run("retries once with a warning without the address", func(t *testing.T) {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if calls.Add(1) == 1 {
				http.Error(w, "ip=203.0.113.42 temporary", http.StatusBadGateway)
				return
			}
			_, _ = w.Write([]byte("ip=203.0.113.42\nloc=SG\n"))
		}))
		defer server.Close()
		var log bytes.Buffer
		old := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
		defer slog.SetDefault(old)
		ip, err := NewClient(server.URL, server.Client()).Lookup(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if calls.Load() != 2 {
			t.Fatalf("attempts %d, want 2", calls.Load())
		}
		if ip.Address.String() != "203.0.113.42" {
			t.Fatalf("address %v", ip.Address)
		}
		if strings.Contains(log.String(), "203.0.113.42") {
			t.Fatalf("log carries the address: %s", log.String())
		}
		if !strings.Contains(log.String(), "retrying") {
			t.Fatalf("no retry warning: %s", log.String())
		}
	})

	t.Run("fails after two attempts", func(t *testing.T) {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			http.Error(w, "ip=203.0.113.42 unavailable", http.StatusServiceUnavailable)
		}))
		defer server.Close()
		_, err := NewClient(server.URL, server.Client()).Lookup(context.Background())
		if err == nil {
			t.Fatal("expected an error")
		}
		if calls.Load() != 2 {
			t.Fatalf("attempts %d, want 2", calls.Load())
		}
		if !strings.Contains(err.Error(), "503") {
			t.Fatalf("error lacks the status: %v", err)
		}
		if strings.Contains(err.Error(), "203.0.113.42") {
			t.Fatalf("error carries the address: %v", err)
		}
	})

	t.Run("bounds the reply", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(bytes.Repeat([]byte("a"), maxTrace+1))
		}))
		defer server.Close()
		_, err := NewClient(server.URL, server.Client()).Lookup(context.Background())
		if err == nil || !strings.Contains(err.Error(), "over") {
			t.Fatalf("got %v", err)
		}
	})
}
