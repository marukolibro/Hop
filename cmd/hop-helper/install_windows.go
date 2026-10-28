//go:build windows

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"hop/internal/repository/helper"
	"hop/internal/repository/mihomo"
)

// serviceStartTimeout bounds how long an install waits for the service it
// registered to report Running.
const serviceStartTimeout = 20 * time.Second

// runPlatformCommand handles the elevated commands Hop runs through UAC:
// install and uninstall. They run before the serving flags apply.
func runPlatformCommand(_ context.Context, args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch args[0] {
	case "install":
		return true, runInstall(args[1:])
	case "uninstall":
		return true, runUninstall(args[1:])
	}
	return false, nil
}

// runInstall copies this helper and the core into Program Files, guards the
// socket directory with a DACL, registers the service and starts it. It runs
// elevated, as the user's explicit approval at the UAC prompt; it never
// elevates anything itself.
func runInstall(args []string) error {
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	core := flags.String("core", "", "mihomo binary to install")
	geodata := flags.String("geodata", "", "directory holding GeoSite.dat and geoip.metadb")
	sid := flags.String("sid", "", "SID of the user allowed to talk to the helper")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("read the install arguments: %w", err)
	}
	if *core == "" {
		return errors.New("install: -core is required")
	}
	userSID, err := resolveSID(*sid)
	if err != nil {
		return err
	}
	logFile, err := openInstallLog(userSID)
	if err != nil {
		return err
	}
	defer logFile.Close()
	useLog(logFile)
	slog.Info("installing the helper", "core", *core, "geodata", *geodata, "sid", userSID, "dir", helper.InstallDir)

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate the helper binary: %w", err)
	}
	for _, dir := range []string{helper.InstallDir, filepath.Dir(helper.CorePath), helper.HomeDir, filepath.Dir(helper.SocketPath)} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create the helper's directory (dir=%q): %w", dir, err)
		}
	}
	if err := applyDirSDDL(helper.HomeDir, helper.HomeSDDL()); err != nil {
		return fmt.Errorf("guard the helper's home (dir=%q): %w", helper.HomeDir, err)
	}
	socketSDDL, err := helper.SocketDirSDDL(userSID)
	if err != nil {
		return err
	}
	if err := applyDirSDDL(filepath.Dir(helper.SocketPath), socketSDDL); err != nil {
		return fmt.Errorf("guard the helper's socket directory (dir=%q): %w", filepath.Dir(helper.SocketPath), err)
	}

	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to the service control manager: %w", err)
	}
	defer manager.Disconnect()
	if err := removeExistingService(manager); err != nil {
		return err
	}
	// A helper that crashed may have left its core running; the pid file and
	// the command line carrying the helper's home are the only things this
	// acts on, and the core's file must not be replaced under it.
	if err := mihomo.ReapStale(filepath.Join(helper.HomeDir, "mihomo.pid"), helper.HomeDir); err != nil {
		return fmt.Errorf("stop the core an earlier helper left running: %w", err)
	}

	if !strings.EqualFold(filepath.Clean(self), filepath.Clean(helper.ToolPath)) {
		if err := copyFileAtomic(self, helper.ToolPath, 0o755); err != nil {
			return err
		}
	}
	if err := installCore(*core); err != nil {
		return err
	}
	if *geodata != "" {
		if err := mihomo.SeedGeodata(*geodata, helper.HomeDir); err != nil {
			return fmt.Errorf("seed the helper's geodata: %w", err)
		}
	}

	service, err := manager.CreateService(
		helper.ServiceName,
		helper.ToolPath,
		mgr.Config{
			DisplayName:  helper.ServiceDisplayName,
			Description:  helper.ServiceDescription,
			StartType:    mgr.StartAutomatic,
			ErrorControl: mgr.ErrorNormal,
		},
		helper.ServiceArgs(helper.SocketPath, helper.HomeDir, helper.CorePath, userSID)...,
	)
	if err != nil {
		return fmt.Errorf("register the helper service: %w", err)
	}
	defer service.Close()
	if err := service.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
	}, 24*60*60); err != nil {
		return fmt.Errorf("set the helper service's recovery actions: %w", err)
	}
	if err := service.Start(); err != nil {
		return fmt.Errorf("start the helper service: %w", err)
	}
	if err := waitServiceRunning(service, serviceStartTimeout); err != nil {
		return fmt.Errorf("the helper service did not come up: %w%s", err, serviceLogTail())
	}
	slog.Info("installed the helper", "dir", helper.InstallDir, "service", helper.ServiceName)
	return nil
}

// runUninstall stops and deletes the service and removes everything the
// install put in place. It runs from a copy in the temp directory, so the
// installed binary can be deleted while this process keeps running.
func runUninstall(args []string) error {
	flags := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	sid := flags.String("sid", "", "SID of the user allowed to read the log")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("read the uninstall arguments: %w", err)
	}
	userSID, err := resolveSID(*sid)
	if err != nil {
		return err
	}
	logFile, err := openInstallLog(userSID)
	if err != nil {
		return err
	}
	defer logFile.Close()
	useLog(logFile)
	slog.Info("uninstalling the helper", "dir", helper.InstallDir, "service", helper.ServiceName)

	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to the service control manager: %w", err)
	}
	if err := removeExistingService(manager); err != nil {
		manager.Disconnect()
		return err
	}
	if err := manager.Disconnect(); err != nil {
		return fmt.Errorf("disconnect from the service control manager: %w", err)
	}
	if err := os.Remove(helper.SocketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove the helper socket (path=%q): %w", helper.SocketPath, err)
	}
	if err := os.RemoveAll(helper.InstallDir); err != nil {
		return fmt.Errorf("remove the helper's files (dir=%q): %w", helper.InstallDir, err)
	}
	if err := os.Remove(filepath.Dir(helper.InstallDir)); err != nil {
		// The parent is shared with nothing else today; a failure here means
		// something else was put beside the helper, which is not ours to
		// remove.
		slog.Info("leaving the Hop program directory", "path", filepath.Dir(helper.InstallDir), "error", err)
	}
	slog.Info("uninstalled the helper")
	return nil
}

// resolveSID reads the installing user's SID: the flag when Hop passed one,
// otherwise this process's own user (a helper installed by hand).
func resolveSID(flagValue string) (string, error) {
	if flagValue == "" {
		return helper.CurrentUserSID()
	}
	if !helper.ValidSIDText(flagValue) {
		return "", fmt.Errorf("the installing user's SID is not a SID (%q)", flagValue)
	}
	return flagValue, nil
}

// openInstallLog prepares the fixed, user-readable record of this elevated
// run. The path comes from helper.InstallLogPath, never from a caller, so
// the elevated process cannot be made to write anywhere else.
func openInstallLog(userSID string) (*os.File, error) {
	path := helper.InstallLogPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create the install log directory (dir=%q): %w", dir, err)
	}
	sddl, err := helper.LogDirSDDL(userSID)
	if err != nil {
		return nil, err
	}
	if err := applyDirSDDL(dir, sddl); err != nil {
		return nil, fmt.Errorf("guard the install log directory (dir=%q): %w", dir, err)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("clear the old install log (path=%q): %w", path, err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open the install log (path=%q): %w", path, err)
	}
	return file, nil
}

// useLog sends this process's output, including the core's inherited stderr
// and slog, to the install log.
func useLog(file *os.File) {
	os.Stdout, os.Stderr = file, file
	slog.SetDefault(slog.New(slog.NewTextHandler(file, nil)))
}

// installCore stages the core, has it identify itself as mihomo, and moves
// the checked bytes into place. A running core is never replaced: the
// service was stopped above.
func installCore(src string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open the core to install (path=%q): %w", src, err)
	}
	defer in.Close()
	staged, err := os.CreateTemp(filepath.Dir(helper.CorePath), "mihomo-*.exe")
	if err != nil {
		return fmt.Errorf("stage the core (dir=%q): %w", filepath.Dir(helper.CorePath), err)
	}
	stagedName := staged.Name()
	_, copyErr := io.Copy(staged, in)
	closeErr := staged.Close()
	if copyErr != nil || closeErr != nil {
		os.Remove(stagedName)
		return fmt.Errorf("write the staged core (path=%q): %w", stagedName, errors.Join(copyErr, closeErr))
	}
	if err := os.Chmod(stagedName, 0o755); err != nil {
		os.Remove(stagedName)
		return fmt.Errorf("make the staged core executable (path=%q): %w", stagedName, err)
	}
	if _, err := mihomo.BinaryVersion(context.Background(), stagedName); err != nil {
		os.Remove(stagedName)
		return fmt.Errorf("the file to install is not a mihomo core (path=%q): %w", src, err)
	}
	if err := os.Rename(stagedName, helper.CorePath); err != nil {
		os.Remove(stagedName)
		return fmt.Errorf("put the core in place (path=%q): %w", helper.CorePath, err)
	}
	return nil
}

// copyFileAtomic writes src to dst through a temporary file in dst's
// directory, so an interrupted copy never leaves a half binary in place.
func copyFileAtomic(src string, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open the file to install (path=%q): %w", src, err)
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".*")
	if err != nil {
		return fmt.Errorf("stage the file (path=%q): %w", dst, err)
	}
	name := tmp.Name()
	_, copyErr := io.Copy(tmp, in)
	chmodErr := tmp.Chmod(perm)
	closeErr := tmp.Close()
	if copyErr != nil || chmodErr != nil || closeErr != nil {
		os.Remove(name)
		return fmt.Errorf("write the staged file (path=%q): %w", name, errors.Join(copyErr, chmodErr, closeErr))
	}
	if err := os.Rename(name, dst); err != nil {
		os.Remove(name)
		return fmt.Errorf("put the file in place (path=%q): %w", dst, err)
	}
	return nil
}

// removeExistingService stops and deletes a previous helper service, if one
// is registered, so the new binary is never overwritten under a running
// process.
func removeExistingService(manager *mgr.Mgr) error {
	service, err := manager.OpenService(helper.ServiceName)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open the existing helper service: %w", err)
	}
	defer service.Close()
	if err := stopService(service, serviceStopTimeout); err != nil {
		return err
	}
	if err := service.Delete(); err != nil {
		return fmt.Errorf("delete the existing helper service: %w", err)
	}
	return nil
}

// stopService asks the service to stop until it reports Stopped. A service
// that is still starting can refuse the request for a moment, so that answer
// is retried rather than failed.
func stopService(service *mgr.Service, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		status, err := service.Query()
		if err != nil {
			return fmt.Errorf("query the helper service while stopping: %w", err)
		}
		if status.State == svc.Stopped {
			return nil
		}
		if _, err := service.Control(svc.Stop); err != nil &&
			!errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) &&
			!errors.Is(err, windows.ERROR_SERVICE_CANNOT_ACCEPT_CTRL) &&
			!errors.Is(err, windows.ERROR_INVALID_SERVICE_CONTROL) {
			return fmt.Errorf("stop the helper service: %w", err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the helper service did not stop (state=%d)", status.State)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// waitServiceRunning waits for a freshly started service to report Running,
// and reports a service that stopped instead.
func waitServiceRunning(service *mgr.Service, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		status, err := service.Query()
		if err != nil {
			return fmt.Errorf("query the helper service while starting: %w", err)
		}
		switch status.State {
		case svc.Running:
			return nil
		case svc.Stopped:
			return fmt.Errorf("the service stopped while starting (win32 exit=%d)", status.Win32ExitCode)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the service stayed in state %d", status.State)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// serviceLogTail is the end of the service's own log, for an install log
// that the user can read (the service's home is admin-only).
func serviceLogTail() string {
	const maxTail = 4096
	path := filepath.Join(helper.HomeDir, "hop-helper.log")
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ""
	}
	if info.Size() > maxTail {
		if _, err := file.Seek(info.Size()-maxTail, io.SeekStart); err != nil {
			return ""
		}
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxTail))
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return ""
	}
	return ":\n" + text
}
