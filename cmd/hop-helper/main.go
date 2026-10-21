// Command hop-helper is Hop's privileged helper: the process that owns the
// mihomo core while TUN mode is on. Hop drives it over a unix socket, and
// every request is checked against the peer's credentials: on macOS by the
// peer's uid, on Windows by the socket directory's ACL. In production it
// runs under a LaunchDaemon on macOS and as a Windows service on Windows;
// every path it uses comes from its flags, so tests can run it unprivileged
// in temp directories.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
)

func main() {
	if err := run(context.Background()); err != nil {
		slog.Error("hop-helper failed", "error", err)
		os.Exit(1)
	}
}

func run(parent context.Context) error {
	// Windows defines the elevated install and uninstall commands, which run
	// before any of the serving flags apply.
	if handled, err := runPlatformCommand(parent, os.Args[1:]); handled {
		return err
	}
	socket := flag.String("socket", "", "unix socket to serve Hop on (required)")
	home := flag.String("home", "", "directory holding the helper's own files (required)")
	core := flag.String("core", "", "mihomo binary the helper starts (required)")
	allowUID := flag.Int("allow-uid", -1, "macOS: uid served beside root (required)")
	allowSID := flag.String("allow-sid", "", "Windows: SID of the installing user allowed to connect")
	manageDNS := flag.Bool("manage-dns", false, "macOS: take the system DNS over while a core runs with TUN on")
	flag.Parse()
	opts := serverOptions{
		socket:    *socket,
		home:      *home,
		core:      *core,
		allowUID:  *allowUID,
		allowSID:  *allowSID,
		manageDNS: *manageDNS,
	}
	if err := validateOptions(opts); err != nil {
		return err
	}
	slog.Info("hop-helper starting",
		"socket", opts.socket, "home", opts.home, "core", opts.core,
		"allow_uid", opts.allowUID, "allow_sid", opts.allowSID, "manage_dns", opts.manageDNS,
		"version", helperVersion, "pid", os.Getpid())
	return serveCommand(parent, newServer(opts))
}
