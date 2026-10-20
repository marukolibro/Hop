package mihomo

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const (
	// checkFilePerm is the permission of the config written for a check,
	// matching the core's own config file.
	checkFilePerm = 0o600

	// maxValidationOutput bounds how much of the core's test log an error
	// carries.
	maxValidationOutput = 2048
)

// queryPattern matches a URL's query, where providers carry tokens: whatever it
// matches in the core's output is a credential.
var queryPattern = regexp.MustCompile(`\?[^\s"']+`)

// ConfigValidator checks a runtime config with the core's own config test (-t).
// The test runs in an isolated directory: the running core is never swapped and
// no port is bound; the directory is kept between checks, so geodata the configs
// need is downloaded once. Remote rule-providers are not fetched, exactly as the
// running core treats them at load time.
type ConfigValidator struct {
	binary  string
	dir     string
	timeout time.Duration
}

// NewConfigValidator returns a validator that tests configs by running binary
// with its home in dir, each test bounded by timeout.
func NewConfigValidator(binary string, dir string, timeout time.Duration) *ConfigValidator {
	return &ConfigValidator{binary: binary, dir: dir, timeout: timeout}
}

// Validate reports whether the core accepts config. The command is bounded by
// ctx; URLs and query strings in its output are masked before the tail is taken,
// a failed cleanup of the scratch file is joined into the error, and nothing is
// silently skipped over.
func (v *ConfigValidator) Validate(ctx context.Context, config []byte) (err error) {
	if err := os.MkdirAll(v.dir, homeDirPerm); err != nil {
		return fmt.Errorf("create config test dir (path=%q): %w", v.dir, err)
	}
	check, err := os.CreateTemp(v.dir, "check-*.yaml")
	if err != nil {
		return fmt.Errorf("create config test file (dir=%q): %w", v.dir, err)
	}
	path := check.Name()
	if err := check.Close(); err != nil {
		return fmt.Errorf("close config test file (path=%q): %w", path, err)
	}
	// The scratch config carries the nodes' credentials; a leftover must be
	// reported.
	defer func() {
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove config test file (path=%q): %w", path, removeErr))
		}
	}()
	if err := os.WriteFile(path, config, checkFilePerm); err != nil {
		return fmt.Errorf("write config test file (path=%q): %w", path, err)
	}

	testCtx, cancel := context.WithTimeout(ctx, v.timeout)
	defer cancel()
	cmd := exec.CommandContext(testCtx, v.binary, "-t", "-d", v.dir, "-f", path)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](err); !ok {
		return fmt.Errorf("run config test (binary=%q): %w", v.binary, err)
	} else if testCtx.Err() != nil {
		return fmt.Errorf("config test did not finish in %s (status=%d): %w", v.timeout, exitErr.ExitCode(), testCtx.Err())
	}
	return fmt.Errorf("the core rejected the config: %s", redactTestOutput(output))
}

// redactTestOutput masks full URLs (a path can be a credential too) and bare
// query strings before taking the log's tail — truncating first could cut off
// the '?' and let a token through.
func redactTestOutput(output []byte) string {
	text := maskQueries(redactURLs(string(output)))
	if text == "" {
		return "(no output)"
	}
	runes := []rune(text)
	if len(runes) > maxValidationOutput {
		runes = runes[len(runes)-maxValidationOutput:]
	}
	return strings.TrimSpace(string(runes))
}

// redactURLs replaces every http(s) link in text with its scheme and host only:
// both the path and the query can carry credentials.
func redactURLs(text string) string {
	return urlPattern.ReplaceAllStringFunc(text, func(match string) string {
		u, err := url.Parse(match)
		if err != nil || u.Host == "" {
			return match
		}
		return u.Scheme + "://" + u.Host + "***"
	})
}

// urlPattern matches an http(s) link in text.
var urlPattern = regexp.MustCompile(`https?://[^\s"'` + "`" + `]+`)

// maskQueries masks query strings that appear without a scheme prefix.
func maskQueries(text string) string {
	return queryPattern.ReplaceAllString(text, "?***")
}
