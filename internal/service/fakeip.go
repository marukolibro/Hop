package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// Prefixes a fake-IP exception may start with, besides a plain name: "*."
// matches the subdomains, "+." the name and its subdomains.
const (
	fakeIPSubdomains = "*."
	fakeIPSuffix     = "+."
)

// FakeIPExceptionStore keeps the names the user keeps out of fake-ip between
// starts.
type FakeIPExceptionStore interface {
	LoadFakeIPExceptions() ([]string, error)
	SaveFakeIPExceptions(names []string) error
}

// FakeIPExceptionsState is the list of exceptions every config built from now
// on carries under TUN, shared like LANState: the exceptions service sets it,
// and the services that build configs read it, so no reload drops it.
type FakeIPExceptionsState struct {
	mu    sync.Mutex
	names []string
}

// NewFakeIPExceptionsState returns a state with no exceptions.
func NewFakeIPExceptionsState() *FakeIPExceptionsState { return &FakeIPExceptionsState{} }

// Get returns a copy of the exceptions now.
func (s *FakeIPExceptionsState) Get() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.names)
}

// Set makes names the exceptions of the configs built from now on.
func (s *FakeIPExceptionsState) Set(names []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.names = slices.Clone(names)
}

// StartupFakeIPExceptions loads the saved exceptions into state before any
// config is built. A saved list that is not valid is an error, not replaced.
func StartupFakeIPExceptions(store FakeIPExceptionStore, state *FakeIPExceptionsState) error {
	saved, err := store.LoadFakeIPExceptions()
	if err != nil {
		return err
	}
	names, err := NormalizeFakeIPExceptions(saved)
	if err != nil {
		return fmt.Errorf("saved fake-ip exceptions: %w", err)
	}
	state.Set(names)
	return nil
}

// NormalizeFakeIPExceptions checks every name and returns them in the form the
// config writes: trimmed, lower case, and each once. A name is a domain, with
// "*." or "+." before it when its subdomains are meant too.
func NormalizeFakeIPExceptions(names []string) ([]string, error) {
	normalized := make([]string, 0, len(names))
	for i, name := range names {
		next := strings.ToLower(strings.TrimSpace(name))
		if err := validateFakeIPException(next); err != nil {
			return nil, fmt.Errorf("fake-ip exception %d (%q): %w", i+1, name, err)
		}
		if !slices.Contains(normalized, next) {
			normalized = append(normalized, next)
		}
	}
	return normalized, nil
}

// validateFakeIPException checks one trimmed, lower-case name.
func validateFakeIPException(name string) error {
	if name == "" {
		return errors.New("the name is empty")
	}
	domain := strings.TrimPrefix(strings.TrimPrefix(name, fakeIPSubdomains), fakeIPSuffix)
	if !strings.Contains(domain, ".") {
		return errors.New("a name needs at least two labels, such as example.com")
	}
	return validateDomain(domain)
}

// FakeIPExceptionsService keeps the names the user asks the TUN resolver to
// answer with their real addresses, and makes the running core use a changed
// list.
type FakeIPExceptionsService struct {
	state   *FakeIPExceptionsState
	store   FakeIPExceptionStore
	runtime RuntimeReloader
	tun     *TunFlag
	// mu serializes changes, so two never reload at once.
	mu sync.Mutex
}

// NewFakeIPExceptionsService returns a service keeping the list in store and
// state. A change reaches a running core through runtime, but only while tun
// is on: the exceptions mean nothing to the plain resolver.
func NewFakeIPExceptionsService(state *FakeIPExceptionsState, store FakeIPExceptionStore, runtime RuntimeReloader, tun *TunFlag) *FakeIPExceptionsService {
	return &FakeIPExceptionsService{state: state, store: store, runtime: runtime, tun: tun}
}

// List returns the exceptions now.
func (s *FakeIPExceptionsService) List() []string { return s.state.Get() }

// Save checks names, keeps them and applies a rebuilt config to the running
// core. The list is checked before anything is written; a config the core
// rejects puts the previous list back, so a failed change leaves the list the
// user had.
func (s *FakeIPExceptionsService) Save(ctx context.Context, names []string) error {
	normalized, err := NormalizeFakeIPExceptions(names)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.state.Get()
	if err := s.store.SaveFakeIPExceptions(normalized); err != nil {
		return err
	}
	s.state.Set(normalized)
	if !s.tun.On() {
		return nil
	}
	if err := s.runtime.ReloadRuntime(ctx); err != nil {
		s.state.Set(previous)
		if undoErr := s.store.SaveFakeIPExceptions(previous); undoErr != nil {
			return errors.Join(err, fmt.Errorf("put the previous fake-ip exceptions back in the store: %w", undoErr))
		}
		return fmt.Errorf("the core did not accept the fake-ip exceptions: %w", err)
	}
	return nil
}
