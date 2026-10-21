package mihomo

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"hop/config"
	"hop/internal/model"
)

// Runner owns the core process of this Hop and can replace it with a
// different binary on the same config, which is how a core is upgraded.
// A core that leaves on its own is started again by the supervisor
// (supervise.go); the stops Hop asks for itself never trigger it.
type Runner struct {
	cfg    config.Config
	client *Client
	parent context.Context

	// mu serializes starts and stops: there is at most one core at a time.
	mu     sync.Mutex
	proc   *Process
	cancel context.CancelFunc
	binary string
	// crashes counts the cores that left on their own without a stable run
	// between them; supervising says the supervisor goroutine already runs.
	crashes     int
	supervising bool

	// exits carries each core's exit to the supervisor goroutine.
	exits chan exitEvent

	// onRestart, when set, hears every supervised restart's outcome. It is
	// set through OnRestart before Start.
	onRestart func(model.CoreRestart)
}

// NewRunner returns a runner for the core cfg describes. Every core it
// starts ends when parent is cancelled.
func NewRunner(parent context.Context, cfg config.Config, client *Client) *Runner {
	return &Runner{cfg: cfg, client: client, parent: parent, binary: cfg.MihomoBinary, exits: make(chan exitEvent, 4)}
}

// OnRestart makes fn hear what supervision did after a core left on its own:
// a restart that brought it back, or giving up. Set it before Start; fn runs
// on the supervisor's goroutine.
func (r *Runner) OnRestart(fn func(model.CoreRestart)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onRestart = fn
}

// Start writes configYAML and starts the core, returning once its
// controller answers.
func (r *Runner) Start(configYAML []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	procCtx, cancel := context.WithCancel(r.parent)
	proc, err := Start(procCtx, r.cfg, configYAML)
	if err != nil {
		cancel()
		return err
	}
	// A start Hop asked for itself begins a new crash series.
	r.adopt(proc, cancel)
	return r.waitReady(r.parent)
}

// Restart replaces the core with binary on the config the previous one was
// running, returning once the new core's controller answers. When binary
// does not come up, the runner holds no core: the caller restarts the old
// binary or reports it.
func (r *Runner) Restart(ctx context.Context, binary string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.end(); err != nil {
		return err
	}
	r.binary = binary
	// A start Hop asked for itself begins a new crash series.
	r.crashes = 0
	return r.launchAndWait(ctx, binary)
}

// RestartCurrent replaces the core with the same binary on the config it was
// running, returning once the new core's controller answers. It is how data
// the core reads once at start, such as geodata, is made current without
// changing anything else. With no core running it starts one.
func (r *Runner) RestartCurrent(ctx context.Context) error {
	r.mu.Lock()
	binary := r.binary
	r.mu.Unlock()
	return r.Restart(ctx, binary)
}

// Binary is the executable the running core was started from.
func (r *Runner) Binary() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.binary
}

// Stop ends the core and waits for it to exit.
func (r *Runner) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.end()
}

// adopt takes a freshly started core on as the runner's own: its exit is
// reported to the supervisor, and the first core adopted starts the
// supervisor. mu must be held.
func (r *Runner) adopt(proc *Process, cancel context.CancelFunc) {
	r.proc, r.cancel = proc, cancel
	r.crashes = 0
	go r.reportExit(proc)
	if !r.supervising {
		r.supervising = true
		go r.supervise()
	}
}

func (r *Runner) launchAndWait(ctx context.Context, binary string) error {
	procCtx, cancel := context.WithCancel(r.parent)
	proc, err := launch(procCtx, r.cfg, binary)
	if err != nil {
		cancel()
		return err
	}
	r.proc, r.cancel = proc, cancel
	go r.reportExit(proc)
	return r.waitReady(ctx)
}

// waitReady waits for the controller; a core that never answers is stopped,
// so a failed start leaves nothing running.
func (r *Runner) waitReady(ctx context.Context) error {
	if err := WaitReady(ctx, r.client); err != nil {
		if endErr := r.end(); endErr != nil {
			slog.Warn("mihomo exited after failed start", "error", endErr)
		}
		return err
	}
	return nil
}

// end stops the core, if one runs. The exit it causes is wanted: the
// supervisor ignores it because r.proc no longer points at that core.
func (r *Runner) end() error {
	if r.proc == nil {
		return nil
	}
	r.cancel()
	err := r.proc.Wait()
	r.proc, r.cancel = nil, nil
	if err != nil {
		return fmt.Errorf("stop mihomo: %w", err)
	}
	return nil
}
