// Package unlock asks a dozen services whether they work through the route
// the proxy it was given takes: Hop's mixed port, where the rules pick it.
package unlock

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"sync"
	"time"

	"hop/internal/model"
)

const (
	// attemptTimeout bounds each request of a check.
	attemptTimeout = 10 * time.Second

	// userAgent is a current browser's: these sites answer a bare Go client
	// differently.
	userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15"
)

// ConnectionLister is the core's connection table, where a request's route is
// read while it is open.
type ConnectionLister interface {
	Connections(ctx context.Context) (model.Connections, error)
}

// Client checks the services through one proxy.
type Client struct {
	http        *http.Client
	connections ConnectionLister
}

// NewClient returns a client sending through the proxy proxy names for each
// request, reading routes from connections. Keep-alives are off, so every
// request has its own connection, which the core lists under that request's
// local port.
func NewClient(proxy func(*http.Request) (*url.URL, error), connections ConnectionLister) *Client {
	transport := &http.Transport{Proxy: proxy, DisableKeepAlives: true}
	return &Client{http: &http.Client{Transport: transport}, connections: connections}
}

// Check runs every service's check at once and hands each result to report
// as it comes, one at a time. A service whose check failed is reported as
// far as it got, UnlockUnknown when it got nowhere, and the returned error
// says which and why.
func (c *Client) Check(ctx context.Context, report func(model.UnlockResult)) error {
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for _, service := range model.UnlockServices {
		if _, known := checks[service]; !known {
			return fmt.Errorf("no check for service %q", service)
		}
	}
	for _, service := range model.UnlockServices {
		run := checks[service]
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := run(ctx, c)
			result.Service = service
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Errorf("check %s: %w", service, err))
			}
			report(result)
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

// reply is what a request found: its status, the first bytes of its body,
// the address it ended at after redirects, and, for the first request of a
// check, how long it took and the route it took.
type reply struct {
	status  int
	body    []byte
	final   *url.URL
	latency time.Duration
	route   []string
}

// probe fetches address as the first request of a check: it also times the
// reply and reads the route the core gave the request while its connection
// is still open. A route the core no longer lists is left empty.
func (c *Client) probe(ctx context.Context, address string, limit int64) (reply, error) {
	ctx, cancel := context.WithTimeout(ctx, attemptTimeout)
	defer cancel()
	var local string
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) {
		local = info.Conn.LocalAddr().String()
	}})
	start := time.Now()
	resp, err := c.send(ctx, address)
	if err != nil {
		return reply{}, err
	}
	defer resp.Body.Close()
	latency := time.Since(start)
	route, err := c.route(ctx, local)
	if err != nil {
		return reply{}, err
	}
	body, err := readBody(resp, address, limit)
	if err != nil {
		return reply{}, err
	}
	return reply{status: resp.StatusCode, body: body, final: resp.Request.URL, latency: latency, route: route}, nil
}

// get fetches address as a later request of a check.
func (c *Client) get(ctx context.Context, address string, limit int64) (reply, error) {
	ctx, cancel := context.WithTimeout(ctx, attemptTimeout)
	defer cancel()
	resp, err := c.send(ctx, address)
	if err != nil {
		return reply{}, err
	}
	defer resp.Body.Close()
	body, err := readBody(resp, address, limit)
	if err != nil {
		return reply{}, err
	}
	return reply{status: resp.StatusCode, body: body, final: resp.Request.URL}, nil
}

// send makes the GET a check sends, as a browser would.
func (c *Client) send(ctx context.Context, address string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", address, err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "en")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", address, err)
	}
	return resp, nil
}

// readBody reads the first limit bytes of resp's body, none when limit is 0.
func readBody(resp *http.Response, address string, limit int64) ([]byte, error) {
	if limit == 0 {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, fmt.Errorf("GET %s: read reply (status %d): %w", address, resp.StatusCode, err)
	}
	return body, nil
}

// route finds the open connection that came from local, the request's own
// end of its connection to the proxy, and returns its chain.
func (c *Client) route(ctx context.Context, local string) ([]string, error) {
	_, port, err := net.SplitHostPort(local)
	if err != nil {
		return nil, fmt.Errorf("read the request's local port (addr=%q): %w", local, err)
	}
	if _, err := strconv.Atoi(port); err != nil {
		return nil, fmt.Errorf("read the request's local port (addr=%q): %w", local, err)
	}
	conns, err := c.connections.Connections(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the route of the request from port %s: %w", port, err)
	}
	for _, conn := range conns.Active {
		if conn.SourcePort == port {
			return conn.Chain, nil
		}
	}
	return nil, nil
}
