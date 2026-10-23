package lan

import (
	"net/netip"
	"testing"
)

func TestPrivateIPv4(t *testing.T) {
	addr := func(s string) netip.Addr { return netip.MustParseAddr(s) }
	got := PrivateIPv4([]Interface{
		{Name: "en0", Usable: true, Addresses: []netip.Addr{addr("192.168.1.5"), addr("fe80::1"), addr("8.8.8.8")}},
		{Name: "en1", Usable: false, Addresses: []netip.Addr{addr("10.0.0.2")}},
		{Name: "utun199", Usable: true, Addresses: []netip.Addr{addr("10.9.0.1")}},
		{Name: "bridge100", Usable: true, Addresses: []netip.Addr{addr("192.168.64.1")}},
		{Name: "Ethernet", Usable: true, Addresses: []netip.Addr{addr("172.20.1.9"), addr("169.254.3.3")}},
	})
	want := []netip.Addr{addr("192.168.1.5"), addr("172.20.1.9")}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("PrivateIPv4 = %v, want %v", got, want)
	}
}
