package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"hop/internal/service"
)

func TestAppUpdateTLSHandshakeTimeout(t *testing.T) {
	connections := make(chan net.Conn, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			t.Errorf("proxy received %s, want CONNECT", r.Method)
			http.Error(w, "CONNECT required", http.StatusMethodNotAllowed)
			return
		}
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack proxy connection: %v", err)
			return
		}
		connections <- conn
		if _, err := rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			t.Errorf("write proxy response: %v", err)
			return
		}
		if err := rw.Flush(); err != nil {
			t.Errorf("flush proxy response: %v", err)
		}
	}))
	t.Cleanup(func() {
		proxy.Close()
		select {
		case conn := <-connections:
			if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				t.Errorf("close stalled tunnel: %v", err)
			}
		default:
		}
	})
	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(proxyURL.Port())
	if err != nil {
		t.Fatal(err)
	}
	source := appUpdateSource(service.NewMixedPortState(port))
	if source == nil {
		t.Skip("self-updates are not published on this platform")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	body, err := source.Open(ctx, "https://updates.invalid/archive.tar.gz")
	if body != nil {
		if err := body.Close(); err != nil {
			t.Fatal(err)
		}
		t.Fatal("download succeeded without a TLS reply")
	}
	if ctx.Err() != nil {
		t.Fatalf("download waited for the caller's deadline instead of timing out its TLS handshake: %v", err)
	}
	timeout, ok := errors.AsType[net.Error](err)
	if !ok || !timeout.Timeout() {
		t.Fatalf("download error = %v, want a TLS handshake timeout", err)
	}
}
