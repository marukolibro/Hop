// Package mihomo talks to a mihomo core: its REST controller and the
// process that hosts it.
package mihomo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"hop/internal/model"
)

const (
	globalGroup = "GLOBAL"
	maxBodySize = 4 << 20
	// reloadTimeout bounds a config reload, which the core answers only
	// after it has loaded every provider the config names. One it has no
	// copy of yet it downloads then, direct, giving each 20 s; the proxy
	// providers load before the rule providers, each kind all at once on
	// 64-bit builds, so a first reload can take some 40 s. It stays under
	// the app's helper call timeout, so a reload the helper runs fails with
	// the helper's own error, not the app's.
	reloadTimeout = 50 * time.Second
)

// APIError is a controller response that was not a success.
type APIError struct {
	Method string
	Path   string
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("mihomo controller %s %s returned status %d: %s", e.Method, e.Path, e.Status, e.Body)
}

// Client is a connector to the REST controller of one mihomo core.
type Client struct {
	addr       string
	secret     string
	configPath string
	http       *http.Client
	// timeout bounds each request but a reload (see reloadTimeout); a delay
	// test adds its own timeout to it.
	timeout time.Duration
	// streams are the WebSocket streams open now, which CloseStreams ends.
	streamsMu sync.Mutex
	streams   map[*websocket.Conn]struct{}
}

// NewClient returns a client for the controller listening on addr
// (host:port), authenticating with secret. configPath is the config file the
// core was started with, which ApplyConfig rewrites. timeout bounds each
// request, so httpClient should carry none of its own.
func NewClient(addr string, secret string, configPath string, httpClient *http.Client, timeout time.Duration) *Client {
	return &Client{addr: addr, secret: secret, configPath: configPath, http: httpClient, timeout: timeout, streams: map[*websocket.Conn]struct{}{}}
}

// CloseIdleConnections closes the keep-alive connections to the controller.
// Closed from this side before the core stops, they leave their TIME_WAIT on
// this side's ephemeral ports; closed by a core that stops first, they would
// leave it on the controller's port, which macOS then keeps from another
// user's core for half a minute.
func (c *Client) CloseIdleConnections() {
	c.http.CloseIdleConnections()
}

// CloseStreams ends every stream open now from this side, at once: each
// stream's reader returns an error. A core about to stop is then not the
// side that closes them, which on macOS would keep the controller's port
// from a core run by another user for TIME_WAIT, 30 s.
func (c *Client) CloseStreams() {
	c.streamsMu.Lock()
	defer c.streamsMu.Unlock()
	for conn := range c.streams {
		_ = conn.CloseNow()
	}
}

type versionResponse struct {
	Version string `json:"version"`
}

type delayResponse struct {
	Delay int `json:"delay"`
}

type proxiesResponse struct {
	Proxies map[string]proxyEntry `json:"proxies"`
}

type proxyEntry struct {
	Type    string       `json:"type"`
	Now     string       `json:"now"`
	Fixed   string       `json:"fixed"`
	All     []string     `json:"all"`
	History []delayEntry `json:"history"`
	UDP     bool         `json:"udp"`
}

type delayEntry struct {
	Delay int `json:"delay"`
}

type ruleEntry struct {
	Index   int        `json:"index"`
	Type    string     `json:"type"`
	Payload string     `json:"payload"`
	Proxy   string     `json:"proxy"`
	Size    int        `json:"size"`
	Extra   *ruleExtra `json:"extra"`
}

type ruleExtra struct {
	Disabled bool   `json:"disabled"`
	HitCount uint64 `json:"hitCount"`
}

type proxyProvidersResponse struct {
	Providers map[string]proxyProviderEntry `json:"providers"`
}

type proxyProviderEntry struct {
	Name        string            `json:"name"`
	VehicleType string            `json:"vehicleType"`
	Proxies     []json.RawMessage `json:"proxies"`
	UpdatedAt   time.Time         `json:"updatedAt"`
}

type ruleProvidersResponse struct {
	Providers map[string]ruleProviderEntry `json:"providers"`
}

type ruleProviderEntry struct {
	Name        string    `json:"name"`
	VehicleType string    `json:"vehicleType"`
	Behavior    string    `json:"behavior"`
	RuleCount   int       `json:"ruleCount"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// builtinTypes are the core's own outbounds, which are no proxy.
var builtinTypes = []string{"Direct", "Reject", "RejectDrop", "Compatible", "Pass"}

// groupTypes are the proxy types that are policy groups.
var groupTypes = []string{model.GroupSelector, model.GroupURLTest, model.GroupFallback, model.GroupLoadBalance, model.GroupRelay}

type trafficMessage struct {
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

type reloadRequest struct {
	Path string `json:"path"`
}

type selectRequest struct {
	Name string `json:"name"`
}

// Version returns the core's version string. It is the cheapest request to
// tell whether the controller is up.
func (c *Client) Version(ctx context.Context) (string, error) {
	raw, err := c.do(ctx, c.timeout, http.MethodGet, "/version", nil, http.StatusOK)
	if err != nil {
		return "", err
	}
	var out versionResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("decode mihomo version response (body=%q): %w", raw, err)
	}
	return out.Version, nil
}

// Snapshot returns the policy groups in GLOBAL's order and each proxy's
// latest delay. A custom GLOBAL may omit groups: those follow by name.
func (c *Client) Snapshot(ctx context.Context) (model.Snapshot, error) {
	raw, err := c.do(ctx, c.timeout, http.MethodGet, "/proxies", nil, http.StatusOK)
	if err != nil {
		return model.Snapshot{}, err
	}
	var out proxiesResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return model.Snapshot{}, fmt.Errorf("decode mihomo proxies response (body=%q): %w", raw, err)
	}
	global, ok := out.Proxies[globalGroup]
	if !ok {
		return model.Snapshot{}, fmt.Errorf("mihomo proxies response has no %s group", globalGroup)
	}
	// The core-generated GLOBAL lists every proxy and group in profile order.
	// A profile can supply its own GLOBAL with only some of those options.
	groups := make([]model.ProxyGroup, 0, len(global.All))
	names := slices.Clone(global.All)
	seen := map[string]bool{globalGroup: true}
	for _, name := range names {
		seen[name] = true
	}
	var missing []string
	for name, entry := range out.Proxies {
		if !seen[name] && slices.Contains(groupTypes, entry.Type) {
			missing = append(missing, name)
		}
	}
	slices.Sort(missing)
	for _, name := range append(names, missing...) {
		entry := out.Proxies[name]
		if name == globalGroup || !slices.Contains(groupTypes, entry.Type) {
			continue
		}
		groups = append(groups, model.ProxyGroup{Name: name, Type: entry.Type, Selected: entry.Now, Fixed: entry.Fixed, Options: entry.All})
	}
	delays := make(map[string]int, len(out.Proxies))
	facts := make(map[string]model.ProxyFacts, len(out.Proxies))
	for name, entry := range out.Proxies {
		if len(entry.History) > 0 {
			delays[name] = entry.History[len(entry.History)-1].Delay
		}
		if !slices.Contains(groupTypes, entry.Type) && !slices.Contains(builtinTypes, entry.Type) {
			facts[name] = model.ProxyFacts{Protocol: entry.Type, UDP: entry.UDP}
		}
	}
	globalGroupView := model.ProxyGroup{Name: globalGroup, Type: global.Type, Selected: global.Now, Options: slices.Clone(global.All)}
	return model.Snapshot{Groups: groups, Global: globalGroupView, Delays: delays, Facts: facts}, nil
}

// CatchAll returns the target of the first Match rule, where the core sends
// what no other rule matched, or "" when the rules have none. It reads the
// rules as a stream and stops at that rule: a profile's rules can be far
// larger in the controller's JSON than maxBodySize.
func (c *Client) CatchAll(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.send(ctx, http.MethodGet, "/rules", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", apiError(http.MethodGet, "/rules", resp)
	}
	target, err := readCatchAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("decode mihomo response (method=GET path=\"/rules\"): %w", err)
	}
	return target, nil
}

// readCatchAll decodes the rules of a /rules reply one at a time up to the
// first Match rule and returns its target.
func readCatchAll(body io.Reader) (string, error) {
	decoder := json.NewDecoder(body)
	token, err := decoder.Token()
	if err != nil {
		return "", err
	}
	if token != json.Delim('{') {
		return "", errors.New("mihomo rules response is not an object")
	}
	found := false
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return "", err
		}
		if key != "rules" {
			var ignored json.RawMessage
			if err := decoder.Decode(&ignored); err != nil {
				return "", err
			}
			continue
		}
		token, err := decoder.Token()
		if err != nil {
			return "", err
		}
		if token != json.Delim('[') {
			return "", errors.New("mihomo rules response has no rules array")
		}
		found = true
		for decoder.More() {
			var rule ruleEntry
			if err := decoder.Decode(&rule); err != nil {
				return "", err
			}
			if strings.EqualFold(rule.Type, "Match") {
				if rule.Proxy == "" {
					return "", errors.New("mihomo Match rule has no target")
				}
				return rule.Proxy, nil
			}
		}
		if _, err := decoder.Token(); err != nil {
			return "", err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("mihomo rules response has no rules array")
	}
	return "", nil
}

// Rules returns all active routing rules from the core, streamed from /rules
// so large rule sets never hit maxBodySize.
func (c *Client) Rules(ctx context.Context) ([]model.ActiveRule, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.send(ctx, http.MethodGet, "/rules", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, apiError(http.MethodGet, "/rules", resp)
	}
	rules, err := readAllRules(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("decode mihomo rules (method=GET path=\"/rules\"): %w", err)
	}
	return rules, nil
}

func readAllRules(body io.Reader) ([]model.ActiveRule, error) {
	decoder := json.NewDecoder(body)
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if token != json.Delim('{') {
		return nil, errors.New("mihomo rules response is not an object")
	}
	var out []model.ActiveRule
	found := false
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		if key != "rules" {
			var ignored json.RawMessage
			if err := decoder.Decode(&ignored); err != nil {
				return nil, err
			}
			continue
		}
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		if token != json.Delim('[') {
			return nil, errors.New("mihomo rules response has no rules array")
		}
		found = true
		idx := 0
		for decoder.More() {
			var entry ruleEntry
			if err := decoder.Decode(&entry); err != nil {
				return nil, err
			}
			r := model.ActiveRule{
				Index:   idx,
				Type:    entry.Type,
				Payload: entry.Payload,
				Proxy:   entry.Proxy,
				Size:    entry.Size,
			}
			if entry.Extra != nil {
				r.Disabled = entry.Extra.Disabled
				r.HitCount = entry.Extra.HitCount
			}
			out = append(out, r)
			idx++
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("mihomo rules response has no rules array")
	}
	return out, nil
}

// ProxyProviders lists external (HTTP or File) proxy providers loaded by the
// core, sorted by name. Built-in Compatible/Inline providers are omitted.
func (c *Client) ProxyProviders(ctx context.Context) ([]model.ProviderSummary, error) {
	raw, err := c.do(ctx, c.timeout, http.MethodGet, "/providers/proxies", nil, http.StatusOK)
	if err != nil {
		return nil, err
	}
	var resp proxyProvidersResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode mihomo proxy providers: %w", err)
	}
	var out []model.ProviderSummary
	for name, entry := range resp.Providers {
		if entry.VehicleType != "HTTP" && entry.VehicleType != "File" {
			continue
		}
		n := entry.Name
		if n == "" {
			n = name
		}
		out = append(out, model.ProviderSummary{
			Kind:        model.ProviderKindProxy,
			Name:        n,
			VehicleType: entry.VehicleType,
			Count:       len(entry.Proxies),
			UpdatedAt:   entry.UpdatedAt,
		})
	}
	slices.SortFunc(out, func(a, b model.ProviderSummary) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// RuleProviders lists external (HTTP or File) rule providers loaded by the
// core, sorted by name.
func (c *Client) RuleProviders(ctx context.Context) ([]model.ProviderSummary, error) {
	raw, err := c.do(ctx, c.timeout, http.MethodGet, "/providers/rules", nil, http.StatusOK)
	if err != nil {
		return nil, err
	}
	var resp ruleProvidersResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode mihomo rule providers: %w", err)
	}
	var out []model.ProviderSummary
	for name, entry := range resp.Providers {
		if entry.VehicleType != "HTTP" && entry.VehicleType != "File" {
			continue
		}
		n := entry.Name
		if n == "" {
			n = name
		}
		out = append(out, model.ProviderSummary{
			Kind:        model.ProviderKindRule,
			Name:        n,
			VehicleType: entry.VehicleType,
			Behavior:    entry.Behavior,
			Count:       entry.RuleCount,
			UpdatedAt:   entry.UpdatedAt,
		})
	}
	slices.SortFunc(out, func(a, b model.ProviderSummary) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// UpdateProxyProvider asks the core to refresh one proxy provider.
func (c *Client) UpdateProxyProvider(ctx context.Context, name string) error {
	path := "/providers/proxies/" + url.PathEscape(name)
	_, err := c.do(ctx, reloadTimeout, http.MethodPut, path, nil, http.StatusNoContent)
	return err
}

// UpdateRuleProvider asks the core to refresh one rule provider.
func (c *Client) UpdateRuleProvider(ctx context.Context, name string) error {
	path := "/providers/rules/" + url.PathEscape(name)
	_, err := c.do(ctx, reloadTimeout, http.MethodPut, path, nil, http.StatusNoContent)
	return err
}

// TestProxyDelay measures one proxy against testURL and returns its delay in
// milliseconds. A proxy that failed the test or did not answer within timeout
// returns 0: that is the answer of the test, not an error. The core keeps the
// result, so later snapshots carry it.
func (c *Client) TestProxyDelay(ctx context.Context, proxy string, testURL string, timeout time.Duration) (int, error) {
	query := url.Values{}
	query.Set("url", testURL)
	query.Set("timeout", strconv.FormatInt(timeout.Milliseconds(), 10))
	path := "/proxies/" + url.PathEscape(proxy) + "/delay?" + query.Encode()
	// The core answers once the proxy has replied or timed out.
	raw, err := c.do(ctx, c.timeout+timeout, http.MethodGet, path, nil, http.StatusOK)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && (apiErr.Status == http.StatusServiceUnavailable || apiErr.Status == http.StatusGatewayTimeout) {
			return 0, nil
		}
		return 0, err
	}
	var out delayResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return 0, fmt.Errorf("decode mihomo delay response (proxy=%q body=%q): %w", proxy, raw, err)
	}
	return out.Delay, nil
}

// SelectedProxy returns the proxy group now uses.
func (c *Client) SelectedProxy(ctx context.Context, group string) (string, error) {
	raw, err := c.do(ctx, c.timeout, http.MethodGet, "/proxies/"+url.PathEscape(group), nil, http.StatusOK)
	if err != nil {
		return "", err
	}
	var out proxyEntry
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("decode mihomo proxy response (group=%q body=%q): %w", group, raw, err)
	}
	return out.Now, nil
}

// SelectProxy makes group use proxy. On URLTest and Fallback groups, mihomo
// pins the chosen proxy until ResetProxy clears it.
func (c *Client) SelectProxy(ctx context.Context, group string, proxy string) error {
	body, err := json.Marshal(selectRequest{Name: proxy})
	if err != nil {
		return fmt.Errorf("encode select request (group=%q proxy=%q): %w", group, proxy, err)
	}
	path := "/proxies/" + url.PathEscape(group)
	_, err = c.do(ctx, c.timeout, http.MethodPut, path, body, http.StatusNoContent)
	return err
}

// ResetProxy clears a manually pinned proxy on a URLTest or Fallback group so
// it returns to automatic selection.
func (c *Client) ResetProxy(ctx context.Context, group string) error {
	path := "/proxies/" + url.PathEscape(group)
	_, err := c.do(ctx, c.timeout, http.MethodDelete, path, nil, http.StatusNoContent)
	return err
}

// FlushDNSCache clears the core's in-memory DNS cache so lookups on a newly
// joined network resolve freshly.
func (c *Client) FlushDNSCache(ctx context.Context) error {
	_, err := c.do(ctx, c.timeout, http.MethodPost, "/cache/dns/flush", nil, http.StatusNoContent)
	return err
}

// ApplyConfig replaces the config file of the core and makes it reload. The
// previous file is put back when the core refuses the new one, so the file
// on disk always matches what the core runs.
func (c *Client) ApplyConfig(ctx context.Context, config []byte) error {
	previous, err := os.ReadFile(c.configPath)
	if err != nil {
		return fmt.Errorf("read current mihomo config (path=%q): %w", c.configPath, err)
	}
	if err := os.WriteFile(c.configPath, config, configPerm); err != nil {
		return fmt.Errorf("write mihomo config (path=%q): %w", c.configPath, err)
	}
	reloadErr := c.reload(ctx)
	if reloadErr == nil {
		return nil
	}
	if restoreErr := os.WriteFile(c.configPath, previous, configPerm); restoreErr != nil {
		return fmt.Errorf("mihomo rejected the config (%w) and the previous config could not be restored: %v", reloadErr, restoreErr)
	}
	return reloadErr
}

func (c *Client) reload(ctx context.Context) error {
	body, err := json.Marshal(reloadRequest{Path: c.configPath})
	if err != nil {
		return fmt.Errorf("encode reload request (path=%q): %w", c.configPath, err)
	}
	_, err = c.do(ctx, reloadTimeout, http.MethodPut, "/configs?force=true", body, http.StatusNoContent)
	return err
}

// StreamTraffic calls handle for every traffic sample until ctx is done or
// the connection fails. It always returns a non-nil error.
func (c *Client) StreamTraffic(ctx context.Context, handle func(model.Traffic)) error {
	return c.stream(ctx, "/traffic", func(raw []byte) error {
		var msg trafficMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			return fmt.Errorf("decode mihomo traffic message (body=%q): %w", raw, err)
		}
		handle(model.Traffic{Up: msg.Up, Down: msg.Down})
		return nil
	})
}

type memoryMessage struct {
	Inuse uint64 `json:"inuse"`
}

// StreamMemory calls handle with the resident memory of the core's process,
// in bytes, about once a second until ctx is done or the connection fails.
// The core's first message always says 0, so it is skipped. It always
// returns a non-nil error.
func (c *Client) StreamMemory(ctx context.Context, handle func(uint64)) error {
	first := true
	return c.stream(ctx, "/memory", func(raw []byte) error {
		var msg memoryMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			return fmt.Errorf("decode mihomo memory message (body=%q): %w", raw, err)
		}
		if first {
			first = false
			return nil
		}
		handle(msg.Inuse)
		return nil
	})
}

// stream reads the WebSocket at path and passes every message to handle,
// until ctx is done, the connection fails or handle returns an error. It
// always returns a non-nil error.
func (c *Client) stream(ctx context.Context, path string, handle func(raw []byte) error) error {
	conn, _, err := websocket.Dial(ctx, "ws://"+c.addr+path, &websocket.DialOptions{HTTPHeader: c.authHeader()})
	if err != nil {
		return fmt.Errorf("dial mihomo stream (addr=%q path=%q): %w", c.addr, path, err)
	}
	c.streamsMu.Lock()
	c.streams[conn] = struct{}{}
	c.streamsMu.Unlock()
	defer func() {
		c.streamsMu.Lock()
		delete(c.streams, conn)
		c.streamsMu.Unlock()
		_ = conn.CloseNow()
	}()
	// A busy connection table is far larger than the library's 32 KiB default.
	conn.SetReadLimit(maxBodySize)
	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("read mihomo stream (addr=%q path=%q): %w", c.addr, path, err)
		}
		if err := handle(raw); err != nil {
			return err
		}
	}
}

func (c *Client) authHeader() http.Header {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.secret)
	return header
}

// do sends one request and returns the response body. Any status other
// than want is an *APIError.
func (c *Client) do(ctx context.Context, timeout time.Duration, method string, path string, body []byte, want int) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resp, err := c.send(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		return nil, apiError(method, path, resp)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return nil, fmt.Errorf("read mihomo response (method=%s path=%q status=%d): %w", method, path, resp.StatusCode, err)
	}
	return raw, nil
}

// send sends one request and returns the response, whose body the caller
// closes.
func (c *Client) send(ctx context.Context, method string, path string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, "http://"+c.addr+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build mihomo request (method=%s path=%q): %w", method, path, err)
	}
	req.Header = c.authHeader()
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send mihomo request (method=%s path=%q): %w", method, path, err)
	}
	return resp, nil
}

// apiError is the *APIError of a response with an unexpected status.
func apiError(method string, path string, resp *http.Response) error {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return fmt.Errorf("read mihomo response (method=%s path=%q status=%d): %w", method, path, resp.StatusCode, err)
	}
	return &APIError{Method: method, Path: path, Status: resp.StatusCode, Body: strings.TrimSpace(string(raw))}
}
