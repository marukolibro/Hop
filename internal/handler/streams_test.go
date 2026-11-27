package handler

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestKeepStreamingReopensAndReportsOncePerRun opens a stream that fails twice
// without a message, then delivers and fails, then waits: it must be opened
// four times and reported twice, once per run of failures.
func TestKeepStreamingReopensAndReportsOncePerRun(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	opened := 0
	var reported []error
	var waits []int
	stream := func(ctx context.Context, delivered func()) error {
		opened++
		switch opened {
		case 1, 2:
			return errors.New("dropped before a message")
		case 3:
			delivered()
			return errors.New("dropped after a message")
		default:
			cancel()
			<-ctx.Done()
			return ctx.Err()
		}
	}
	wait := func(failures int) time.Duration {
		waits = append(waits, failures)
		return time.Millisecond
	}
	keepStreaming(ctx, actionWatchConnections, stream, wait, func(err error) { reported = append(reported, err) })
	if opened != 4 {
		t.Fatalf("opened %d times, want 4", opened)
	}
	if len(reported) != 2 {
		t.Fatalf("reported %d failures, want 2 (one per run): %v", len(reported), reported)
	}
	if want := []int{1, 2, 1}; len(waits) != len(want) || waits[0] != want[0] || waits[1] != want[1] || waits[2] != want[2] {
		t.Fatalf("waited with failures %v, want %v: a message must reset the count", waits, want)
	}
}

func TestStreamRetryDelayDoublesUpToTheCap(t *testing.T) {
	for _, c := range []struct {
		failures int
		want     time.Duration
	}{{1, time.Second}, {2, 2 * time.Second}, {5, 16 * time.Second}, {6, streamRetryCap}, {80, streamRetryCap}} {
		if got := streamRetryDelay(c.failures); got != c.want {
			t.Errorf("streamRetryDelay(%d) = %v, want %v", c.failures, got, c.want)
		}
	}
}
