//go:build windows

package sysproxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"hop/internal/model"
)

const internetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// WinINet options that make running apps reread the proxy settings.
const (
	internetOptionRefresh         = 37
	internetOptionSettingsChanged = 39
)

var internetSetOption = windows.NewLazySystemDLL("wininet.dll").NewProc("InternetSetOptionW")

// Current reads the user's proxy from the registry. One ProxyServer for all
// protocols fills every endpoint; a per-protocol list ("http=...;https=...")
// fills the ones it names.
func (c *Controller) Current(_ context.Context) (model.SystemProxyState, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.QUERY_VALUE)
	if err != nil {
		return model.SystemProxyState{}, fmt.Errorf("open the internet settings key: %w", err)
	}
	defer key.Close()
	enabled, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return model.SystemProxyState{}, fmt.Errorf("read ProxyEnable: %w", err)
	}
	server, _, err := key.GetStringValue("ProxyServer")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return model.SystemProxyState{}, fmt.Errorf("read ProxyServer: %w", err)
	}
	pac, _, err := key.GetStringValue("AutoConfigURL")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return model.SystemProxyState{}, fmt.Errorf("read AutoConfigURL: %w", err)
	}
	state := model.SystemProxyState{AutoConfig: pac != ""}
	if enabled == 0 {
		return state, nil
	}
	for _, part := range strings.Split(server, ";") {
		scheme, addr, perProtocol := strings.Cut(part, "=")
		if !perProtocol {
			e := endpoint(part)
			state.HTTP, state.HTTPS, state.SOCKS = e, e, e
			continue
		}
		switch strings.ToLower(strings.TrimSpace(scheme)) {
		case "http":
			state.HTTP = endpoint(addr)
		case "https":
			state.HTTPS = endpoint(addr)
		case "socks":
			state.SOCKS = endpoint(addr)
		}
	}
	return state, nil
}

// endpoint reads one "host:port"; one without a port reads as port 0,
// which is no one's.
func endpoint(addr string) model.ProxyEndpoint {
	host, portText, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return model.ProxyEndpoint{Enabled: true, Host: strings.TrimSpace(addr)}
	}
	port, _ := strconv.Atoi(portText)
	return model.ProxyEndpoint{Enabled: true, Host: host, Port: port}
}

// Set points the user's proxy at host:port for every protocol and sets
// ProxyOverride so local and LAN addresses bypass the proxy.
func (c *Controller) Set(_ context.Context, host string, port int, bypass []string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open the internet settings key: %w", err)
	}
	defer key.Close()
	if err := key.SetStringValue("ProxyServer", net.JoinHostPort(host, strconv.Itoa(port))); err != nil {
		return fmt.Errorf("write ProxyServer: %w", err)
	}
	if override := formatWindowsBypass(bypass); override != "" {
		if err := key.SetStringValue("ProxyOverride", override); err != nil {
			return fmt.Errorf("write ProxyOverride: %w", err)
		}
	}
	if err := key.SetDWordValue("ProxyEnable", 1); err != nil {
		return fmt.Errorf("write ProxyEnable: %w", err)
	}
	return announce()
}

// formatWindowsBypass turns bypass entries into WinINet's semicolon-separated
// ProxyOverride list, translating common private CIDRs into wildcard patterns
// WinINet understands and appending <local>.
func formatWindowsBypass(entries []string) string {
	var out []string
	add := func(v string) {
		for _, existing := range out {
			if existing == v {
				return
			}
		}
		out = append(out, v)
	}
	for _, entry := range entries {
		switch entry {
		case "127.0.0.1", "127.0.0.0/8":
			add("127.*")
		case "10.0.0.0/8":
			add("10.*")
		case "172.16.0.0/12":
			for i := 16; i <= 31; i++ {
				add(fmt.Sprintf("172.%d.*", i))
			}
		case "192.168.0.0/16":
			add("192.168.*")
		case "169.254.0.0/16", "169.254/16":
			add("169.254.*")
		case "100.64.0.0/10":
			for i := 64; i <= 127; i++ {
				add(fmt.Sprintf("100.%d.*", i))
			}
		case "::1":
			// WinINet handles loopback IPv6 via <local>.
		default:
			if prefix, err := netip.ParsePrefix(entry); err == nil && prefix.Addr().Is4() {
				ip := prefix.Masked().Addr().As4()
				switch prefix.Bits() {
				case 8:
					add(fmt.Sprintf("%d.*", ip[0]))
				case 16:
					add(fmt.Sprintf("%d.%d.*", ip[0], ip[1]))
				case 24:
					add(fmt.Sprintf("%d.%d.%d.*", ip[0], ip[1], ip[2]))
				case 32:
					add(prefix.Addr().String())
				default:
					add(entry)
				}
				continue
			}
			add(entry)
		}
	}
	if len(out) == 0 {
		return ""
	}
	add("<local>")
	return strings.Join(out, ";")
}

// Clear turns the user's proxy off.
func (c *Controller) Clear(_ context.Context) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open the internet settings key: %w", err)
	}
	defer key.Close()
	if err := key.SetDWordValue("ProxyEnable", 0); err != nil {
		return fmt.Errorf("write ProxyEnable: %w", err)
	}
	return announce()
}

// announce tells running apps the proxy settings changed.
func announce() error {
	for _, option := range []uintptr{internetOptionSettingsChanged, internetOptionRefresh} {
		ok, _, err := internetSetOption.Call(0, option, 0, 0)
		if ok == 0 {
			return fmt.Errorf("announce the proxy change (InternetSetOption %d): %w", option, err)
		}
	}
	return nil
}
