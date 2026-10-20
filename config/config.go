// Package config loads the settings Hop needs to run its own mihomo core.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
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
	// MihomoBinary is the absolute path of the mihomo executable.
	MihomoBinary string
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
// userConfigDir/hop, apart from any other Clash client. The mihomo binary has no fallback: it must
// be given through HOP_MIHOMO_BINARY.
func Load(userConfigDir string) (Config, error) {
	binary := os.Getenv(envMihomoBinary)
	if binary == "" {
		return Config{}, fmt.Errorf("environment variable %s is not set: point it to a mihomo executable", envMihomoBinary)
	}
	if _, err := os.Stat(binary); err != nil {
		return Config{}, fmt.Errorf("mihomo binary from %s is not usable (path=%q): %w", envMihomoBinary, binary, err)
	}
	secret, err := newSecret()
	if err != nil {
		return Config{}, err
	}
	appDir := filepath.Join(userConfigDir, appDirName)
	return Config{
		MihomoBinary:   binary,
		AppDir:         appDir,
		HomeDir:        filepath.Join(appDir, "mihomo"),
		MixedPort:      mixedPort,
		ControllerAddr: controllerAddr,
		Secret:         secret,
	}, nil
}

func newSecret() (string, error) {
	raw := make([]byte, secretBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate controller secret: %w", err)
	}
	return hex.EncodeToString(raw), nil
}
