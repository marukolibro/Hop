package helper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"hop/internal/model"
)

// APIError is a helper answer that was not a success.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("helper returned status %d: %s", e.Status, e.Message)
}

// Client talks to the helper over its unix socket: one HTTP request per
// call, each bounded by its own timeout.
type Client struct {
	socket  string
	http    *http.Client
	timeout time.Duration
}

// NewClient returns a client for the helper listening on socket.
func NewClient(socket string, timeout time.Duration) *Client {
	return &Client{
		socket: socket,
		http: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _ string, _ string) (net.Conn, error) {
					var dialer net.Dialer
					return dialer.DialContext(ctx, "unix", socket)
				},
			},
		},
		timeout: timeout,
	}
}

// Version returns the helper's version, which also says it is answering.
func (c *Client) Version(ctx context.Context) (string, error) {
	var body VersionResponse
	if err := c.call(ctx, http.MethodGet, "/version", nil, &body); err != nil {
		return "", err
	}
	return body.Version, nil
}

// Status reports whether the helper's core runs, its pid and its last
// supervised restart.
func (c *Client) Status(ctx context.Context) (Status, error) {
	var body Status
	if err := c.call(ctx, http.MethodGet, "/status", nil, &body); err != nil {
		return Status{}, err
	}
	return body, nil
}

// InstallCore has the helper install the core in archive: the helper checks
// the archive against the digest GitHub publishes for its release, asking
// GitHub through proxy (Hop's own loopback proxy, "" to go direct), and
// unpacks and installs the core only when they match.
func (c *Client) InstallCore(ctx context.Context, archive model.CoreArchive, proxy string) error {
	return c.call(ctx, http.MethodPost, "/core/install", InstallRequest{Archive: archive.Path, Tag: archive.Tag, Proxy: proxy}, nil)
}

// InstallGeodata replaces the geodata databases in the helper's home with
// those in geodataDir and, when its core runs, restarts it on them: the core
// reads the databases once, at start.
func (c *Client) InstallGeodata(ctx context.Context, geodataDir string) error {
	return c.call(ctx, http.MethodPost, "/geodata/install", GeodataInstallRequest{Path: geodataDir}, nil)
}

// StartCore makes the helper write config as its core's config and start the
// core on it, returning once the core's controller answers.
func (c *Client) StartCore(ctx context.Context, config []byte) error {
	return c.call(ctx, http.MethodPost, "/core/start", StartRequest{Config: string(config)}, nil)
}

// ApplyConfig makes the helper's running core switch to config: the helper
// writes it as the core's own config file and has the core reload it,
// putting the previous file back when the core refuses it.
func (c *Client) ApplyConfig(ctx context.Context, config []byte) error {
	return c.call(ctx, http.MethodPost, "/core/apply", ApplyRequest{Config: string(config)}, nil)
}

// StopCore stops the helper's core and waits for it to exit.
func (c *Client) StopCore(ctx context.Context) error {
	return c.call(ctx, http.MethodPost, "/core/stop", nil, nil)
}

// call sends one request and decodes its answer into out, which is nil for
// the endpoints that answer nothing.
func (c *Client) call(ctx context.Context, method string, path string, body any, out any) error {
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode helper request (%s %s): %w", method, path, err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(callCtx, method, "http://helper"+path, reader)
	if err != nil {
		return fmt.Errorf("build helper request (%s %s): %w", method, path, err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call the helper (%s %s): %w", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read the helper's answer (%s %s): %w", method, path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return &APIError{Status: resp.StatusCode, Message: errorMessage(data)}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode the helper's answer (%s %s): %w", method, path, err)
	}
	return nil
}

// errorMessage pulls the message out of an error body, falling back to the
// body itself when it does not decode.
func errorMessage(data []byte) string {
	var body ErrorResponse
	if err := json.Unmarshal(data, &body); err == nil && body.Error != "" {
		return body.Error
	}
	return string(data)
}
