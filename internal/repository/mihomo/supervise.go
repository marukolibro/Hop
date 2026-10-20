package mihomo

import (
	"fmt"
	"log/slog"
	"time"

	"hop/internal/model"
)

// Supervision of a core that left on its own: the first restart waits
// restartDelayBase, each further one twice as long up to restartDelayCap. A
// run that holds stableUptime or longer is stable, so the crash that ends it
// begins a new series; after maxCrashes crashes in a row, with no stable run
// between them and every restart failing, the supervisor gives up.
const (
	restartDelayBase = 1 * time.Second
	restartDelayCap  = 30 * time.Second
	stableUptime     = 30 * time.Second
	maxCrashes       = 5
)

// exitEvent is one core's exit, reported by the goroutine that waited it.
type exitEvent struct {
	proc *Process
	err  error
}

// restartDelay is how long to wait before the crash'th restart in a row:
// 1s, 2s, 4s, ... capped at restartDelayCap.
func restartDelay(crashes int) time.Duration {
	delay := restartDelayBase << (crashes - 1)
	if delay <= 0 || delay > restartDelayCap {
		// The shift overflows only long after the cap is reached; either
		// way the wait is the cap.
		return restartDelayCap
	}
	return delay
}

// supervise restarts a core that left on its own. A core that Stop or
// Restart ended is not the runner's current one when its exit arrives, so it
// never triggers this. Goroutine ownership: started by adopt, it belongs to
// the runner and ends when the parent context is cancelled.
func (r *Runner) supervise() {
	for {
		select {
		case <-r.parent.Done():
			return
		case ev := <-r.exits:
			r.mu.Lock()
			current := r.proc == ev.proc
			r.mu.Unlock()
			if !current {
				// A stop Hop asked for itself, or a newer core has
				// already taken over.
				continue
			}
			if r.parent.Err() != nil {
				// Hop is quitting: the exit is the shutdown the
				// cancelled context asked for, before Stop collected it.
				return
			}
			r.restartAfterCrash(ev)
		}
	}
}

// reportExit waits the end of proc and hands the exit to the supervisor.
// Goroutine ownership: started by adopt and launchAndWait, it ends when the
// process has exited and the exit was delivered, or the runner ended.
func (r *Runner) reportExit(proc *Process) {
	err := proc.Wait()
	select {
	case r.exits <- exitEvent{proc: proc, err: err}:
	case <-r.parent.Done():
	}
}

// restartAfterCrash brings the core back after it left on its own, waiting
// longer after each crash in a row, and reports the outcome through the
// restart listener: a core that came back, or giving up. Until one of those,
// the runner holds no core.
func (r *Runner) restartAfterCrash(ev exitEvent) {
	r.mu.Lock()
	uptime := time.Since(ev.proc.startedAt)
	if uptime >= stableUptime {
		// The run was stable: its end begins a new series.
		r.crashes = 0
	}
	r.crashes++
	crashes := r.crashes
	r.proc, r.cancel = nil, nil
	binary := r.binary
	r.mu.Unlock()

	exit := ev.err
	slog.Warn("mihomo left on its own",
		"pid", ev.proc.cmd.Process.Pid, "binary", binary,
		"uptime_ms", uptime.Milliseconds(), "crashes_in_a_row", crashes,
		"error", ev.err)
	for {
		if crashes >= maxCrashes {
			givenUp := fmt.Errorf("mihomo left on its own %d times in a row and would not come back: %w", crashes, exit)
			slog.Error("giving up on restarting mihomo", "crashes_in_a_row", crashes, "error", exit)
			r.notify(model.CoreRestart{InARow: crashes, Exit: givenUp.Error(), GaveUp: true})
			return
		}
		select {
		case <-r.parent.Done():
			return
		case <-time.After(restartDelay(crashes)):
		}
		r.mu.Lock()
		if r.proc != nil || r.parent.Err() != nil {
			// Someone started a core meanwhile; it is theirs to mind now.
			r.mu.Unlock()
			return
		}
		err := r.launchAndWait(r.parent, binary)
		r.mu.Unlock()
		if err == nil {
			slog.Info("mihomo came back after leaving on its own",
				"binary", binary, "crashes_in_a_row", crashes)
			r.notify(model.CoreRestart{InARow: crashes, Exit: exit.Error()})
			return
		}
		// The core would not come back; that counts as the next crash.
		crashes++
		r.mu.Lock()
		r.crashes = crashes
		r.mu.Unlock()
		slog.Warn("restarting mihomo failed",
			"binary", binary, "crashes_in_a_row", crashes, "error", err)
		exit = err
	}
}

// notify hands a supervised restart's outcome to the listener, when one was
// set. It runs on the supervisor's goroutine.
func (r *Runner) notify(ev model.CoreRestart) {
	r.mu.Lock()
	fn := r.onRestart
	r.mu.Unlock()
	if fn != nil {
		fn(ev)
	}
}
