package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"sync"

	"hop/internal/model"
)

// LANSettings is what a config says about other devices using the proxy port.
// The zero value keeps it on this computer alone.
type LANSettings struct {
	Enabled bool
	// Account is what a device must give; zero asks for none.
	Account model.LANCredential
}

// LANState is the LAN access every config built from now on carries, shared
// like TunFlag: the LAN service sets it, and the services that build configs
// read it, so no reload drops it.
type LANState struct {
	mu    sync.Mutex
	value LANSettings
}

// NewLANState returns a state that keeps the proxy on this computer alone.
func NewLANState() *LANState { return &LANState{} }

// Get returns the LAN access now.
func (s *LANState) Get() LANSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value
}

// Set makes next the LAN access of the configs built from now on.
func (s *LANState) Set(next LANSettings) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.value = next
}

// lanAllowedSources are the only sources the proxy port answers while LAN
// access is on: this computer, and the private networks a router hands out.
// Nothing from the internet is let in, even when this computer has a public
// address or the router forwards the port.
var lanAllowedSources = []string{
	"127.0.0.0/8", "::1/128",
	"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16",
	"fc00::/7", "fe80::/10",
}

// lanSkipAuthSources are the sources that need no account even when one is
// asked for: this computer, so the system proxy and Hop's own requests keep
// working.
var lanSkipAuthSources = []string{"127.0.0.1/32", "::1/128"}

const (
	// lanBindAll makes the core listen on every interface, as allow-lan does.
	lanBindAll = "*"
	// lanUsername is the account's fixed name: only the password differs.
	lanUsername = "hop"
	// lanPasswordLength and lanPasswordAlphabet make a password easy to type
	// on a phone: no look-alike characters, about 49 bits.
	lanPasswordLength   = 10
	lanPasswordAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"
)

// applyLAN gives cfg the LAN access: listening everywhere, only for the
// allowed sources, and the account when one is asked for.
func applyLAN(cfg *runtimeConfig, lan LANSettings) {
	if !lan.Enabled {
		return
	}
	cfg.AllowLAN = true
	cfg.BindAddress = lanBindAll
	cfg.LanAllowedIPs = lanAllowedSources
	if lan.Account != (model.LANCredential{}) {
		cfg.Authentication = []string{lan.Account.Username + ":" + lan.Account.Password}
		cfg.SkipAuthPrefixes = lanSkipAuthSources
	}
}

// LANCredentialStore keeps the account between starts.
type LANCredentialStore interface {
	Load() (model.LANCredential, bool, error)
	Save(credential model.LANCredential) error
}

// LANAddressSource lists this computer's addresses on the networks other
// devices can share.
type LANAddressSource interface {
	Addresses() ([]netip.Addr, error)
}

// LANService lets other devices on the network use Hop's proxy port: it keeps
// the account, sets the LAN access every config carries and has the core
// reload it.
type LANService struct {
	state       *LANState
	credentials LANCredentialStore
	addresses   LANAddressSource
	reloader    RuntimeReloader
	tun         *TunFlag
	port        int
	// mu serializes changes, so two never reload at once.
	mu sync.Mutex
}

// NewLANService returns a service setting state, keeping the account in
// credentials, reading addresses from addresses and reloading through
// reloader; tun says whether TUN is on, which pauses the access, and port is
// the proxy port devices connect to.
func NewLANService(state *LANState, credentials LANCredentialStore, addresses LANAddressSource, reloader RuntimeReloader, tun *TunFlag, port int) *LANService {
	return &LANService{state: state, credentials: credentials, addresses: addresses, reloader: reloader, tun: tun, port: port}
}

// Prepare sets the LAN access for a start, before the core runs: nothing is
// reloaded.
func (s *LANService) Prepare(enabled bool, requireAccount bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next, err := s.settingsFor(enabled, requireAccount)
	if err != nil {
		return err
	}
	s.state.Set(next)
	return nil
}

// Apply sets the LAN access and has the core reload it. A reload that fails
// leaves the access as it was.
func (s *LANService) Apply(ctx context.Context, enabled bool, requireAccount bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next, err := s.settingsFor(enabled, requireAccount)
	if err != nil {
		return err
	}
	return s.switchTo(ctx, next)
}

// ResetAccount gives the account a new password and has the core reload it.
func (s *LANService) ResetAccount(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.state.Get()
	if !current.Enabled || current.Account == (model.LANCredential{}) {
		return errors.New("no account is asked for while LAN access is off or open")
	}
	account, err := newLANAccount()
	if err != nil {
		return err
	}
	if err := s.credentials.Save(account); err != nil {
		return err
	}
	next := LANSettings{Enabled: true, Account: account}
	if err := s.switchTo(ctx, next); err != nil {
		if restoreErr := s.credentials.Save(current.Account); restoreErr != nil {
			return errors.Join(err, fmt.Errorf("put the previous LAN account back: %w", restoreErr))
		}
		return err
	}
	return nil
}

// Status is the LAN access now and the addresses devices reach it at.
func (s *LANService) Status() (model.LANStatus, error) {
	current := s.state.Get()
	status := model.LANStatus{Enabled: current.Enabled, Account: current.Account}
	if !current.Enabled {
		return status, nil
	}
	if s.tun.On() {
		status.Paused = true
		return status, nil
	}
	addrs, err := s.addresses.Addresses()
	if err != nil {
		return model.LANStatus{}, err
	}
	for _, addr := range addrs {
		status.Addresses = append(status.Addresses, net.JoinHostPort(addr.String(), strconv.Itoa(s.port)))
	}
	return status, nil
}

// switchTo makes next the LAN access and reloads; if the reload fails, the
// previous access is back.
func (s *LANService) switchTo(ctx context.Context, next LANSettings) error {
	previous := s.state.Get()
	s.state.Set(next)
	if err := s.reloader.ReloadRuntime(ctx); err != nil {
		s.state.Set(previous)
		return fmt.Errorf("the core did not accept the LAN access: %w", err)
	}
	return nil
}

// settingsFor is the LAN access for what the user chose, with the kept
// account, or a new one when none was kept.
func (s *LANService) settingsFor(enabled bool, requireAccount bool) (LANSettings, error) {
	if !enabled || !requireAccount {
		return LANSettings{Enabled: enabled}, nil
	}
	account, found, err := s.credentials.Load()
	if err != nil {
		return LANSettings{}, err
	}
	if !found {
		if account, err = newLANAccount(); err != nil {
			return LANSettings{}, err
		}
		if err := s.credentials.Save(account); err != nil {
			return LANSettings{}, err
		}
	}
	return LANSettings{Enabled: true, Account: account}, nil
}

// newLANAccount makes an account with a random password.
func newLANAccount() (model.LANCredential, error) {
	raw := make([]byte, lanPasswordLength)
	if _, err := rand.Read(raw); err != nil {
		return model.LANCredential{}, fmt.Errorf("generate the LAN password: %w", err)
	}
	password := make([]byte, lanPasswordLength)
	for i, b := range raw {
		// 256 is not a multiple of the alphabet's length; the small bias is
		// no weakness for a password of this kind.
		password[i] = lanPasswordAlphabet[int(b)%len(lanPasswordAlphabet)]
	}
	return model.LANCredential{Username: lanUsername, Password: string(password)}, nil
}
