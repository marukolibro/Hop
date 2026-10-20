package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"

	"hop/internal/model"
)

// ProxySetter reads and sets the system proxy.
type ProxySetter interface {
	Current(ctx context.Context) (model.SystemProxyState, error)
	Set(ctx context.Context, host string, port int) error
	Clear(ctx context.Context) error
}

// ErrSystemProxyTaken is returned when another app holds the system proxy:
// Hop never overrides it.
var ErrSystemProxyTaken = errors.New("another app holds the system proxy")

// SystemProxyService points the system proxy at Hop and back. It is the only
// place Hop changes the system proxy: never over another app's, and when
// clearing, only Hop's own.
type SystemProxyService struct {
	proxy ProxySetter
	host  string
	port  int
}

// NewSystemProxyService returns a service pointing the system proxy at
// Hop's mixed port, host:port.
func NewSystemProxyService(proxy ProxySetter, host string, port int) *SystemProxyService {
	return &SystemProxyService{proxy: proxy, host: host, port: port}
}

// Status says who holds the system proxy now.
func (s *SystemProxyService) Status(ctx context.Context) (model.SystemProxyStatus, error) {
	state, err := s.proxy.Current(ctx)
	if err != nil {
		return model.SystemProxyStatus{}, err
	}
	return ProxyStatusOf(state, s.host, s.port), nil
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
	if err := s.proxy.Set(ctx, s.host, s.port); err != nil {
		return fmt.Errorf("point the system proxy at Hop (%s): %w", net.JoinHostPort(s.host, strconv.Itoa(s.port)), err)
	}
	slog.Info("system proxy points at hop", "host", s.host, "port", s.port)
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
