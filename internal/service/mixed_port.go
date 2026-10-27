package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// MixedPortMin and MixedPortMax bound the proxy port the user may choose:
	// below 1024 a port needs privileges, and 65535 is the last one.
	MixedPortMin = 1024
	MixedPortMax = 65535

	// listenAttempts and listenInterval bound the wait for the core to listen
	// on a new port after it accepted the config.
	listenAttempts = 8
	listenInterval = 250 * time.Millisecond
)

// MixedPortState is the proxy port every part of Hop uses now, shared like
// TunFlag: the port service sets it, and everything that builds a config,
// points an app at the proxy or reaches the internet through it reads it, so
// no reload, refresh or TUN switch puts an old port back.
type MixedPortState struct{ port atomic.Int32 }

// NewMixedPortState returns a state holding port.
func NewMixedPortState(port int) *MixedPortState {
	s := &MixedPortState{}
	s.Set(port)
	return s
}

// Get returns the proxy port now.
func (s *MixedPortState) Get() int { return int(s.port.Load()) }

// Set makes port the proxy port of everything that reads the state.
func (s *MixedPortState) Set(port int) { s.port.Store(int32(port)) }

// MixedPortStore keeps the port the user chose between starts.
type MixedPortStore interface {
	SaveMixedPort(port int) error
	LoadMixedPort() (int, bool, error)
}

// MixedPortProbe checks a port against the machine.
type MixedPortProbe interface {
	// Free says whether port can be listened on now.
	Free(port int) error
	// Listening says whether something accepts connections on port.
	Listening(ctx context.Context, port int) error
}

// MixedPortRuntime has the core run the config Hop would start it with now.
type MixedPortRuntime interface {
	ApplyCurrent(ctx context.Context) error
}

// MixedPortProxy moves a system proxy that points at Hop along with its port.
type MixedPortProxy interface {
	Follow(ctx context.Context, previous int) error
}

// MixedPortRunner tells the core runner which port the core it starts will
// listen on, so it waits for that one to be free.
type MixedPortRunner interface {
	SetMixedPort(port int)
}

// ValidateMixedPort says whether port is one the user may give the proxy:
// inside the allowed range and not one of the ports Hop's own listeners hold.
func ValidateMixedPort(port int, reserved []int) error {
	if port < MixedPortMin || port > MixedPortMax {
		return fmt.Errorf("proxy port %d is outside %d-%d", port, MixedPortMin, MixedPortMax)
	}
	if slices.Contains(reserved, port) {
		return fmt.Errorf("proxy port %d is used by another part of Hop", port)
	}
	return nil
}

// StartupMixedPort is the proxy port to start with: the one the user chose,
// or instancePort before any was chosen. A saved port that is not allowed is
// an error, not replaced. A saved port another program holds is not known
// here: only a core that fails to start on it says so (see FallBack).
func StartupMixedPort(store MixedPortStore, instancePort int, reserved []int) (int, error) {
	port, found, err := store.LoadMixedPort()
	if err != nil {
		return 0, err
	}
	if !found {
		return instancePort, nil
	}
	if err := ValidateMixedPort(port, reserved); err != nil {
		return 0, fmt.Errorf("saved proxy port: %w", err)
	}
	return port, nil
}

// MixedPortService changes the proxy port while Hop runs: the core listens on
// the new port at once, the system proxy follows it, and the choice is kept
// for the next start.
type MixedPortService struct {
	state    *MixedPortState
	store    MixedPortStore
	probe    MixedPortProbe
	runtime  MixedPortRuntime
	proxy    MixedPortProxy
	runner   MixedPortRunner
	reserved []int
	// skipped is the port the user chose that could not be used at start, 0
	// when there is none; the settings row says so.
	skipped atomic.Int32
	// mu serializes changes, so two never move the core at once.
	mu sync.Mutex
}

// NewMixedPortService returns a service changing state's port. reserved are
// the ports Hop's other listeners hold, which the proxy may not take.
func NewMixedPortService(state *MixedPortState, store MixedPortStore, probe MixedPortProbe, runtime MixedPortRuntime, proxy MixedPortProxy, runner MixedPortRunner, reserved []int) *MixedPortService {
	return &MixedPortService{state: state, store: store, probe: probe, runtime: runtime, proxy: proxy, runner: runner, reserved: reserved}
}

// Current is the proxy port now.
func (s *MixedPortService) Current() int { return s.state.Get() }

// Skipped is the port the user chose that Hop could not use at start, or 0.
// The choice stays saved, so the next start tries it again.
func (s *MixedPortService) Skipped() int { return int(s.skipped.Load()) }

// FallBack makes port the proxy port for this run because another program
// holds the one the user chose. It is for a start only, before the core runs
// again on a config built from the new port; the choice stays saved.
func (s *MixedPortService) FallBack(port int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.skipped.Store(int32(s.state.Get()))
	s.setPort(port)
}

// Change makes next the proxy port. Anything that fails leaves the port, the
// core and the system proxy as they were; when putting them back fails too,
// both errors are returned.
//
// The order keeps a system proxy pointing at a live port: the core listens on
// the new port first, the system proxy follows, and only then is the choice
// kept. The window in which a crash leaves the choice behind the system proxy
// is the one file write.
func (s *MixedPortService) Change(ctx context.Context, next int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ValidateMixedPort(next, s.reserved); err != nil {
		return err
	}
	previous := s.state.Get()
	if next == previous {
		return nil
	}
	if err := s.probe.Free(next); err != nil {
		return fmt.Errorf("proxy port %d is taken by another program: %w", next, err)
	}
	if err := s.moveCore(ctx, previous, next); err != nil {
		return err
	}
	if err := s.proxy.Follow(ctx, previous); err != nil {
		return s.revert(ctx, previous, next, fmt.Errorf("move the system proxy to port %d: %w", next, err))
	}
	if err := s.store.SaveMixedPort(next); err != nil {
		return s.revert(ctx, previous, next, fmt.Errorf("keep proxy port %d for the next start: %w", next, err))
	}
	s.skipped.Store(0)
	return nil
}

// moveCore makes the core listen on to instead of from: the port is set for
// every config built from now on, the core reloads, and the new port must
// answer. A reload that does not take, or a port nobody listens on, puts from
// back.
func (s *MixedPortService) moveCore(ctx context.Context, from int, to int) error {
	s.setPort(to)
	if err := s.runtime.ApplyCurrent(ctx); err != nil {
		s.setPort(from)
		return fmt.Errorf("the core did not accept proxy port %d: %w", to, err)
	}
	// The core can accept a config and still fail to bind a listener, so the
	// reload's success does not say the port works.
	if err := s.waitListening(ctx, to); err != nil {
		s.setPort(from)
		cause := fmt.Errorf("the core does not listen on proxy port %d: %w", to, err)
		if restoreErr := s.runtime.ApplyCurrent(ctx); restoreErr != nil {
			return errors.Join(cause, fmt.Errorf("put proxy port %d back: %w", from, restoreErr))
		}
		return cause
	}
	return nil
}

// revert puts the core and the system proxy back on previous after a later
// step failed, and returns cause with anything that could not be put back.
func (s *MixedPortService) revert(ctx context.Context, previous int, next int, cause error) error {
	var failures []error
	if err := s.moveCore(ctx, next, previous); err != nil {
		failures = append(failures, fmt.Errorf("put proxy port %d back: %w", previous, err))
	}
	// The port in the state is previous again: a proxy that already moved to
	// next goes back with it, and one that never moved is left alone.
	if err := s.proxy.Follow(ctx, next); err != nil {
		failures = append(failures, fmt.Errorf("put the system proxy back on port %d: %w", previous, err))
	}
	return errors.Join(append([]error{cause}, failures...)...)
}

// setPort is the proxy port of every config and check from now on.
func (s *MixedPortService) setPort(port int) {
	s.state.Set(port)
	s.runner.SetMixedPort(port)
}

// waitListening waits for port to accept a connection. Each failed attempt is
// warned about; the last error is returned.
func (s *MixedPortService) waitListening(ctx context.Context, port int) error {
	var last error
	for attempt := 1; attempt <= listenAttempts; attempt++ {
		if last = s.probe.Listening(ctx, port); last == nil {
			return nil
		}
		slog.Warn("the core does not listen on the new proxy port yet", "port", port, "attempt", attempt, "max_attempts", listenAttempts, "error", last)
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for port %d: %w", port, ctx.Err())
		case <-time.After(listenInterval):
		}
	}
	return fmt.Errorf("nothing listened on port %d after %d attempts: %w", port, listenAttempts, last)
}
