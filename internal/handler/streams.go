package handler

import (
	"context"
	"log/slog"
	"time"
)

const (
	// streamRetryBase is the wait before opening a stream that ended again,
	// twice as long after each further failure in a row up to streamRetryCap,
	// as the runner restarts a core that quit.
	streamRetryBase = 1 * time.Second
	streamRetryCap  = 30 * time.Second
)

// streamRetryDelay is the wait before the failures'th attempt in a row to open
// a stream again: 1s, 2s, 4s, ... capped at streamRetryCap.
func streamRetryDelay(failures int) time.Duration {
	delay := streamRetryBase << (failures - 1)
	if delay <= 0 || delay > streamRetryCap {
		return streamRetryCap
	}
	return delay
}

// streamFunc opens one stream from the core and reads it until it ends;
// delivered is called, on the stream's own goroutine, for every message.
type streamFunc func(ctx context.Context, delivered func()) error

// keepStreaming reads a stream from the core until ctx ends. A stream can end
// while the core runs on (one read that fails, a connection table past the
// read limit), and nothing else would open it again: the stats would stop
// counting and the lists stand still for the rest of the run. So it opens the
// stream again after wait(failures), failures counting the ends in a row
// since one delivered something, and reports only the first of them, not each
// attempt. Goroutine ownership: the caller's goroutine runs it; it returns
// when ctx is cancelled.
func keepStreaming(ctx context.Context, a action, stream streamFunc, wait func(failures int) time.Duration, report func(err error)) {
	failures := 0
	for {
		delivered := false
		err := stream(ctx, func() { delivered = true })
		if ctx.Err() != nil {
			return
		}
		if delivered {
			failures = 0
		}
		failures++
		if failures == 1 {
			report(err)
		}
		delay := wait(failures)
		slog.Warn("core stream ended, opening it again", "stream", a.id, "failures", failures, "wait", delay, "error", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}
