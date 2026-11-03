// Package appupdate serves Hop's own releases to mygo's updater through the
// HTTP client Hop gives it, so a check and a download reach GitHub through
// Hop's own core instead of directly. mygo still checks every archive it
// receives against the public key the app was built with.
package appupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Repository is the GitHub repository Hop's releases are published in, the
// `updates.github` of mygo.json.
const Repository = "marukolibro/Hop"

// userAgent is ASCII on purpose: GitHub's release store answers 400 to a
// User-Agent with other bytes for every file its CDN has not cached yet.
const userAgent = "hop-updater"

// ErrNoRelease is returned by ManifestURL for a platform Hop publishes no
// release for.
var ErrNoRelease = errors.New("no Hop release is published for this platform")

// ErrPrereleases is returned by Source.Manifest when asked for prereleases:
// Hop publishes none and never offers them.
var ErrPrereleases = errors.New("hop offers no prereleases")

// ManifestURL is the manifest of the latest release for goos and goarch, as
// `mygo build` names it for the platforms the release workflow builds: one
// universal app on macOS, and amd64 on Windows.
func ManifestURL(goos string, goarch string) (string, error) {
	var target string
	switch {
	case goos == "darwin":
		target = "darwin-universal"
	case goos == "windows" && goarch == "amd64":
		target = "windows-amd64"
	default:
		return "", fmt.Errorf("%w (os=%s arch=%s)", ErrNoRelease, goos, goarch)
	}
	return "https://github.com/" + Repository + "/releases/latest/download/update-" + target + ".json", nil
}

// Source is a mygo.UpdateSource that fetches the manifest and the files it
// names with its client.
type Source struct {
	client   *http.Client
	manifest string
}

// NewSource returns a source that reads the manifest at manifestURL and
// everything else with client.
func NewSource(client *http.Client, manifestURL string) *Source {
	return &Source{client: client, manifest: manifestURL}
}

// Manifest returns the manifest of the latest release.
func (s *Source) Manifest(ctx context.Context, prereleases bool) (io.ReadCloser, error) {
	if prereleases {
		return nil, ErrPrereleases
	}
	return s.get(ctx, s.manifest)
}

// Open returns a file the manifest names: the archive or one of its deltas.
func (s *Source) Open(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	return s.get(ctx, rawURL)
}

// get fetches rawURL over HTTPS. Its errors name the URL without its query:
// GitHub redirects downloads to a store whose URLs carry a signed token.
func (s *Source) get(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse update URL: %w", err)
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("refusing update URL %s: updates need HTTPS", redact(u))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", redact(u), err)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := s.client.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return nil, fmt.Errorf("GET %s: %w", redact(u), err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", redact(u), resp.Status)
	}
	return resp.Body, nil
}

// redact is u without its query and fragment.
func redact(u *url.URL) string {
	clean := *u
	clean.RawQuery, clean.Fragment, clean.User = "", "", nil
	return clean.String()
}
