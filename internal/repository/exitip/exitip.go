// Package exitip asks Cloudflare's trace endpoint which public address
// traffic leaves by, through Hop's own loopback proxy.
package exitip

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
	"time"

	"hop/internal/model"
)

const (
	// TraceURL is Cloudflare's plain key=value trace, which reports the
	// caller's public address and the country it is registered in.
	TraceURL = "https://1.1.1.1/cdn-cgi/trace"
	// attemptTimeout bounds one request; a lookup makes up to attempts.
	attemptTimeout = 8 * time.Second
	attempts       = 2
	// maxTrace bounds what is read from the reply.
	maxTrace = 4 << 10
)

// ipv4Literal finds dotted addresses in a failed reply, so a short body
// prefix never carries the user's address.
var ipv4Literal = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

// Client reads the exit IP through the HTTP client it was given, which main
// points at Hop's loopback proxy.
type Client struct {
	endpoint string
	http     *http.Client
}

// NewClient returns a client for endpoint, sending through httpClient.
func NewClient(endpoint string, httpClient *http.Client) *Client {
	return &Client{endpoint: endpoint, http: httpClient}
}

// Lookup reads the exit IP, retrying once with a warning. The returned error
// never contains the address: only the endpoint, status and a redacted body
// prefix.
func (c *Client) Lookup(ctx context.Context) (model.ExitIP, error) {
	var last error
	for attempt := 1; attempt <= attempts; attempt++ {
		ip, err := c.lookup(ctx)
		if err == nil {
			return ip, nil
		}
		last = err
		if ctx.Err() != nil {
			break
		}
		if attempt < attempts {
			slog.Warn("exit IP lookup failed; retrying", "attempt", attempt, "error", err)
		}
	}
	return model.ExitIP{}, last
}

func (c *Client) lookup(ctx context.Context) (model.ExitIP, error) {
	ctx, cancel := context.WithTimeout(ctx, attemptTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return model.ExitIP{}, fmt.Errorf("exit IP lookup: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return model.ExitIP{}, fmt.Errorf("exit IP lookup: GET %s: %w", c.endpoint, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTrace+1))
	if err != nil {
		return model.ExitIP{}, fmt.Errorf("exit IP lookup: GET %s: read reply: %w", c.endpoint, err)
	}
	if len(body) > maxTrace {
		return model.ExitIP{}, fmt.Errorf("exit IP lookup: GET %s: reply over %d bytes", c.endpoint, maxTrace)
	}
	if resp.StatusCode != http.StatusOK {
		message := fmt.Sprintf("exit IP lookup: GET %s: %s", c.endpoint, resp.Status)
		if prefix := redactedPrefix(body); prefix != "" {
			message += ": " + prefix
		}
		return model.ExitIP{}, errors.New(message)
	}
	ip, err := ParseTrace(body)
	if err != nil {
		return model.ExitIP{}, fmt.Errorf("exit IP lookup: GET %s: %w", c.endpoint, err)
	}
	return ip, nil
}

// ParseTrace reads the ip and loc of Cloudflare's trace. The address is
// validated with net/netip and the country is kept as an upper-case ISO 3166
// alpha-2 code; a reply missing either is refused.
func ParseTrace(body []byte) (model.ExitIP, error) {
	var address netip.Addr
	var country string
	// Lines hold one key=value each; a condensed reply puts them on one
	// line, so fields are read too. Values we use never contain spaces.
	for _, line := range strings.Split(string(body), "\n") {
		for _, field := range strings.Fields(line) {
			key, value, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			switch key {
			case "ip":
				parsed, err := netip.ParseAddr(value)
				if err != nil {
					return model.ExitIP{}, errors.New("exit IP trace has no valid ip")
				}
				address = parsed.Unmap()
			case "loc":
				code := strings.ToUpper(value)
				if len(code) != 2 || code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z' {
					return model.ExitIP{}, errors.New("exit IP trace has no valid country")
				}
				country = code
			}
		}
	}
	if !address.IsValid() {
		return model.ExitIP{}, errors.New("exit IP trace has no ip")
	}
	if country == "" {
		return model.ExitIP{}, errors.New("exit IP trace has no country")
	}
	return model.ExitIP{Address: address, Country: country}, nil
}

// redactedPrefix is a short one-line prefix of a failed reply with any
// address taken out, so the error stays useful without carrying the user's
// public IP.
func redactedPrefix(body []byte) string {
	s := strings.Join(strings.Fields(string(body)), " ")
	if s == "" {
		return ""
	}
	fields := strings.Fields(s)
	for i, field := range fields {
		if strings.HasPrefix(field, "ip=") {
			fields[i] = "ip=[redacted]"
			continue
		}
		token := strings.Trim(field, `.,;:()[]{}"'`)
		if _, err := netip.ParseAddr(token); err == nil {
			fields[i] = "[redacted]"
		}
	}
	s = ipv4Literal.ReplaceAllString(strings.Join(fields, " "), "[redacted]")
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}
