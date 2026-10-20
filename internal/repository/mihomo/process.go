package mihomo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
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
	cmd *exec.Cmd
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

	cmd := exec.CommandContext(ctx, cfg.MihomoBinary, "-d", cfg.HomeDir, "-f", configPath)
	// Ask the core to shut down before the context kills it.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = stopGrace
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start mihomo (binary=%q home=%q): %w", cfg.MihomoBinary, cfg.HomeDir, err)
	}
	slog.Info("mihomo started", "pid", cmd.Process.Pid, "home", cfg.HomeDir, "controller", cfg.ControllerAddr, "mixed_port", cfg.MixedPort)
	return &Process{cmd: cmd}, nil
}

// Wait blocks until the core exits. An exit caused by cancelling the context
// given to Start is a normal shutdown, not an error: exec reports it as
// context.Canceled even when the core stopped cleanly after SIGTERM.
func (p *Process) Wait() error {
	if err := p.cmd.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("mihomo exited (pid=%d): %w", p.cmd.Process.Pid, err)
	}
	return nil
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
