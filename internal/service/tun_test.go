package service

import (
	"slices"
	"testing"

	"go.yaml.in/yaml/v3"

	"hop/internal/model"
	"hop/internal/repository/routetable"
)

// TestTunRouteConflict guards the rule that another client's TUN on the
// default path reads as taken, and Hop's own TUN never does. TUN clients
// split the default route around the private ranges, so taking the default
// path is measured by coverage, not by one 0.0.0.0/0 row.
func TestTunRouteConflict(t *testing.T) {
	split := routetable.Table{
		{Destination: "1", Interface: "utun4", Tunnel: true},
		{Destination: "2/7", Interface: "utun4", Tunnel: true},
		{Destination: "4/6", Interface: "utun4", Tunnel: true},
		{Destination: "8/5", Interface: "utun4", Tunnel: true},
		{Destination: "16/4", Interface: "utun4", Tunnel: true},
		{Destination: "32/3", Interface: "utun4", Tunnel: true},
		{Destination: "64/2", Interface: "utun4", Tunnel: true},
		{Destination: "128.0/1", Interface: "utun4", Tunnel: true},
	}
	cases := []struct {
		name      string
		table     routetable.Table
		ownDevice string
		wantTaken bool
		wantBy    string
	}{
		{
			name: "another client's tun on this machine",
			table: append(routetable.Table{
				{Destination: "default", Interface: "en0"},
				{Destination: "127", Interface: "lo0"},
				{Destination: "169.254", Interface: "en0"},
				{Destination: "192.168.5", Interface: "en0"},
			}, split...),
			ownDevice: TunDevice,
			wantTaken: true,
			wantBy:    "utun4",
		},
		{
			name: "hop's own tun",
			table: routetable.Table{
				{Destination: "default", Interface: TunDevice, Tunnel: true},
				{Destination: "128.0/1", Interface: TunDevice, Tunnel: true},
			},
			ownDevice: TunDevice,
			wantTaken: false,
		},
		{
			name: "hop's own tun under another case",
			table: routetable.Table{
				{Destination: "default", Interface: "UTUN199", Tunnel: true},
			},
			ownDevice: TunDevice,
			wantTaken: false,
		},
		{
			name: "plain default on the physical interface",
			table: routetable.Table{
				{Destination: "default", Interface: "en0"},
			},
			ownDevice: TunDevice,
			wantTaken: false,
		},
		{
			name: "both tuns, hop's and another's",
			table: routetable.Table{
				{Destination: "default", Interface: TunDevice, Tunnel: true},
				{Destination: "0/1", Interface: "utun7", Tunnel: true},
			},
			ownDevice: TunDevice,
			wantTaken: true,
			wantBy:    "utun7",
		},
		{
			name: "another tun with the private ranges excluded",
			table: routetable.Table{
				{Destination: "0.0.0.0/5", Interface: "utun8", Tunnel: true},
				{Destination: "8.0.0.0/7", Interface: "utun8", Tunnel: true},
				{Destination: "32.0.0.0/3", Interface: "utun8", Tunnel: true},
				{Destination: "64.0.0.0/2", Interface: "utun8", Tunnel: true},
				{Destination: "128.0.0.0/1", Interface: "utun8", Tunnel: true},
			},
			ownDevice: TunDevice,
			wantTaken: true,
			wantBy:    "utun8",
		},
		{
			name: "split-tunnel vpn on one subnet",
			table: routetable.Table{
				{Destination: "10.0.0.0/8", Interface: "utun9", Tunnel: true},
			},
			ownDevice: TunDevice,
			wantTaken: false,
		},
		{name: "headers only", table: routetable.ParseNetstat("Routing tables\n\nInternet:\n"), ownDevice: TunDevice},
		{name: "empty", table: nil, ownDevice: TunDevice},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			taken, by := TunRouteConflict(tc.table, tc.ownDevice)
			if taken != tc.wantTaken || by != tc.wantBy {
				t.Fatalf("taken=%v by=%q, want taken=%v by=%q", taken, by, tc.wantTaken, tc.wantBy)
			}
		})
	}
}

// tunTestSubscription is the smallest profile RuntimeConfig accepts.
const tunTestSubscription = `proxies:
  - name: "node"
    type: socks5
    server: 1.2.3.4
    port: 1080
rules:
  - MATCH,DIRECT
`

func tunTestSettings(tun bool) RuntimeSettings {
	return RuntimeSettings{
		MixedPort: 17890, ControllerAddr: "127.0.0.1:19090", ControllerSecret: "secret",
		BindAddress: "127.0.0.1", LogLevel: "info", BootstrapGroupName: "Hop",
		Mode: "rule", Tun: tun,
	}
}

// TestRuntimeConfigTun pins the TUN section and the fake-ip resolver Hop
// writes when TUN is on, and that a config without TUN carries no tun key at
// all, exactly as before.
func TestRuntimeConfigTun(t *testing.T) {
	plain, err := RuntimeConfig([]byte(tunTestSubscription), model.RulesProvider, nil, tunTestSettings(false))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(plain, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["tun"]; ok {
		t.Errorf("config with TUN off carries a tun section:\n%s", plain)
	}
	if _, ok := doc["sniffer"]; ok {
		t.Errorf("config with TUN off carries a sniffer:\n%s", plain)
	}
	dns := doc["dns"].(map[string]any)
	if _, ok := dns["enhanced-mode"]; ok {
		t.Errorf("config with TUN off sets a DNS enhanced-mode:\n%s", plain)
	}

	withTun, err := RuntimeConfig([]byte(tunTestSubscription), model.RulesProvider, nil, tunTestSettings(true))
	if err != nil {
		t.Fatal(err)
	}
	doc = nil
	if err := yaml.Unmarshal(withTun, &doc); err != nil {
		t.Fatal(err)
	}
	tun := doc["tun"].(map[string]any)
	wantTun := map[string]any{
		"enable": true, "device": "utun199", "stack": "mixed",
		"auto-route": true, "auto-detect-interface": true,
	}
	for key, want := range wantTun {
		if tun[key] != want {
			t.Errorf("tun.%s = %v, want %v", key, tun[key], want)
		}
	}
	if !slices.Equal(tun["dns-hijack"].([]any), []any{"any:53", "tcp://any:53"}) {
		t.Errorf("tun.dns-hijack = %v", tun["dns-hijack"])
	}
	excludes := tun["route-exclude-address"].([]any)
	for _, want := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "100.64.0.0/10", "224.0.0.0/4", "255.255.255.255/32"} {
		if !slices.ContainsFunc(excludes, func(got any) bool { return got.(string) == want }) {
			t.Errorf("tun.route-exclude-address misses %s: %v", want, excludes)
		}
	}
	dns = doc["dns"].(map[string]any)
	if dns["enhanced-mode"] != "fake-ip" {
		t.Errorf("dns.enhanced-mode = %v, want fake-ip", dns["enhanced-mode"])
	}
	filter := dns["fake-ip-filter"].([]any)
	if len(filter) == 0 {
		t.Error("dns.fake-ip-filter is empty under TUN")
	}
	// A resolver outside the TUN can answer with a poisoned address; the
	// sniffed domain must replace it.
	sniffer := doc["sniffer"].(map[string]any)
	if sniffer["enable"] != true || sniffer["override-destination"] != true {
		t.Errorf("sniffer under TUN = %v, want enabled and overriding the destination", sniffer)
	}
	for _, protocol := range []string{"HTTP", "TLS", "QUIC"} {
		if _, ok := sniffer["sniff"].(map[string]any)[protocol]; !ok {
			t.Errorf("sniffer does not sniff %s: %v", protocol, sniffer["sniff"])
		}
	}
}

// TestBootstrapConfigTunMatchesRuntime makes sure the bootstrap profile
// carries the same TUN section, so TUN works before any subscription is
// imported too.
func TestBootstrapConfigTunMatchesRuntime(t *testing.T) {
	bootstrap, err := BootstrapConfig(tunTestSettings(true))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(bootstrap, &doc); err != nil {
		t.Fatal(err)
	}
	tun, ok := doc["tun"].(map[string]any)
	if !ok || tun["enable"] != true || tun["device"] != TunDevice {
		t.Fatalf("bootstrap config with TUN on carries no proper tun section:\n%s", bootstrap)
	}
}
