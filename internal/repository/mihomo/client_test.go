package mihomo

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// rulesTargetClient serves a /rules reply whose Match rule names target, and
// counts the reads.
func rulesTargetClient(t *testing.T, target string) (*Client, *atomic.Int32) {
	t.Helper()
	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rules" {
			http.NotFound(w, r)
			return
		}
		reads.Add(1)
		_, _ = io.WriteString(w, `{"rules":[{"type":"DOMAIN","payload":"example.com","proxy":"DIRECT"},{"type":"Match","payload":"","proxy":"`+target+`"}]}`)
	}))
	t.Cleanup(server.Close)
	addr := strings.TrimPrefix(server.URL, "http://")
	return NewClient(addr, "secret", filepath.Join(t.TempDir(), "config.yaml"), &http.Client{}, time.Second), &reads
}

func TestUncertainExpired(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name      string
		uncertain bool
		until     time.Time
		want      bool
	}{
		{"settled", false, time.Time{}, false},
		{"inside the window", true, now.Add(time.Minute), false},
		{"a doubt with no window", true, time.Time{}, false},
		{"the window just ended", true, now, true},
		{"the window passed", true, now.Add(-time.Second), true},
	} {
		cache := ruleTargetCache{uncertain: tc.uncertain, uncertainUntil: tc.until}
		if got := cache.uncertainExpired(now); got != tc.want {
			t.Errorf("%s: uncertainExpired = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestCatchAllDoubtIsBounded drives the doubt a lost reload reply starts: the
// target stays cached for the re-read interval while the window lasts, and the
// read that finds the window over ends the doubt, so the target is kept until
// the next invalidation. The cache's own clocks are moved rather than slept
// through, so the test does not depend on machine load.
func TestCatchAllDoubtIsBounded(t *testing.T) {
	client, reads := rulesTargetClient(t, "Proxy")
	ctx := context.Background()
	read := func(t *testing.T, want int32) {
		t.Helper()
		target, err := client.CatchAll(ctx)
		if err != nil || target != "Proxy" {
			t.Fatalf("target = %q (error %v), want Proxy", target, err)
		}
		if got := reads.Load(); got != want {
			t.Fatalf("/rules reads = %d, want %d", got, want)
		}
	}
	// state reads what the cache holds of a doubt, under its lock.
	state := func(t *testing.T) (uncertain bool, until time.Time, expires time.Time) {
		t.Helper()
		client.rules.mu.Lock()
		defer client.rules.mu.Unlock()
		return client.rules.uncertain, client.rules.uncertainUntil, client.rules.expiresAt
	}
	// expire passes the re-read interval, as waiting would.
	expire := func() {
		client.rules.mu.Lock()
		defer client.rules.mu.Unlock()
		client.rules.expiresAt = time.Now().Add(-time.Second)
	}
	// settleWindow passes the window a doubt is bounded by.
	settleWindow := func() {
		client.rules.mu.Lock()
		defer client.rules.mu.Unlock()
		client.rules.uncertainUntil = time.Now().Add(-time.Second)
	}

	// A settled target is read once and kept.
	read(t, 1)
	read(t, 1)

	// A lost reply drops it: the read stays cached for the re-read interval.
	client.CoreChanged(false, false)
	read(t, 2)
	uncertain, until, expires := state(t)
	if !uncertain {
		t.Fatal("a lost reply left no doubt")
	}
	if left := time.Until(until); left < 89*time.Second || left > 91*time.Second {
		t.Fatalf("the doubt's window ends in %v, want about %v", left, uncertainRulesWindow)
	}
	if left := time.Until(expires); left < 14*time.Second || left > 16*time.Second {
		t.Fatalf("the target is cached for %v, want about %v", left, uncertainRulesTTL)
	}
	read(t, 2)

	// Past the interval, still inside the window: read again, the doubt stays.
	expire()
	read(t, 3)
	if uncertain, _, _ = state(t); !uncertain {
		t.Fatal("the doubt ended inside its window")
	}

	// Past both the interval and the window, the read is final and the doubt is
	// gone; a value still cached would be served without asking, so the doubt
	// only has to end when the cache is read again.
	expire()
	settleWindow()
	read(t, 4)
	uncertain, until, expires = state(t)
	if uncertain || !until.IsZero() || !expires.IsZero() {
		t.Fatalf("after the window: uncertain=%v, window until %v, cached until %v, want a settled target", uncertain, until, expires)
	}
	read(t, 4)

	// A confirmed reload that replaced no process leaves the doubt alone: an
	// earlier request may still finish after it.
	client.CoreChanged(false, false)
	client.CoreChanged(true, false)
	if uncertain, _, _ = state(t); !uncertain {
		t.Fatal("a confirmed reload cleared the doubt")
	}
	// Only a core replacement settles it before the window ends, and the read
	// that follows is kept until the next invalidation rather than for the
	// interval.
	client.CoreChanged(true, true)
	if uncertain, until, _ = state(t); uncertain || !until.IsZero() {
		t.Fatal("a core replacement left the doubt behind")
	}
	read(t, 5)
	if _, _, expires = state(t); !expires.IsZero() {
		t.Fatalf("a settled read is cached until %v, want no expiry", expires)
	}
	read(t, 5)
}
