package mihomo

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	pidFileName = "mihomo.pid"
	pidFilePerm = 0o600
	stalePoll   = 100 * time.Millisecond
)

// ReapStale stops a core a previous Hop left running, such as after it was
// killed. It acts only on the pid Hop itself recorded, and only when that
// process's command line says it was started with this Hop's own home
// directory; any other process, a Clash client's core included, is never
// touched, even when it reused the pid.
func ReapStale(pidFile string, homeDir string) error {
	raw, err := os.ReadFile(pidFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read mihomo pid file (path=%q): %w", pidFile, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return fmt.Errorf("mihomo pid file does not hold a pid (path=%q content=%q): %w", pidFile, raw, err)
	}
	line, err := commandLine(pid)
	if err != nil {
		return err
	}
	if !strings.Contains(line, "-d "+homeDir+" ") {
		// Gone, or the pid belongs to something else now.
		slog.Info("recorded mihomo pid is not Hop's core, left alone", "pid", pid, "running", line != "")
		return removePidFile(pidFile)
	}
	if err := terminate(pid, homeDir); err != nil {
		return err
	}
	return removePidFile(pidFile)
}

// terminate stops Hop's own leftover core, politely first.
func terminate(pid int, homeDir string) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find leftover mihomo (pid=%d): %w", pid, err)
	}
	slog.Warn("stopping a mihomo left running by an earlier Hop", "pid", pid)
	if err := requestStop(proc); err != nil {
		return fmt.Errorf("stop leftover mihomo (pid=%d): %w", pid, err)
	}
	deadline := time.Now().Add(stopGrace)
	for time.Now().Before(deadline) {
		line, err := commandLine(pid)
		if err != nil {
			return err
		}
		if !strings.Contains(line, "-d "+homeDir+" ") {
			return nil
		}
		time.Sleep(stalePoll)
	}
	if err := proc.Kill(); err != nil {
		return fmt.Errorf("kill leftover mihomo (pid=%d): %w", pid, err)
	}
	return nil
}

func writePidFile(pidFile string, pid int) error {
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), pidFilePerm); err != nil {
		return fmt.Errorf("write mihomo pid file (path=%q): %w", pidFile, err)
	}
	return nil
}

func removePidFile(pidFile string) error {
	if err := os.Remove(pidFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove mihomo pid file (path=%q): %w", pidFile, err)
	}
	return nil
}
