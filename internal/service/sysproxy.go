package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"hop/internal/model"
)

// DefaultProxyBypass are the local and private addresses that always bypass
// the system proxy when Hop sets it, so localhost development servers, Bonjour
// hosts, and LAN devices stay reachable directly.
var DefaultProxyBypass = []string{
	"localhost",
	"127.0.0.1",
	"::1",
	"*.local",
	"169.254.0.0/16",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"100.64.0.0/10",
}

// ProxySetter reads and sets the system proxy.
type ProxySetter interface {
	Current(ctx context.Context) (model.SystemProxyState, error)
	Set(ctx context.Context, host string, port int, bypass []string) error
	Clear(ctx context.Context) error
}

// ProxyBypassStore keeps the user's extra system proxy bypass entries on disk.
type ProxyBypassStore interface {
	LoadProxyBypass() ([]string, error)
	SaveProxyBypass(entries []string) error
}

// ErrSystemProxyTaken is returned when another app holds the system proxy:
// Hop never overrides it.
var ErrSystemProxyTaken = errors.New("another app holds the system proxy")

// SystemProxyService points the system proxy at Hop and back. It is the only
// place Hop changes the system proxy: never over another app's, and when
// clearing, only Hop's own.
type SystemProxyService struct {
	proxy       ProxySetter
	bypassStore ProxyBypassStore
	host        string
	port        *MixedPortState
	mu          sync.Mutex
	bypass      []string
	applyMu     sync.Mutex
}

// NewSystemProxyService returns a service pointing the system proxy at
// Hop's mixed port, host:port, wherever the user has put it.
func NewSystemProxyService(proxy ProxySetter, bypassStore ProxyBypassStore, host string, port *MixedPortState) *SystemProxyService {
	return &SystemProxyService{proxy: proxy, bypassStore: bypassStore, host: host, port: port}
}

// LoadBypass loads the user's saved bypass entries before the UI starts.
func (s *SystemProxyService) LoadBypass() error {
	if s.bypassStore == nil {
		return nil
	}
	saved, err := s.bypassStore.LoadProxyBypass()
	if err != nil {
		return err
	}
	normalized, err := NormalizeProxyBypass(saved)
	if err != nil {
		return fmt.Errorf("saved system proxy bypass list: %w", err)
	}
	s.mu.Lock()
	s.bypass = normalized
	s.mu.Unlock()
	return nil
}

// BypassList returns a copy of the user's extra bypass entries.
func (s *SystemProxyService) BypassList() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.bypass)
}

// effectiveBypass returns the built-in local bypass entries followed by any
// extra entries the user added, without duplicates.
func (s *SystemProxyService) effectiveBypass() []string {
	extra := s.BypassList()
	out := make([]string, 0, len(DefaultProxyBypass)+len(extra))
	out = append(out, DefaultProxyBypass...)
	for _, entry := range extra {
		if !slices.Contains(out, entry) {
			out = append(out, entry)
		}
	}
	return out
}

// SaveBypass validates and persists the user's extra bypass entries. If Hop
// currently holds the system proxy, the updated bypass list is applied to the
// OS immediately; another app's system proxy is never touched.
func (s *SystemProxyService) SaveBypass(ctx context.Context, entries []string) error {
	normalized, err := NormalizeProxyBypass(entries)
	if err != nil {
		return err
	}
	s.applyMu.Lock()
	defer s.applyMu.Unlock()
	previous := s.BypassList()
	if s.bypassStore != nil {
		if err := s.bypassStore.SaveProxyBypass(normalized); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.bypass = normalized
	s.mu.Unlock()

	status, err := s.Status(ctx)
	if err != nil {
		return err
	}
	if status.Owner != model.ProxyOwnerHop {
		return nil
	}
	port := s.port.Get()
	if err := s.proxy.Set(ctx, s.host, port, s.effectiveBypass()); err != nil {
		s.mu.Lock()
		s.bypass = previous
		s.mu.Unlock()
		if s.bypassStore != nil {
			if undoErr := s.bypassStore.SaveProxyBypass(previous); undoErr != nil {
				return errors.Join(err, fmt.Errorf("put the previous proxy bypass list back in the store: %w", undoErr))
			}
		}
		return fmt.Errorf("apply system proxy bypass list: %w", err)
	}
	slog.Info("system proxy bypass list updated", "extra_count", len(normalized))
	return nil
}

// NormalizeProxyBypass checks and canonicalizes user-entered bypass entries:
// trimmed, lower-case, deduplicated, accepting hostnames, wildcard suffixes
// ("*.example.com", ".example.com"), IPs, CIDRs, and simple IPv4 wildcards
// ("192.168.*").
func NormalizeProxyBypass(entries []string) ([]string, error) {
	out := make([]string, 0, len(entries))
	for i, raw := range entries {
		entry := strings.ToLower(strings.TrimSpace(raw))
		canonical, err := validateProxyBypassEntry(entry)
		if err != nil {
			return nil, fmt.Errorf("proxy bypass entry %d (%q): %w", i+1, raw, err)
		}
		if !slices.Contains(out, canonical) {
			out = append(out, canonical)
		}
	}
	return out, nil
}

func validateProxyBypassEntry(entry string) (string, error) {
	if entry == "" {
		return "", errors.New("the entry is empty")
	}
	if strings.ContainsAny(entry, ",;\r\n") || strings.IndexFunc(entry, unicode.IsSpace) >= 0 {
		return "", errors.New("an entry cannot contain spaces, commas or semicolons")
	}
	if entry == "localhost" {
		return entry, nil
	}
	if addr, err := netip.ParseAddr(entry); err == nil {
		return addr.String(), nil
	}
	if prefix, err := netip.ParsePrefix(entry); err == nil {
		return prefix.Masked().String(), nil
	}
	if isIPv4Wildcard(entry) {
		return entry, nil
	}
	domain := entry
	if rest, ok := strings.CutPrefix(entry, "*."); ok {
		domain = rest
	} else if rest, ok := strings.CutPrefix(entry, "."); ok {
		domain = rest
	}
	if err := validateDomain(domain); err != nil {
		return "", err
	}
	return entry, nil
}

func isIPv4Wildcard(entry string) bool {
	if !strings.Contains(entry, "*") {
		return false
	}
	parts := strings.Split(entry, ".")
	if len(parts) < 2 || len(parts) > 4 {
		return false
	}
	for i, part := range parts {
		if part == "*" {
			// Allow '*' only at the end or trailing octets.
			for j := i + 1; j < len(parts); j++ {
				if parts[j] != "*" {
					return false
				}
			}
			return true
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || n > 255 {
			return false
		}
	}
	return false
}

// Status says who holds the system proxy now.
func (s *SystemProxyService) Status(ctx context.Context) (model.SystemProxyStatus, error) {
	state, err := s.proxy.Current(ctx)
	if err != nil {
		return model.SystemProxyStatus{}, err
	}
	return ProxyStatusOf(state, s.host, s.port.Get()), nil
}

// Enable points the system proxy at Hop, unless another app holds it.
func (s *SystemProxyService) Enable(ctx context.Context) error {
	status, err := s.Status(ctx)
	if err != nil {
		return err
	}
	if status.Owner == model.ProxyOwnerOther {
		return fmt.Errorf("%w (points at %s)", ErrSystemProxyTaken, status.Other)
	}
	port := s.port.Get()
	if err := s.proxy.Set(ctx, s.host, port, s.effectiveBypass()); err != nil {
		return fmt.Errorf("point the system proxy at Hop (%s): %w", net.JoinHostPort(s.host, strconv.Itoa(port)), err)
	}
	slog.Info("system proxy points at hop", "host", s.host, "port", port)
	return nil
}

// Follow moves a system proxy that pointed at Hop on port previous to the
// port Hop has now. Another app's proxy, none, or one already elsewhere is
// left as it is: only Hop's own is ever changed.
func (s *SystemProxyService) Follow(ctx context.Context, previous int) error {
	state, err := s.proxy.Current(ctx)
	if err != nil {
		return err
	}
	if ProxyStatusOf(state, s.host, previous).Owner != model.ProxyOwnerHop {
		return nil
	}
	port := s.port.Get()
	if err := s.proxy.Set(ctx, s.host, port, s.effectiveBypass()); err != nil {
		return fmt.Errorf("move the system proxy from port %d to %d: %w", previous, port, err)
	}
	slog.Info("system proxy follows hop to its new port", "host", s.host, "from", previous, "to", port)
	return nil
}

// Disable clears the system proxy when it points at Hop. Another app's, or
// none, is left as it is.
func (s *SystemProxyService) Disable(ctx context.Context) error {
	status, err := s.Status(ctx)
	if err != nil {
		return err
	}
	if status.Owner != model.ProxyOwnerHop {
		slog.Info("system proxy is not hop's, left as it is", "owner", string(status.Owner), "points_at", status.Other)
		return nil
	}
	if err := s.proxy.Clear(ctx); err != nil {
		return fmt.Errorf("clear the system proxy: %w", err)
	}
	slog.Info("system proxy cleared")
	return nil
}

// ProxyStatusOf says who holds the system proxy in state: no one when
// nothing is set, Hop when every proxy set points at host:port, and another
// app as soon as one proxy or a PAC script points elsewhere.
func ProxyStatusOf(state model.SystemProxyState, host string, port int) model.SystemProxyStatus {
	if state.AutoConfig {
		return model.SystemProxyStatus{Owner: model.ProxyOwnerOther, Other: "PAC"}
	}
	owner := model.ProxyOwnerNone
	for _, e := range []model.ProxyEndpoint{state.HTTP, state.HTTPS, state.SOCKS} {
		if !e.Enabled {
			continue
		}
		if !isLoopbackName(e.Host, host) || e.Port != port {
			return model.SystemProxyStatus{Owner: model.ProxyOwnerOther, Other: net.JoinHostPort(e.Host, strconv.Itoa(e.Port))}
		}
		owner = model.ProxyOwnerHop
	}
	return model.SystemProxyStatus{Owner: owner}
}

// isLoopbackName says whether name is host, or "localhost" for a loopback
// host.
func isLoopbackName(name string, host string) bool {
	if name == host {
		return true
	}
	ip := net.ParseIP(host)
	return name == "localhost" && ip != nil && ip.IsLoopback()
}
