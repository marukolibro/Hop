package routetable

import (
	"math"
	"testing"
)

// netstatWithVerge is this machine's real `netstat -rn -f inet` output while
// another Clash client's TUN (utun4) held the default-ish routes through the
// subnet-chain split, with the plain default still on the physical interface.
const netstatWithVerge = `Routing tables

Internet:
Destination        Gateway            Flags               Netif Expire
default            192.168.5.1        UGScg                 en0
1                  198.18.0.1         UGSc                utun4
2/7                198.18.0.1         UGSc                utun4
4/6                198.18.0.1         UGSc                utun4
8/5                198.18.0.1         UGSc                utun4
16/4               198.18.0.1         UGSc                utun4
32/3               198.18.0.1         UGSc                utun4
64/2               198.18.0.1         UGSc                utun4
127                127.0.0.1          UCS                   lo0
127.0.0.1          127.0.0.1          UH                    lo0
128.0/1            198.18.0.1         UGSc                utun4
169.254            link#15            UCS                   en0      !
192.168.5          link#15            UCS                   en0      !
198.18.0.1         198.18.0.1         UH                  utun4
224.0.0/4          link#15            UmCS                  en0      !
`

// TestParseNetstat pins the fields the conflict rule reads: the destination
// shorthand, and which interfaces count as tunnels.
func TestParseNetstat(t *testing.T) {
	table := ParseNetstat(netstatWithVerge)
	if len(table) != 15 {
		t.Fatalf("parsed %d routes, want 15: %v", len(table), table)
	}
	first := table[0]
	if first.Destination != "default" || first.Interface != "en0" || first.Tunnel {
		t.Errorf("first route = %+v, want the default on en0, not a tunnel", first)
	}
	var utun int
	for _, route := range table {
		if route.Interface == "utun4" {
			utun++
			if !route.Tunnel {
				t.Errorf("utun4 route %+v is not marked a tunnel", route)
			}
		}
	}
	if utun != 9 {
		t.Errorf("parsed %d routes on utun4, want 9", utun)
	}
	if table := ParseNetstat("Routing tables\n\nInternet:\n"); len(table) != 0 {
		t.Errorf("headers parsed as %d routes: %v", len(table), table)
	}
}

// TestParseDestination pins macOS's shorthand and plain prefixes.
func TestParseDestination(t *testing.T) {
	cases := map[string]string{
		"default":        "0.0.0.0/0",
		"0/1":            "0.0.0.0/1",
		"128.0/1":        "128.0.0.0/1",
		"1":              "1.0.0.0/8",
		"0.0.0.0/0":      "0.0.0.0/0",
		"192.168.0.0/16": "192.168.0.0/16",
	}
	for in, want := range cases {
		prefix, ok := parseDestination(in)
		if !ok || prefix.String() != want {
			t.Errorf("parseDestination(%q) = %v, %v; want %s", in, prefix, ok, want)
		}
	}
	for _, bad := range []string{"", "garbage", "link#15", "::/0", "fe80::/10"} {
		if _, ok := parseDestination(bad); ok {
			t.Errorf("parseDestination(%q) parsed", bad)
		}
	}
}

// TestDefaultCoverage pins how much of IPv4 an interface's routes cover:
// the whole space, half, a subnet, overlaps, and other interfaces ignored.
func TestDefaultCoverage(t *testing.T) {
	cases := []struct {
		name  string
		table Table
		iface string
		want  float64
	}{
		{
			name:  "the default route",
			table: Table{{Destination: "default", Interface: "utun4", Tunnel: true}},
			iface: "utun4",
			want:  1,
		},
		{
			name:  "one half",
			table: Table{{Destination: "0.0.0.0/1", Interface: "utun4", Tunnel: true}},
			iface: "utun4",
			want:  0.5,
		},
		{
			name: "two halves",
			table: Table{
				{Destination: "0.0.0.0/1", Interface: "utun4", Tunnel: true},
				{Destination: "128.0.0.0/1", Interface: "utun4", Tunnel: true},
			},
			iface: "utun4",
			want:  1,
		},
		{
			name: "one subnet",
			table: Table{
				{Destination: "10.0.0.0/8", Interface: "utun4", Tunnel: true},
			},
			iface: "utun4",
			want:  1.0 / 256,
		},
		{
			name: "overlapping prefixes count once",
			table: Table{
				{Destination: "0.0.0.0/1", Interface: "utun4", Tunnel: true},
				{Destination: "64.0.0.0/2", Interface: "utun4", Tunnel: true},
				{Destination: "192.0.0.0/2", Interface: "utun4", Tunnel: true},
			},
			iface: "utun4",
			want:  0.75,
		},
		{
			name: "another interface",
			table: Table{
				{Destination: "0.0.0.0/0", Interface: "en0"},
			},
			iface: "utun4",
			want:  0,
		},
		{
			name:  "a lowercase name match",
			table: Table{{Destination: "0.0.0.0/0", Interface: "UTUN4", Tunnel: true}},
			iface: "utun4",
			want:  1,
		},
		{
			name:  "no routes",
			table: nil,
			iface: "utun4",
			want:  0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DefaultCoverage(tc.table, tc.iface)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("DefaultCoverage = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestTunnelAdapter pins what a Windows interface must look like to count
// as a tunnel: Wintun and other virtual adapters yes, a physical NIC no.
func TestTunnelAdapter(t *testing.T) {
	cases := []struct {
		name        string
		alias       string
		description string
		ifType      uint32
		want        bool
	}{
		{"wintun by description", "utun199", "Wintun Userspace Tunnel", 6, true},
		{"clash's wintun", "Clash", "Wintun Userspace Tunnel", 6, true},
		{"wireguard", "wg0", "WireGuard Tunnel", 6, true},
		{"tap adapter", "Local Area Connection", "TAP-Windows Adapter V9", 6, true},
		{"system tunnel type", "Ethernet 2", "", 131, true},
		{"physical wifi", "Wi-Fi", "Intel(R) Wi-Fi 6 AX201 160MHz", 71, false},
		{"physical ethernet", "Ethernet", "Realtek PCIe GbE Family Controller", 6, false},
		{"empty", "", "", 6, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := TunnelAdapter(tc.alias, tc.description, tc.ifType); got != tc.want {
				t.Fatalf("TunnelAdapter(%q, %q, %d) = %v, want %v", tc.alias, tc.description, tc.ifType, got, tc.want)
			}
		})
	}
}
