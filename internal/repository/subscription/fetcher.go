// Package subscription downloads proxy subscriptions from providers.
package subscription

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"hop/internal/model"
)

const (
	// Providers often pick the profile format from the user agent; naming
	// the core makes them return Clash YAML.
	userAgent = "clash.meta Hop/0.1"

	// userInfoHeader carries the account's traffic and expiry.
	userInfoHeader = "Subscription-Userinfo"

	maxSize       = 16 << 20
	maxErrorBody  = 256
	fetchAttempts = 3
	retryDelay    = 2 * time.Second
)

// StatusError is a provider response that was not a success.
type StatusError struct {
	Target string
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("subscription provider %s returned status %d: %s", e.Target, e.Status, e.Body)
}

// Fetcher is a connector to subscription providers.
type Fetcher struct {
	http *http.Client
}

// NewFetcher returns a fetcher that sends its requests with httpClient.
func NewFetcher(httpClient *http.Client) *Fetcher {
	return &Fetcher{http: httpClient}
}

// Fetch downloads the profile at rawURL, with the provider's usage header. A failed attempt is retried with a
// warning; the last error is returned when every attempt fails. Errors name
// the provider's host and path but never its query, which carries the
// account's token.
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (model.Download, error) {
	target, err := Redact(rawURL)
	if err != nil {
		return model.Download{}, err
	}
	var last error
	for attempt := 1; attempt <= fetchAttempts; attempt++ {
		download, err := f.fetchOnce(ctx, rawURL, target)
		if err == nil {
			return download, nil
		}
		last = err
		if !retryable(err) {
			return model.Download{}, err
		}
		slog.Warn("subscription download failed", "target", target, "attempt", attempt, "max_attempts", fetchAttempts, "error", err)
		select {
		case <-ctx.Done():
			return model.Download{}, fmt.Errorf("download subscription %s: %w", target, ctx.Err())
		case <-time.After(retryDelay):
		}
	}
	return model.Download{}, last
}

// Redact returns rawURL as scheme://host/path, without credentials or query.
func Redact(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("subscription link is not a valid URL: %w", stripURL(err))
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("subscription link must start with http:// or https:// and name a host")
	}
	return u.Scheme + "://" + u.Host + u.Path, nil
}

// stripURL drops the URL, query included, that net/url and net/http echo
// in their errors, keeping the underlying cause.
func stripURL(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

func (f *Fetcher) fetchOnce(ctx context.Context, rawURL string, target string) (model.Download, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return model.Download{}, fmt.Errorf("build subscription request (target=%s): %w", target, stripURL(err))
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := f.http.Do(req)
	if err != nil {
		return model.Download{}, fmt.Errorf("download subscription (target=%s): %w", target, stripURL(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, readErr := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		if readErr != nil {
			return model.Download{}, fmt.Errorf("read subscription error response (target=%s status=%d): %w", target, resp.StatusCode, readErr)
		}
		return model.Download{}, &StatusError{Target: target, Status: resp.StatusCode, Body: strings.TrimSpace(string(snippet))}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		return model.Download{}, fmt.Errorf("read subscription (target=%s): %w", target, err)
	}
	if len(body) > maxSize {
		return model.Download{}, fmt.Errorf("subscription from %s is larger than %d bytes", target, maxSize)
	}
	return model.Download{Body: body, UserInfo: resp.Header.Get(userInfoHeader)}, nil
}

// retryable tells transient failures from ones a retry cannot fix: a
// provider that answers 4xx will answer the same again.
func retryable(err error) bool {
	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		return true
	}
	return statusErr.Status >= 500
}
