// Package routetable reads the system's IPv4 routing table, so Hop can tell
// whose TUN holds the default routes without touching the table itself. The
// parser and the derivations are plain functions, shared by both platforms
// and tested anywhere; only the read is platform-specific.
package routetable

import (
	"encoding/binary"
	"net/netip"
	"sort"
	"strings"
)

// Route is one IPv4 route as Hop needs it: where it goes, which interface
// carries it, and whether that interface is a tunnel (a TUN included). A
// physical interface with the same destination is not a conflict.
type Route struct {
	Destination string
	Interface   string
	Tunnel      bool
}

// Table is the IPv4 routing table.
type Table []Route

// netstatDestinations expands the shorthand `netstat -rn -f inet` prints for
// the split default routes the macOS TUN clients use.
var netstatDestinations = map[string]string{
	"default": "0.0.0.0/0",
	"0/1":     "0.0.0.0/1",
	"128.0/1": "128.0.0.0/1",
	"1":       "1.0.0.0/8",
	"2/7":     "2.0.0.0/7",
	"4/6":     "4.0.0.0/6",
	"8/5":     "8.0.0.0/5",
	"16/4":    "16.0.0.0/4",
	"32/3":    "32.0.0.0/3",
	"64/2":    "64.0.0.0/2",
}

// ParseNetstat reads the routes out of `netstat -rn -f inet` output. A
// route line is destination, gateway, flags and interface, with an optional
// expire column; anything shorter is a header or a note. A utun interface is
// a tunnel.
func ParseNetstat(out string) Table {
	var table Table
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[0] == "Destination" {
			continue
		}
		iface := fields[3]
		table = append(table, Route{
			Destination: fields[0],
			Interface:   iface,
			Tunnel:      strings.HasPrefix(iface, "utun"),
		})
	}
	return table
}

// interfaceTypeTunnel is IF_TYPE_TUNNEL from the Windows interface table.
const interfaceTypeTunnel = 131

// tunnelAdapterWords name the virtual adapters Windows TUN and VPN clients
// install: Wintun (mihomo, Clash, sing-box, Tailscale), TAP-Windows
// (OpenVPN), and WireGuard. A tunnel adapter holding most of the default
// route is another client's takeover, whoever made it.
var tunnelAdapterWords = []string{
	"wintun", "tap-windows", "tap-win32", "wireguard", "tailscale", "openvpn", "sing-tun", "vpn",
}

// TunnelAdapter says whether a Windows interface is a tunnel: the system
// reports some as IF_TYPE_TUNNEL, while Wintun adapters describe themselves
// as "Wintun Userspace Tunnel". The alias and description are matched
// case-insensitively.
func TunnelAdapter(alias string, description string, ifType uint32) bool {
	if ifType == interfaceTypeTunnel {
		return true
	}
	lowerAlias := strings.ToLower(alias)
	for _, prefix := range []string{"utun", "wintun", "tun", "tap"} {
		if strings.HasPrefix(lowerAlias, prefix) {
			return true
		}
	}
	text := strings.ToLower(alias + " " + description)
	for _, word := range tunnelAdapterWords {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

// DefaultCoverage is the fraction of the IPv4 unicast space that the routes
// on iface cover, from 0 to 1. A TUN that takes the default path covers
// (nearly) all of it: even with the private ranges excluded, the excluded
// part is a small minority. A split-tunnel VPN's single subnet covers a
// sliver.
func DefaultCoverage(table Table, iface string) float64 {
	var spans [][2]uint64
	for _, route := range table {
		if !strings.EqualFold(route.Interface, iface) {
			continue
		}
		prefix, ok := parseDestination(route.Destination)
		if !ok || !prefix.Addr().Is4() {
			continue
		}
		start, end := ipv4Span(prefix)
		spans = append(spans, [2]uint64{start, end})
	}
	if len(spans) == 0 {
		return 0
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i][0] < spans[j][0] })
	var covered uint64
	start, end := spans[0][0], spans[0][1]
	for _, span := range spans[1:] {
		if span[0] > end+1 {
			covered += end - start + 1
			start, end = span[0], span[1]
			continue
		}
		if span[1] > end {
			end = span[1]
		}
	}
	covered += end - start + 1
	return float64(covered) / (1 << 32)
}

// parseDestination reads one destination, expanding macOS's shorthand.
func parseDestination(text string) (netip.Prefix, bool) {
	if expanded, ok := netstatDestinations[text]; ok {
		text = expanded
	}
	prefix, err := netip.ParsePrefix(text)
	if err != nil || !prefix.Addr().Is4() {
		return netip.Prefix{}, false
	}
	return prefix.Masked(), true
}

// ipv4Span is the first and last address of an IPv4 prefix as integers.
func ipv4Span(prefix netip.Prefix) (uint64, uint64) {
	addr := prefix.Masked().Addr().As4()
	value := uint64(binary.BigEndian.Uint32(addr[:]))
	size := uint64(1) << (32 - prefix.Bits())
	start := value & ^(size - 1)
	return start, start + size - 1
}
