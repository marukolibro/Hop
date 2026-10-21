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

	"go.yaml.in/yaml/v3"
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

// Validate reports whether the core accepts config, with the validator's own
// home directory. The command is bounded by ctx; URLs and query strings in its
// output are masked before the tail is taken, a failed cleanup of the scratch
// file is joined into the error, and nothing is silently skipped over.
func (v *ConfigValidator) Validate(ctx context.Context, config []byte) error {
	return v.validateIn(ctx, config, v.dir)
}

// ValidateWithGeodata reports whether the core accepts config with the geo
// databases in dir instead of the validator's own. It is how a downloaded
// GeoSite.dat or geoip.metadb is checked before it replaces what a running
// core reads: the config test loads them exactly as the core would, with
// downloads blocked so a bad file fails the check instead of being replaced.
func (v *ConfigValidator) ValidateWithGeodata(ctx context.Context, config []byte, dir string) error {
	config, err := withoutGeodataDownloads(config)
	if err != nil {
		return err
	}
	return v.validateIn(ctx, config, dir)
}

// validateIn runs the core's config test with dir as its home.
func (v *ConfigValidator) validateIn(ctx context.Context, config []byte, dir string) (err error) {
	if err := os.MkdirAll(dir, homeDirPerm); err != nil {
		return fmt.Errorf("create config test dir (path=%q): %w", dir, err)
	}
	check, err := os.CreateTemp(dir, "check-*.yaml")
	if err != nil {
		return fmt.Errorf("create config test file (dir=%q): %w", dir, err)
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
	cmd := exec.CommandContext(testCtx, v.binary, "-t", "-d", dir, "-f", path)
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

// withoutGeodataDownloads points the check config's geox-url at a closed
// local port. Parsing a GEOSITE or GEOIP rule with a missing or invalid
// database otherwise makes the core download the default URLs and silently
// succeed, replacing the very file the check is meant to test — through the
// system network, which Hop never uses for its own downloads. With the URLs
// unreachable the check fails, as it should.
func withoutGeodataDownloads(config []byte) ([]byte, error) {
	var doc map[string]yaml.Node
	if err := yaml.Unmarshal(config, &doc); err != nil {
		return nil, fmt.Errorf("parse the config to check (bytes=%d): %w", len(config), err)
	}
	if doc == nil {
		return nil, errors.New("the config to check is empty")
	}
	unreachable := "http://127.0.0.1:1/"
	var urls yaml.Node
	if err := urls.Encode(map[string]string{
		"geoip":   unreachable + "geoip.dat",
		"mmdb":    unreachable + "geoip.metadb",
		"asn":     unreachable + "GeoLite2-ASN.mmdb",
		"geosite": unreachable + "geosite.dat",
	}); err != nil {
		return nil, fmt.Errorf("encode the blocked geodata URLs: %w", err)
	}
	doc["geox-url"] = urls
	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encode the config to check: %w", err)
	}
	return out, nil
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
