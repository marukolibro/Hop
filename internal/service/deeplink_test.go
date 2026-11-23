package service

import (
	"strings"
	"testing"
)

func TestParseInstallConfigURL(t *testing.T) {
	t.Run("standard clash install-config", func(t *testing.T) {
		raw := "clash://install-config?url=https%3A%2F%2Fexample.com%2Fsub%3Ftoken%3Dsecret123&name=My+Airport"
		got, ok, err := ParseInstallConfigURL(raw)
		if err != nil || !ok {
			t.Fatalf("got ok=%v err=%v, want ok=true err=nil", ok, err)
		}
		if got.Name != "My Airport" || got.URL != "https://example.com/sub?token=secret123" {
			t.Fatalf("got = %+v", got)
		}
	})

	t.Run("mihomo scheme with double encoded url", func(t *testing.T) {
		raw := "mihomo://install-config/?url=https%253A%252F%252Fsub.example.org%252Flink"
		got, ok, err := ParseInstallConfigURL(raw)
		if err != nil || !ok {
			t.Fatalf("got ok=%v err=%v", ok, err)
		}
		if got.URL != "https://sub.example.org/link" || got.Name != "" {
			t.Fatalf("got = %+v", got)
		}
	})

	t.Run("rejects non-http target without leaking secret", func(t *testing.T) {
		raw := "clash://install-config?url=file%3A%2F%2F%2Fetc%2Fpasswd%3Fsecret%3Dxyz"
		_, ok, err := ParseInstallConfigURL(raw)
		if !ok || err == nil {
			t.Fatalf("expected error for file:// target, got ok=%v err=%v", ok, err)
		}
		if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "passwd") {
			t.Fatalf("error leaked URL contents: %v", err)
		}
	})

	t.Run("ignores unrelated urls", func(t *testing.T) {
		_, ok, err := ParseInstallConfigURL("https://example.com/")
		if ok || err != nil {
			t.Fatalf("expected ok=false err=nil, got ok=%v err=%v", ok, err)
		}
	})
}
