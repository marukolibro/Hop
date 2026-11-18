package helper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
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
	// coreMu protects the callback and the last observed core identity.
	coreMu       sync.Mutex
	onCoreChange func(bool, bool)
	// coreKnown and restartKnown say the last Status left a baseline to compare
	// against. A core whose pid the helper could not read reports 0, which
	// leaves the pid baseline as it was: unknown is not a replacement.
	coreKnown    bool
	corePid      int
	restartKnown bool
	coreRestart  model.CoreRestart
}

// OnCoreChange registers the controller cache invalidator before use. It runs
// after core mutations and when Status observes a supervised core replacement.
// confirmed distinguishes replies from transport failures; replaced is true
// only for an acknowledged process change or an observed restart.
func (c *Client) OnCoreChange(fn func(confirmed, replaced bool)) {
	c.coreMu.Lock()
	defer c.coreMu.Unlock()
	c.onCoreChange = fn
}

func (c *Client) coreChanged(err error, replaced bool) {
	c.coreMu.Lock()
	fn := c.onCoreChange
	c.coreMu.Unlock()
	if fn != nil {
		var apiErr *APIError
		fn(err == nil || errors.As(err, &apiErr), err == nil && replaced)
	}
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
// supervised restart. A replacement it observes against the previous reply
// tells OnCoreChange.
func (c *Client) Status(ctx context.Context) (Status, error) {
	var body Status
	if err := c.call(ctx, http.MethodGet, "/status", nil, &body); err != nil {
		return Status{}, err
	}
	c.coreMu.Lock()
	restart := model.CoreRestart{}
	if body.LastRestart != nil {
		restart = *body.LastRestart
	}
	// A pid the helper reports against the one seen before, or a restart against
	// the last one reported, is a replacement. The first Status only takes the
	// baseline, and a pid of 0 (the helper could not read the core's pid file)
	// stays unknown.
	moved := c.coreKnown && body.Pid != 0 && c.corePid != body.Pid
	restarted := c.restartKnown && restart != c.coreRestart
	changed := moved || restarted
	if body.Pid != 0 {
		c.coreKnown, c.corePid = true, body.Pid
	}
	c.restartKnown, c.coreRestart = true, restart
	fn := c.onCoreChange
	c.coreMu.Unlock()
	if changed && fn != nil {
		fn(true, true)
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
	err := c.call(ctx, http.MethodPost, "/geodata/install", GeodataInstallRequest{Path: geodataDir}, nil)
	c.coreChanged(err, true)
	return err
}

// StartCore makes the helper write config as its core's config and start the
// core on it, returning once the core's controller answers.
func (c *Client) StartCore(ctx context.Context, config []byte) error {
	err := c.call(ctx, http.MethodPost, "/core/start", StartRequest{Config: string(config)}, nil)
	c.coreChanged(err, true)
	return err
}

// ApplyConfig makes the helper's running core switch to config: the helper
// writes it as the core's own config file and has the core reload it,
// putting the previous file back when the core refuses it.
func (c *Client) ApplyConfig(ctx context.Context, config []byte) error {
	err := c.call(ctx, http.MethodPost, "/core/apply", ApplyRequest{Config: string(config)}, nil)
	c.coreChanged(err, false)
	return err
}

// StopCore stops the helper's core and waits for it to exit.
func (c *Client) StopCore(ctx context.Context) error {
	err := c.call(ctx, http.MethodPost, "/core/stop", nil, nil)
	c.coreChanged(err, true)
	return err
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
