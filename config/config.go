// Package config loads the settings Hop needs to run its own mihomo core.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"hop/internal/model"
	"hop/internal/repository/corestore"
)

const (
	envMihomoBinary = "HOP_MIHOMO_BINARY"

	secretBytes = 16

	// secretFileName keeps the controller secret between starts;
	// secretFilePerm is owner-only, like everything else in the app dir.
	secretFileName = "controller-secret"
	secretFilePerm = 0o600
	// appDirPerm keeps the app dir owner-only: it holds the subscriptions
	// and the controller secret.
	appDirPerm = 0o700
)

// Instance is one copy of Hop on a machine: where it keeps its files and the
// ports its core listens on. The ports are deliberately away from the ones
// other Clash clients use, so Hop can run beside them without touching their
// listeners; the development build has its own too, so it runs beside an
// installed Hop without sharing its files, ports or core.
type Instance struct {
	// AppDirName is the directory under the user's config dir.
	AppDirName string
	// MixedPort is the local HTTP/SOCKS port.
	MixedPort int
	// ControllerAddr is host:port of the core's REST controller.
	ControllerAddr string
	// NodeDNSAddr is the core's own DNS listener, used only to look up
	// nodes' servers when a subscription sends them through its hosts.
	NodeDNSAddr string
	// ProbePort is the first of model.ProbeLanes loopback ports the unlock
	// checks go through; each sends everything to the group Hop picked in its
	// lane, never by the rules.
	ProbePort int
	// UsesHelper says this instance may drive the machine's privileged
	// helper. There is one helper per machine, so only the installed Hop
	// does: another instance would stop or replace the core under it.
	UsesHelper bool
}

// Release is the installed Hop. Development is the build run by mygo dev.
var (
	Release     = Instance{AppDirName: "hop", MixedPort: 17890, ControllerAddr: "127.0.0.1:19090", NodeDNSAddr: "127.0.0.1:17853", ProbePort: 17900, UsesHelper: true}
	Development = Instance{AppDirName: "hop-dev", MixedPort: 17892, ControllerAddr: "127.0.0.1:19092", NodeDNSAddr: "127.0.0.1:17855", ProbePort: 17910, UsesHelper: false}
)

// Config is everything the sidecar core and its controller client need.
type Config struct {
	// MihomoBinary is the mihomo executable to run.
	MihomoBinary string
	// BundledBinary is the core that ships with the app, which Hop can always
	// go back to.
	BundledBinary string
	// CoreOrigin says which of the cores MihomoBinary is.
	CoreOrigin model.CoreOrigin
	// AppDir holds everything Hop stores, such as the imported subscription.
	AppDir string
	// HomeDir is the core's private working directory (-d).
	HomeDir string
	// MixedPort is the local HTTP/SOCKS port the core listens on.
	MixedPort int
	// ControllerAddr is host:port of the core's REST controller.
	ControllerAddr string
	// NodeDNSAddr is host:port of the core's DNS listener for node servers.
	NodeDNSAddr string
	// ProbePort is the first loopback port of the unlock checks' listeners.
	ProbePort int
	// Secret authenticates controller requests; it is kept between starts so
	// a core TUN mode left running under the helper still answers after Hop
	// comes back.
	Secret string
}

// Load reads the configuration of instance; the core keeps its files under
// userConfigDir/<instance's directory>, apart from any other Clash client.
// resourcesDir is where the app's resources are, which hold the bundled core.
func Load(instance Instance, userConfigDir string, resourcesDir string) (Config, error) {
	appDir := filepath.Join(userConfigDir, instance.AppDirName)
	bundled, bundledErr := findBundledBinary(resourcesDir)
	binary, origin, err := chooseBinary(appDir, bundled, bundledErr)
	if err != nil {
		return Config{}, err
	}
	// The controller secret is kept between starts, not made anew each one:
	// a core TUN mode left running under the helper — across a crash, an
	// adopted restart — keeps answering the controller Hop reconnects with.
	// It lives beside the config file that carries it anyway, owner-only.
	secret, err := loadOrCreateSecret(filepath.Join(appDir, secretFileName))
	if err != nil {
		return Config{}, err
	}
	return Config{
		MihomoBinary:   binary,
		BundledBinary:  bundled,
		CoreOrigin:     origin,
		AppDir:         appDir,
		HomeDir:        filepath.Join(appDir, "mihomo"),
		MixedPort:      instance.MixedPort,
		ControllerAddr: instance.ControllerAddr,
		NodeDNSAddr:    instance.NodeDNSAddr,
		ProbePort:      instance.ProbePort,
		Secret:         secret,
	}, nil
}

// chooseBinary picks the core to run, in this order:
//
//  1. HOP_MIHOMO_BINARY, for development against another build.
//  2. The core the user upgraded to, when its files are intact; a damaged
//     one is warned about and the bundled core runs instead, so Hop still
//     starts and the user can upgrade again.
//  3. The bundled core.
func chooseBinary(appDir string, bundled string, bundledErr error) (string, model.CoreOrigin, error) {
	if override := os.Getenv(envMihomoBinary); override != "" {
		if _, err := os.Stat(override); err != nil {
			return "", "", fmt.Errorf("mihomo binary from %s is not usable (path=%q): %w", envMihomoBinary, override, err)
		}
		return override, model.CoreOverride, nil
	}
	if bundledErr != nil {
		return "", "", bundledErr
	}
	tag, installed, found, err := corestore.New(appDir).Current()
	if err != nil {
		slog.Warn("the upgraded mihomo core is unusable, running the bundled one", "error", err)
		return bundled, model.CoreBundled, nil
	}
	if found {
		slog.Info("running the upgraded mihomo core", "tag", tag, "binary", installed)
		return installed, model.CoreInstalled, nil
	}
	return bundled, model.CoreBundled, nil
}

// findBundledBinary locates the core that ships with the app, in this order:
//
//  1. resources/bin, where a packaged app and `mygo dev` have it.
//  2. resources/<os>-<arch>/bin, where `go run` finds it in the source tree.
//
// Without either it fails, naming what it tried.
func findBundledBinary(resourcesDir string) (string, error) {
	name := "mihomo"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	candidates := []string{
		filepath.Join(resourcesDir, "bin", name),
		filepath.Join(resourcesDir, runtime.GOOS+"-"+runtime.GOARCH, "bin", name),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no bundled mihomo core found (tried %v): run scripts/fetch-mihomo.sh %s-%s", candidates, runtime.GOOS, runtime.GOARCH)
}

// loadOrCreateSecret returns the controller secret kept at path, generating
// and storing one when the file does not exist yet. Stored owner-only, it is
// no more exposed than the core's own config file, which carries the secret
// during every session.
func loadOrCreateSecret(path string) (string, error) {
	if raw, err := os.ReadFile(path); err == nil {
		if secret := strings.TrimSpace(string(raw)); secret != "" {
			return secret, nil
		}
		return "", fmt.Errorf("the stored controller secret is empty (path=%q)", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read the stored controller secret (path=%q): %w", path, err)
	}
	secret, err := newSecret()
	if err != nil {
		return "", err
	}
	// On a Mac Hop never ran on, the app dir does not exist yet.
	if err := os.MkdirAll(filepath.Dir(path), appDirPerm); err != nil {
		return "", fmt.Errorf("create the app dir for the controller secret (path=%q): %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(secret), secretFilePerm); err != nil {
		return "", fmt.Errorf("store the controller secret (path=%q): %w", path, err)
	}
	return secret, nil
}

func newSecret() (string, error) {
	raw := make([]byte, secretBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate controller secret: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// OwnPorts are the ports Hop's other listeners hold: the controller, the
// core's DNS and the unlock checks' lanes. The proxy port may not take them.
func (c Config) OwnPorts() ([]int, error) {
	var ports []int
	for _, addr := range []string{c.ControllerAddr, c.NodeDNSAddr} {
		if addr == "" {
			continue
		}
		_, raw, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("listen address is not host:port (addr=%q): %w", addr, err)
		}
		port, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("listen address has no numeric port (addr=%q): %w", addr, err)
		}
		ports = append(ports, port)
	}
	if c.ProbePort > 0 {
		for lane := range model.ProbeLanes {
			ports = append(ports, c.ProbePort+lane)
		}
	}
	return ports, nil
}
