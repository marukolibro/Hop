// Package config loads the settings Hop needs to run its own mihomo core.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"

	"hop/internal/model"
	"hop/internal/repository/corestore"
)

const (
	envMihomoBinary = "HOP_MIHOMO_BINARY"

	// Ports are deliberately away from the ones other Clash clients use, so
	// Hop can run beside them without touching their listeners.
	mixedPort      = 17890
	controllerAddr = "127.0.0.1:19090"

	secretBytes = 16
	appDirName  = "hop"
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
	// Secret authenticates controller requests; it is new on every start.
	Secret string
}

// Load reads the configuration; the core keeps its files under
// userConfigDir/hop, apart from any other Clash client. resourcesDir is
// where the app's resources are, which hold the bundled core.
func Load(userConfigDir string, resourcesDir string) (Config, error) {
	appDir := filepath.Join(userConfigDir, appDirName)
	bundled, bundledErr := findBundledBinary(resourcesDir)
	binary, origin, err := chooseBinary(appDir, bundled, bundledErr)
	if err != nil {
		return Config{}, err
	}
	secret, err := newSecret()
	if err != nil {
		return Config{}, err
	}
	return Config{
		MihomoBinary:   binary,
		BundledBinary:  bundled,
		CoreOrigin:     origin,
		AppDir:         appDir,
		HomeDir:        filepath.Join(appDir, "mihomo"),
		MixedPort:      mixedPort,
		ControllerAddr: controllerAddr,
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

func newSecret() (string, error) {
	raw := make([]byte, secretBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate controller secret: %w", err)
	}
	return hex.EncodeToString(raw), nil
}
