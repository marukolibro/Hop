package mihomo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// versionPattern finds the release in the output of `mihomo -v`, such as
// "Mihomo Meta v1.19.32 darwin arm64 with go1.26.8 ...".
var versionPattern = regexp.MustCompile(`v\d+\.\d+\.\d+`)

const versionTimeout = 10 * time.Second

// CoreChecker tells whether a mihomo binary can take over from the running
// one: it runs, and the config the running core uses passes its own config
// test.
type CoreChecker struct {
	configPath string
	dir        string
	timeout    time.Duration
}

// NewCoreChecker returns a checker that tests the config at configPath in
// dir, each test bounded by timeout.
func NewCoreChecker(configPath string, dir string, timeout time.Duration) *CoreChecker {
	return &CoreChecker{configPath: configPath, dir: dir, timeout: timeout}
}

// Check returns the version binary reports once it ran and accepted the
// running config.
func (c *CoreChecker) Check(ctx context.Context, binary string) (string, error) {
	version, err := BinaryVersion(ctx, binary)
	if err != nil {
		return "", err
	}
	config, err := os.ReadFile(c.configPath)
	if err != nil {
		return "", fmt.Errorf("read the running config (path=%q): %w", c.configPath, err)
	}
	if err := NewConfigValidator(binary, c.dir, c.timeout).Validate(ctx, config); err != nil {
		return "", fmt.Errorf("core %s rejected the running config: %w", version, err)
	}
	return version, nil
}

// BinaryVersion runs binary with -v and returns the release it reports.
func BinaryVersion(ctx context.Context, binary string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, "-v").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("run %q -v (output=%q): %w", binary, strings.TrimSpace(string(out)), err)
	}
	version := versionPattern.FindString(string(out))
	if version == "" {
		return "", errors.New("the binary does not report a mihomo version (output=" + strings.TrimSpace(string(out)) + ")")
	}
	return version, nil
}
