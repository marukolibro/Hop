// Package corerelease finds and downloads mihomo releases from GitHub.
package corerelease

import (
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"hop/internal/model"
)

const (
	// ReleasesAPI is where GitHub answers about mihomo's releases: the
	// newest at /latest, one by its tag at /tags/<tag>.
	ReleasesAPI = "https://api.github.com/repos/MetaCubeX/mihomo/releases"

	attempts      = 3
	retryDelay    = time.Second
	maxAPIBody    = 4 << 20
	maxBinarySize = 256 << 20
	// MaxArchiveSize bounds a release archive: a real one is far smaller.
	MaxArchiveSize = 256 << 20
	progressEvery  = 100 * time.Millisecond
	dirPerm        = 0o700
	binaryPerm     = 0o755
	digestPrefix   = "sha256:"
)

// tagPattern is what a release tag must look like: a plain version, which
// keeps a tampered tag from reaching a URL or leaving a directory.
var tagPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// ErrInvalidTag is returned for a tag that is not a plain release version.
var ErrInvalidTag = errors.New("core release tag is not a plain version like v1.19.33")

// CheckTag returns ErrInvalidTag for a tag that is not a plain release
// version.
func CheckTag(tag string) error {
	if !tagPattern.MatchString(tag) {
		return fmt.Errorf("%w (tag=%q)", ErrInvalidTag, tag)
	}
	return nil
}

// Older reports whether tag is an earlier release than floor. Both must be
// plain versions.
func Older(tag string, floor string) (bool, error) {
	t, err := tagNumbers(tag)
	if err != nil {
		return false, err
	}
	f, err := tagNumbers(floor)
	if err != nil {
		return false, err
	}
	for i := range t {
		if t[i] != f[i] {
			return t[i] < f[i], nil
		}
	}
	return false, nil
}

func tagNumbers(tag string) ([3]int, error) {
	var out [3]int
	if err := CheckTag(tag); err != nil {
		return out, err
	}
	for i, part := range strings.Split(strings.TrimPrefix(tag, "v"), ".") {
		n, err := strconv.Atoi(part)
		if err != nil {
			return out, fmt.Errorf("%w (tag=%q): %w", ErrInvalidTag, tag, err)
		}
		out[i] = n
	}
	return out, nil
}

// Source is a connector to the place mihomo releases are published.
type Source struct {
	client *http.Client
	apiURL string
}

// NewSource returns a source that asks apiURL (ReleasesAPI, or a server
// that answers the same paths) about releases and downloads through client,
// which should carry no overall timeout: the download of a core takes as
// long as the connection needs.
func NewSource(client *http.Client, apiURL string) *Source {
	return &Source{client: client, apiURL: apiURL}
}

type releaseResponse struct {
	TagName string          `json:"tag_name"`
	Assets  []assetResponse `json:"assets"`
}

type assetResponse struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
	URL    string `json:"browser_download_url"`
}

// assetName is the release asset that holds the core for this platform. The
// Intel Mac and Windows builds are the ones for CPUs without AVX2, as
// scripts/fetch-mihomo.sh ships.
func assetName(goos string, goarch string, tag string) (string, error) {
	switch goos + "/" + goarch {
	case "darwin/arm64":
		return "mihomo-darwin-arm64-" + tag + ".gz", nil
	case "darwin/amd64":
		return "mihomo-darwin-amd64-compatible-" + tag + ".gz", nil
	case "windows/amd64":
		return "mihomo-windows-amd64-compatible-" + tag + ".zip", nil
	}
	return "", fmt.Errorf("no mihomo build is offered for %s/%s", goos, goarch)
}

// Latest returns the newest release and the asset of this platform. A
// release whose asset carries no digest is refused: Hop installs nothing it
// cannot verify.
func (s *Source) Latest(ctx context.Context) (model.CoreRelease, error) {
	var release model.CoreRelease
	err := withRetries(ctx, "ask for the newest mihomo release", func() error {
		var err error
		release, err = s.fetchRelease(ctx, s.apiURL+"/latest")
		return err
	})
	return release, err
}

// Release returns the release of tag and the asset of this platform, with
// the digest GitHub publishes for it.
func (s *Source) Release(ctx context.Context, tag string) (model.CoreRelease, error) {
	if err := CheckTag(tag); err != nil {
		return model.CoreRelease{}, err
	}
	var release model.CoreRelease
	err := withRetries(ctx, "ask for a mihomo release", func() error {
		var err error
		release, err = s.fetchRelease(ctx, s.apiURL+"/tags/"+tag)
		return err
	})
	if err != nil {
		return model.CoreRelease{}, err
	}
	if release.Tag != tag {
		return model.CoreRelease{}, fmt.Errorf("asked for release %s, GitHub answered with %s", tag, release.Tag)
	}
	return release, nil
}

func (s *Source) fetchRelease(ctx context.Context, apiURL string) (model.CoreRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		return model.CoreRelease{}, fmt.Errorf("build release request (url=%q): %w", apiURL, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Hop")
	resp, err := s.client.Do(req)
	if err != nil {
		return model.CoreRelease{}, fmt.Errorf("ask for a mihomo release (url=%q): %w", apiURL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIBody))
	if err != nil {
		return model.CoreRelease{}, fmt.Errorf("read release response (url=%q status=%d): %w", apiURL, resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK {
		return model.CoreRelease{}, fmt.Errorf("release request returned status %d (url=%q body=%q)", resp.StatusCode, apiURL, truncate(body))
	}
	var out releaseResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return model.CoreRelease{}, fmt.Errorf("decode release response (url=%q body=%q): %w", apiURL, truncate(body), err)
	}
	name, err := assetName(runtime.GOOS, runtime.GOARCH, out.TagName)
	if err != nil {
		return model.CoreRelease{}, err
	}
	for _, asset := range out.Assets {
		if asset.Name != name {
			continue
		}
		digest, ok := strings.CutPrefix(asset.Digest, digestPrefix)
		if !ok || digest == "" {
			return model.CoreRelease{}, fmt.Errorf("release %s publishes no sha256 digest for %s (digest=%q)", out.TagName, name, asset.Digest)
		}
		return model.CoreRelease{Tag: out.TagName, AssetName: name, AssetURL: asset.URL, Size: asset.Size, SHA256: digest}, nil
	}
	return model.CoreRelease{}, fmt.Errorf("release %s has no asset %s", out.TagName, name)
}

// Download fetches release's asset to archivePath, checks it against the
// published digest and unpacks the core to binaryPath. The archive stays: it
// is what the privileged helper verifies on its own before it runs this
// core. progress hears the bytes received, at most every 100 ms. Nothing is
// left behind on failure.
func (s *Source) Download(ctx context.Context, release model.CoreRelease, archivePath string, binaryPath string, progress func(done int64, total int64)) (err error) {
	if err := s.DownloadArchive(ctx, release, archivePath, progress); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, removeQuietly(archivePath))
		}
	}()
	return Unpack(archivePath, release.AssetName, binaryPath)
}

// DownloadArchive fetches release's asset to archivePath and checks it
// against the published digest. Nothing is left behind on failure.
func (s *Source) DownloadArchive(ctx context.Context, release model.CoreRelease, archivePath string, progress func(done int64, total int64)) error {
	return withRetries(ctx, "download the mihomo core", func() error {
		return s.downloadOnce(ctx, release, archivePath, progress)
	})
}

func (s *Source) downloadOnce(ctx context.Context, release model.CoreRelease, archivePath string, progress func(done int64, total int64)) (err error) {
	if err := os.MkdirAll(filepath.Dir(archivePath), dirPerm); err != nil {
		return fmt.Errorf("create core dir (path=%q): %w", filepath.Dir(archivePath), err)
	}
	partial := archivePath + ".partial"
	defer func() {
		if err != nil {
			err = errors.Join(err, removeQuietly(partial))
		}
	}()
	if err := s.fetchTo(ctx, release, partial, progress); err != nil {
		return err
	}
	if err := os.Rename(partial, archivePath); err != nil {
		return fmt.Errorf("move the archive into place (from=%q to=%q): %w", partial, archivePath, err)
	}
	return nil
}

// Unpack extracts the core from the release archive of assetName to
// binaryPath, executable, through a temporary name. Nothing is left behind
// on failure.
func Unpack(archive string, assetName string, binaryPath string) (err error) {
	partial := binaryPath + ".partial"
	defer func() {
		if err != nil {
			err = errors.Join(err, removeQuietly(partial))
		}
	}()
	if err := unpack(archive, assetName, partial); err != nil {
		return err
	}
	if err := os.Chmod(partial, binaryPerm); err != nil {
		return fmt.Errorf("make the core executable (path=%q): %w", partial, err)
	}
	if err := os.Rename(partial, binaryPath); err != nil {
		return fmt.Errorf("move the core into place (from=%q to=%q): %w", partial, binaryPath, err)
	}
	return nil
}

// SHA256 returns the hex SHA-256 of the file at path, as GitHub publishes a
// release asset's digest.
func SHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file to hash (path=%q): %w", path, err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash file (path=%q): %w", path, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// fetchTo saves the asset to path and verifies its digest.
func (s *Source) fetchTo(ctx context.Context, release model.CoreRelease, path string, progress func(done int64, total int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, release.AssetURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("build download request (asset=%s): %w", release.AssetName, err)
	}
	req.Header.Set("User-Agent", "Hop")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", release.AssetName, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxAPIBody))
		return fmt.Errorf("download of %s returned status %d (body=%q)", release.AssetName, resp.StatusCode, truncate(body))
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create download file (path=%q): %w", path, err)
	}
	hash := sha256.New()
	counter := &progressWriter{total: release.Size, report: progress, last: time.Now()}
	_, copyErr := io.Copy(io.MultiWriter(file, hash, counter), resp.Body)
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("receive %s: %w", release.AssetName, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close download file (path=%q): %w", path, closeErr)
	}
	counter.finish()
	if got := hex.EncodeToString(hash.Sum(nil)); got != release.SHA256 {
		return fmt.Errorf("sha256 of %s is %s, GitHub published %s", release.AssetName, got, release.SHA256)
	}
	return nil
}

// progressWriter counts bytes and reports them at a steady pace.
type progressWriter struct {
	done   int64
	total  int64
	report func(done int64, total int64)
	last   time.Time
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	if time.Since(p.last) >= progressEvery {
		p.last = time.Now()
		p.report(p.done, p.total)
	}
	return len(b), nil
}

func (p *progressWriter) finish() { p.report(p.done, p.total) }

// unpack extracts the single executable of the archive to dest.
func unpack(archive string, assetName string, dest string) error {
	switch {
	case strings.HasSuffix(assetName, ".gz"):
		return unpackGzip(archive, dest)
	case strings.HasSuffix(assetName, ".zip"):
		return unpackZip(archive, dest)
	}
	return fmt.Errorf("unknown archive format (asset=%s)", assetName)
}

func unpackGzip(archive string, dest string) error {
	in, err := os.Open(archive)
	if err != nil {
		return fmt.Errorf("open archive (path=%q): %w", archive, err)
	}
	defer in.Close()
	reader, err := gzip.NewReader(in)
	if err != nil {
		return fmt.Errorf("read gzip archive (path=%q): %w", archive, err)
	}
	defer reader.Close()
	return writeLimited(dest, reader)
}

func unpackZip(archive string, dest string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("open zip archive (path=%q): %w", archive, err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !strings.EqualFold(filepath.Ext(f.Name), ".exe") {
			continue
		}
		in, err := f.Open()
		if err != nil {
			return fmt.Errorf("open %s in archive (path=%q): %w", f.Name, archive, err)
		}
		writeErr := writeLimited(dest, in)
		closeErr := in.Close()
		return errors.Join(writeErr, closeErr)
	}
	return fmt.Errorf("no executable inside the archive (path=%q)", archive)
}

// writeLimited writes in to path, refusing more than maxBinarySize bytes: a
// real core is far smaller, and an archive bomb must not fill the disk.
func writeLimited(path string, in io.Reader) error {
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, binaryPerm)
	if err != nil {
		return fmt.Errorf("create core file (path=%q): %w", path, err)
	}
	n, copyErr := io.Copy(out, io.LimitReader(in, maxBinarySize+1))
	closeErr := out.Close()
	if copyErr != nil {
		return fmt.Errorf("unpack core (path=%q): %w", path, copyErr)
	}
	if n > maxBinarySize {
		return fmt.Errorf("unpacked core is larger than %d bytes (path=%q)", maxBinarySize, path)
	}
	if closeErr != nil {
		return fmt.Errorf("close core file (path=%q): %w", path, closeErr)
	}
	return nil
}

func removeQuietly(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove file (path=%q): %w", path, err)
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

func truncate(body []byte) string {
	const limit = 200
	if len(body) > limit {
		return string(body[:limit])
	}
	return string(body)
}
