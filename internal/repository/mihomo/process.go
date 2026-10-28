package mihomo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"hop/config"
)

const (
	configFileName = "config.yaml"
	homeDirPerm    = 0o700
	configPerm     = 0o600
	stopGrace      = 3 * time.Second

	readyAttempts = 40
	readyInterval = 250 * time.Millisecond
)

// Process is a running mihomo core owned by Hop.
type Process struct {
	cmd     *exec.Cmd
	pidFile string
	// startedAt is when the core was started; a supervisor tells a stable
	// run from a fast crash by it.
	startedAt time.Time
	// stopped is done once Hop asked the core to stop, by cancelling the
	// context it was started with.
	stopped <-chan struct{}

	// done is closed once the core has exited and been waited, so any
	// number of Wait callers hear the one result in waitErr.
	done    chan struct{}
	waitErr error
}

// ConfigPath is the config file of a core whose home directory is homeDir.
func ConfigPath(homeDir string) string {
	return filepath.Join(homeDir, configFileName)
}

// Start writes configYAML into the core's own home directory and starts the
// core. The core is stopped when ctx is cancelled; Wait reports when it has
// exited.
func Start(ctx context.Context, cfg config.Config, configYAML []byte) (*Process, error) {
	if err := os.MkdirAll(cfg.HomeDir, homeDirPerm); err != nil {
		return nil, fmt.Errorf("create mihomo home dir (path=%q): %w", cfg.HomeDir, err)
	}
	configPath := ConfigPath(cfg.HomeDir)
	if err := os.WriteFile(configPath, configYAML, configPerm); err != nil {
		return nil, fmt.Errorf("write mihomo config (path=%q): %w", configPath, err)
	}
	return launch(ctx, cfg, cfg.MihomoBinary)
}

// launch starts binary on the config file already in the home directory,
// first stopping a core an earlier Hop left running.
func launch(ctx context.Context, cfg config.Config, binary string) (*Process, error) {
	pidFile := filepath.Join(cfg.AppDir, pidFileName)
	if err := ReapStale(pidFile, cfg.HomeDir); err != nil {
		return nil, err
	}
	if err := waitPortsFree(ctx, cfg); err != nil {
		return nil, err
	}
	configPath := ConfigPath(cfg.HomeDir)
	cmd := coreCommand(ctx, binary, "-d", cfg.HomeDir, "-f", configPath)
	// Ask the core to shut down before the context kills it.
	cmd.Cancel = func() error { return requestStop(cmd.Process) }
	cmd.WaitDelay = stopGrace
	cmd.Stdout = cfg.CoreOutput
	cmd.Stderr = cfg.CoreOutput
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start mihomo (binary=%q home=%q): %w", binary, cfg.HomeDir, err)
	}
	if err := writePidFile(pidFile, cmd.Process.Pid); err != nil {
		// Without the pid file a crash of Hop would leave the core
		// unrecoverable, so do not run it.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, err
	}
	p := &Process{cmd: cmd, pidFile: pidFile, startedAt: time.Now(), stopped: ctx.Done(), done: make(chan struct{})}
	go p.await()
	slog.Info("mihomo started", "pid", cmd.Process.Pid, "binary", binary, "home", cfg.HomeDir, "controller", cfg.ControllerAddr, "mixed_port", cfg.MixedPort)
	return p, nil
}

// await waits the core and records why it left, so every Wait caller hears
// one result. Goroutine ownership: started by launch, it ends when the core
// has exited and its pid file was removed.
func (p *Process) await() {
	waitErr := p.cmd.Wait()
	if waitErr != nil {
		if p.stopRequested() {
			// A shutdown Hop asked for is a normal stop, whatever the exit
			// status: after SIGTERM the core exits 0 and exec reports the
			// cancelled context, but Windows can only kill it, which exits 1
			// and exec reports that instead.
			waitErr = nil
		} else {
			waitErr = fmt.Errorf("mihomo exited (pid=%d): %w", p.cmd.Process.Pid, waitErr)
		}
	}
	p.waitErr = errors.Join(waitErr, removePidFile(p.pidFile))
	close(p.done)
}

// stopRequested says whether Hop has asked the core to stop.
func (p *Process) stopRequested() bool {
	select {
	case <-p.stopped:
		return true
	default:
		return false
	}
}

// Wait blocks until the core exits. An exit caused by cancelling the context
// given to Start is a normal shutdown, not an error: exec reports it as
// context.Canceled even when the core stopped cleanly after SIGTERM. Any
// number of callers may wait; all hear the same result.
func (p *Process) Wait() error {
	<-p.done
	return p.waitErr
}

// WaitReady polls the controller until it answers, retrying with a warning
// for each failed attempt, and returns the last error when it never does.
func WaitReady(ctx context.Context, client *Client) error {
	var last error
	for attempt := 1; attempt <= readyAttempts; attempt++ {
		_, err := client.Version(ctx)
		if err == nil {
			return nil
		}
		last = err
		slog.Warn("mihomo controller not ready", "attempt", attempt, "max_attempts", readyAttempts, "error", err)
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for mihomo controller: %w", ctx.Err())
		case <-time.After(readyInterval):
		}
	}
	return fmt.Errorf("mihomo controller not ready after %d attempts: %w", readyAttempts, last)
}
