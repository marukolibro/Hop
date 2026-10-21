//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/svc"

	"hop/internal/repository/helper"
)

// serviceStopTimeout bounds the graceful stop the service handler waits for:
// draining the socket server, then stopping the core.
const serviceStopTimeout = 20 * time.Second

// validateOptions checks the flags the helper needs on Windows. A SID is
// optional for a helper run by hand (it then serves the user who ran it);
// the install always passes one. The DNS takeover is macOS's: on Windows
// sing-tun gives the Wintun adapter its own DNS server and metric, and the
// helper must never touch the system DNS.
func validateOptions(opts serverOptions) error {
	switch {
	case opts.socket == "":
		return errors.New("-socket is required")
	case opts.home == "":
		return errors.New("-home is required")
	case opts.core == "":
		return errors.New("-core is required")
	case opts.manageDNS:
		return errors.New("-manage-dns is not used on Windows: the TUN adapter carries its own DNS")
	case opts.allowSID != "" && !helper.ValidSIDText(opts.allowSID):
		return fmt.Errorf("-allow-sid is not a SID (%q)", opts.allowSID)
	}
	return nil
}

// serveCommand serves until the service control manager asks the service to
// stop, or until Ctrl+C for a helper run by hand in a terminal.
func serveCommand(parent context.Context, srv *server) error {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return fmt.Errorf("tell whether the helper runs as a service: %w", err)
	}
	if !isService {
		ctx, stop := signal.NotifyContext(parent, os.Interrupt)
		defer stop()
		return srv.serve(ctx)
	}
	srv.opts.serviceMode = true
	closeLog, err := redirectServiceLogs(srv.opts.home)
	if err != nil {
		return err
	}
	defer closeLog()
	return svc.Run(helper.ServiceName, &serviceHandler{srv: srv})
}

// maxServiceLogBytes is where the service's log is restarted: a core can log
// for days, and the file is a diagnostic, not a record.
const maxServiceLogBytes = 8 << 20

// redirectServiceLogs sends the helper's and the core's logs to a file in
// the helper's home: a service has no console to write to.
func redirectServiceLogs(home string) (func(), error) {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, fmt.Errorf("create the helper's home (dir=%q): %w", home, err)
	}
	path := filepath.Join(home, "hop-helper.log")
	if info, err := os.Stat(path); err == nil && info.Size() > maxServiceLogBytes {
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("restart the helper's log (path=%q): %w", path, err)
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open the helper's log (path=%q): %w", path, err)
	}
	os.Stdout, os.Stderr = file, file
	slog.SetDefault(slog.New(slog.NewTextHandler(file, nil)))
	return func() { file.Close() }, nil
}

// serviceHandler serves the helper while the service control manager runs
// it, and stops the core on a stop request.
type serviceHandler struct {
	srv *server
}

// Execute is the service's entry point: it serves the socket until a stop
// or shutdown request cancels the context, then reports the outcome.
func (h *serviceHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const accepts = svc.AcceptStop | svc.AcceptShutdown
	status <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.srv.serve(ctx) }()
	select {
	case <-h.srv.ready:
	case err := <-done:
		if err != nil {
			slog.Error("hop-helper failed to start", "error", err)
			return false, 1
		}
		return false, 0
	}
	status <- svc.Status{State: svc.Running, Accepts: accepts}
	for {
		select {
		case err := <-done:
			if err != nil {
				slog.Error("hop-helper stopped on its own", "error", err)
				return false, 1
			}
			return false, 0
		case request := <-requests:
			switch request.Cmd {
			case svc.Interrogate:
				status <- request.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{
					State:    svc.StopPending,
					WaitHint: uint32(serviceStopTimeout / time.Millisecond),
				}
				cancel()
				select {
				case err := <-done:
					if err != nil {
						slog.Error("hop-helper did not stop cleanly", "error", err)
						return false, 1
					}
					return false, 0
				case <-time.After(serviceStopTimeout):
					slog.Error("hop-helper did not stop before the service manager's deadline")
					return false, 1
				}
			}
		}
	}
}
