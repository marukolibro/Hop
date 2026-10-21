//go:build !windows

package main

import (
	"context"
	"errors"
	"os/signal"
	"syscall"
)

// runPlatformCommand handles commands another platform defines.
func runPlatformCommand(context.Context, []string) (bool, error) { return false, nil }

// validateOptions checks the flags the helper needs on this platform.
func validateOptions(opts serverOptions) error {
	switch {
	case opts.socket == "":
		return errors.New("-socket is required")
	case opts.home == "":
		return errors.New("-home is required")
	case opts.core == "":
		return errors.New("-core is required")
	case opts.allowUID < 0:
		return errors.New("-allow-uid is required")
	}
	return nil
}

// serveCommand serves until SIGTERM or SIGINT, as launchd sends them.
func serveCommand(parent context.Context, srv *server) error {
	ctx, stop := signal.NotifyContext(parent, syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	return srv.serve(ctx)
}
