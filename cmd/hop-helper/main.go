// Command hop-helper is Hop's privileged helper: the root-run process that
// owns the mihomo core while TUN mode is on. Hop drives it over a unix
// socket, and every request is checked against the peer's credentials, so
// only the installing user and root are served. In production it runs under
// a LaunchDaemon; every path it uses comes from its flags, so tests can run
// it unprivileged in temp directories.
package main

import (
	"context"
	"flag"
	"fmt"
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
	socket := flag.String("socket", "", "unix socket to serve Hop on (required)")
	home := flag.String("home", "", "directory holding the helper's own files (required)")
	core := flag.String("core", "", "mihomo binary the helper starts (required)")
	allowUID := flag.Int("allow-uid", -1, "uid served beside root (required)")
	manageDNS := flag.Bool("manage-dns", false, "take the system DNS over while a core runs with TUN on")
	flag.Parse()
	switch {
	case *socket == "":
		return fmt.Errorf("-socket is required")
	case *home == "":
		return fmt.Errorf("-home is required")
	case *core == "":
		return fmt.Errorf("-core is required")
	case *allowUID < 0:
		return fmt.Errorf("-allow-uid is required")
	}
	slog.Info("hop-helper starting", "socket", *socket, "home", *home, "core", *core, "allow_uid", *allowUID, "manage_dns", *manageDNS, "version", helperVersion, "pid", os.Getpid())
	srv := newServer(serverOptions{socket: *socket, home: *home, core: *core, allowUID: uint32(*allowUID), manageDNS: *manageDNS})
	return srv.serve(parent)
}
