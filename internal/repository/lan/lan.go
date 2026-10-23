// Package lan keeps the account other devices on the network use to reach
// Hop's proxy, and finds the addresses they reach it at.
package lan

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"

	"hop/internal/model"
)

const (
	credentialFile = "lan-credential"
	// credentialPerm keeps the account owner-only, like the rest of the app
	// dir.
	credentialPerm = 0o600
)

// Credentials keeps the account in one file under the app dir.
type Credentials struct {
	path string
}

// NewCredentials returns a store keeping the account in appDir.
func NewCredentials(appDir string) *Credentials {
	return &Credentials{path: filepath.Join(appDir, credentialFile)}
}

// Load returns the kept account, and false when none was kept yet.
func (c *Credentials) Load() (model.LANCredential, bool, error) {
	raw, err := os.ReadFile(c.path)
	if errors.Is(err, os.ErrNotExist) {
		return model.LANCredential{}, false, nil
	}
	if err != nil {
		return model.LANCredential{}, false, fmt.Errorf("read the LAN account (path=%q): %w", c.path, err)
	}
	var credential model.LANCredential
	if err := json.Unmarshal(raw, &credential); err != nil {
		return model.LANCredential{}, false, fmt.Errorf("decode the LAN account (path=%q): %w", c.path, err)
	}
	if credential.Username == "" || credential.Password == "" {
		return model.LANCredential{}, false, fmt.Errorf("the stored LAN account is incomplete (path=%q)", c.path)
	}
	return credential, true, nil
}

// Save keeps credential, replacing the one kept before in one step.
func (c *Credentials) Save(credential model.LANCredential) error {
	raw, err := json.Marshal(credential)
	if err != nil {
		return fmt.Errorf("encode the LAN account: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return fmt.Errorf("create the app dir for the LAN account (path=%q): %w", filepath.Dir(c.path), err)
	}
	temp, err := os.CreateTemp(filepath.Dir(c.path), credentialFile+".*")
	if err != nil {
		return fmt.Errorf("write the LAN account (path=%q): %w", c.path, err)
	}
	defer os.Remove(temp.Name())
	if err := temp.Chmod(credentialPerm); err != nil {
		temp.Close()
		return fmt.Errorf("protect the LAN account (path=%q): %w", c.path, err)
	}
	if _, err := temp.Write(raw); err != nil {
		temp.Close()
		return fmt.Errorf("write the LAN account (path=%q): %w", c.path, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("write the LAN account (path=%q): %w", c.path, err)
	}
	if err := os.Rename(temp.Name(), c.path); err != nil {
		return fmt.Errorf("keep the LAN account (path=%q): %w", c.path, err)
	}
	return nil
}

// Source finds this computer's addresses on the networks other devices can
// share.
type Source struct{}

// Addresses lists them, see Addresses.
func (Source) Addresses() ([]netip.Addr, error) { return Addresses() }

// Interface is one network interface as the address search reads it.
type Interface struct {
	Name string
	// Usable says the interface is up, is no loopback and is no
	// point-to-point link.
	Usable    bool
	Addresses []netip.Addr
}

// virtualPrefixes start the names of interfaces that are no network a phone
// shares with this computer: tunnels, Apple's peer-to-peer links, virtual
// machine and container bridges.
var virtualPrefixes = []string{"utun", "awdl", "llw", "bridge", "vmnet", "docker", "veth", "br-", "vEthernet", "ZeroTier", "Tailscale", "tailscale"}

// Addresses lists the private IPv4 addresses of this computer on the networks
// other devices can share: the ones a router hands out, such as 192.168.1.5.
func Addresses() ([]netip.Addr, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list the network interfaces: %w", err)
	}
	found := make([]Interface, 0, len(ifaces))
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			return nil, fmt.Errorf("list the addresses of interface %q: %w", iface.Name, err)
		}
		entry := Interface{
			Name:   iface.Name,
			Usable: iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagLoopback == 0 && iface.Flags&net.FlagPointToPoint == 0,
		}
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok {
				if parsed, ok := netip.AddrFromSlice(ipNet.IP); ok {
					entry.Addresses = append(entry.Addresses, parsed.Unmap())
				}
			}
		}
		found = append(found, entry)
	}
	return PrivateIPv4(found), nil
}

// PrivateIPv4 picks the private IPv4 addresses of the usable, non-virtual
// interfaces, in the order the interfaces come.
func PrivateIPv4(ifaces []Interface) []netip.Addr {
	var out []netip.Addr
	for _, iface := range ifaces {
		if !iface.Usable || hasAnyPrefix(iface.Name, virtualPrefixes) {
			continue
		}
		for _, addr := range iface.Addresses {
			if addr.Is4() && addr.IsPrivate() {
				out = append(out, addr)
			}
		}
	}
	return out
}

func hasAnyPrefix(name string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
