// Package geodata downloads the geo databases the core's rules load.
package geodata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the MetaCubeX meta-rules-dat release both databases
	// come from. Its "latest" tag is re-published as the lists change, and
	// every asset has a .sha256sum beside it, which Download requires.
	DefaultBaseURL = "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/"

	attempts   = 3
	retryDelay = time.Second
	// maxDatabaseBytes bounds one download: the real files are a few
	// megabytes, and a bad response must not fill the disk.
	maxDatabaseBytes = 64 << 20
	downloadPerm     = 0o600
	downloadDirPerm  = 0o700
)

// sourceFile is one release asset and the name the core looks for.
type sourceFile struct {
	remote string
	local  string
}

// sourceFiles are the databases Hop's generated rules need: GeoSite.dat
// serves GEOSITE rules and geoip.metadb GEOIP ones, as geodata.go seeds
// them. The release names the first one in lower case; the core's own name
// is kept on disk.
var sourceFiles = []sourceFile{
	{remote: "geosite.dat", local: "GeoSite.dat"},
	{remote: "geoip.metadb", local: "geoip.metadb"},
}

// Source downloads the databases from meta-rules-dat through client, which
// should carry no overall timeout: a download takes as long as the
// connection needs. baseURL is the release's download prefix.
type Source struct {
	client *http.Client
	base   string
}

// NewSource returns a source reading baseURL through client.
func NewSource(client *http.Client, baseURL string) *Source {
	return &Source{client: client, base: baseURL}
}

// Download fetches both databases into dir under their local names, each
// checked against the release's published SHA-256 before it is written.
// Every step retries with a warning and returns the last error with the URL
// and status; nothing unverified is left in dir.
func (s *Source) Download(ctx context.Context, dir string) error {
	if err := os.MkdirAll(dir, downloadDirPerm); err != nil {
		return fmt.Errorf("create geodata download dir (path=%q): %w", dir, err)
	}
	for _, file := range sourceFiles {
		if err := withRetries(ctx, "download "+file.remote, func() error {
			return s.downloadOnce(ctx, file, dir)
		}); err != nil {
			return err
		}
	}
	return nil
}

// downloadOnce fetches one asset and its checksum, verifies the digest, and
// writes the file atomically.
func (s *Source) downloadOnce(ctx context.Context, file sourceFile, dir string) error {
	sumURL := s.base + file.remote + ".sha256sum"
	sumBody, err := s.fetch(ctx, sumURL)
	if err != nil {
		return err
	}
	want, err := parseChecksum(sumBody, file.remote)
	if err != nil {
		return fmt.Errorf("read the published checksum (url=%q): %w", sumURL, err)
	}
	dataURL := s.base + file.remote
	data, err := s.fetch(ctx, dataURL)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(data)
	if got := hex.EncodeToString(digest[:]); got != want {
		return fmt.Errorf("sha256 of %s is %s, the release published %s (url=%q)", file.remote, got, want, dataURL)
	}
	if err := writeFile(filepath.Join(dir, file.local), data); err != nil {
		return err
	}
	slog.Info("downloaded geodata", "file", file.remote, "bytes", len(data), "sha256", want)
	return nil
}

// fetch reads one release file, bounded by maxDatabaseBytes.
func (s *Source) fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build geodata request (url=%q): %w", url, err)
	}
	req.Header.Set("User-Agent", "Hop")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download geodata (url=%q): %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("geodata request returned status %d (url=%q body=%q)", resp.StatusCode, url, strings.TrimSpace(string(body)))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDatabaseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read geodata response (url=%q status=%d): %w", url, resp.StatusCode, err)
	}
	if len(data) > maxDatabaseBytes {
		return nil, fmt.Errorf("geodata is larger than %d bytes (url=%q)", maxDatabaseBytes, url)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("geodata response is empty (url=%q)", url)
	}
	return data, nil
}

// parseChecksum reads the single line of a .sha256sum asset: the digest
// first, and the file name it belongs to after it.
func parseChecksum(body []byte, name string) (string, error) {
	fields := strings.Fields(string(body))
	if len(fields) == 0 {
		return "", fmt.Errorf("the checksum file is empty")
	}
	digest := strings.ToLower(fields[0])
	if len(digest) != sha256.Size*2 {
		return "", fmt.Errorf("the checksum %q is not a SHA-256 digest", fields[0])
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return "", fmt.Errorf("the checksum %q is not hexadecimal: %w", fields[0], err)
	}
	if len(fields) > 1 && strings.TrimPrefix(fields[1], "*") != name {
		return "", fmt.Errorf("the checksum is for %q, not %q", fields[1], name)
	}
	return digest, nil
}

// writeFile writes data through a temporary file in the same directory, so a
// failed write never leaves half a database behind.
func writeFile(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("stage geodata (path=%q): %w", path, err)
	}
	name := tmp.Name()
	defer func() {
		if err == nil {
			return
		}
		if removeErr := os.Remove(name); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove staged geodata (path=%q): %w", name, removeErr))
		}
	}()
	if _, writeErr := tmp.Write(data); writeErr != nil {
		closeErr := tmp.Close()
		return errors.Join(fmt.Errorf("write staged geodata (path=%q): %w", name, writeErr), closeErr)
	}
	if err := tmp.Chmod(downloadPerm); err != nil {
		return errors.Join(fmt.Errorf("set staged geodata's mode (path=%q): %w", name, err), tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close staged geodata (path=%q): %w", name, err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("put geodata in place (from=%q to=%q): %w", name, path, err)
	}
	return nil
}

// withRetries runs fn up to attempts times, warning on each failure, and
// returns the last error.
func withRetries(ctx context.Context, what string, fn func() error) error {
	var last error
	for attempt := 1; attempt <= attempts; attempt++ {
		if last = fn(); last == nil {
			return nil
		}
		if ctx.Err() != nil {
			return last
		}
		slog.Warn("will retry", "what", what, "attempt", attempt, "max_attempts", attempts, "error", last)
		if attempt == attempts {
			break
		}
		select {
		case <-ctx.Done():
			return last
		case <-time.After(retryDelay):
		}
	}
	return fmt.Errorf("%s failed after %d attempts: %w", what, attempts, last)
}
