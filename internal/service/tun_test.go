package service

import (
	"slices"
	"testing"

	"go.yaml.in/yaml/v3"

	"hop/internal/model"
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
192.168.5.1/32     link#15            UCS                   en0      !
192.168.5.215/32   link#15            UCS                   en0      !
198.18.0.1         198.18.0.1         UH                  utun4
224.0.0/4          link#15            UmCS                  en0      !
255.255.255.255/32 link#15            UCS                   en0      !
`

// TestTunRouteConflict guards the rule that another client's TUN on the
// default-ish routes reads as taken, and Hop's own TUN never does.
func TestTunRouteConflict(t *testing.T) {
	cases := []struct {
		name      string
		out       string
		ownDevice string
		wantTaken bool
		wantBy    string
	}{
		{
			name:      "another client's tun on this machine",
			out:       netstatWithVerge,
			ownDevice: TunDevice,
			wantTaken: true,
			wantBy:    "utun4",
		},
		{
			name: "hop's own tun",
			out: `Internet:
Destination        Gateway            Flags               Netif Expire
default            198.18.0.1         UGScg             utun199
128.0/1            198.18.0.1         UGSc              utun199
`,
			ownDevice: TunDevice,
			wantTaken: false,
		},
		{
			name: "plain default on the physical interface",
			out: `Internet:
Destination        Gateway            Flags               Netif Expire
default            192.168.5.1        UGScg               en0
`,
			ownDevice: TunDevice,
			wantTaken: false,
		},
		{
			name: "both tuns, hop's and another's",
			out: `Internet:
Destination        Gateway            Flags               Netif Expire
default            198.18.0.1         UGScg             utun199
0/1                10.0.0.1           UGSc               utun7
`,
			ownDevice: TunDevice,
			wantTaken: true,
			wantBy:    "utun7",
		},
		{name: "headers only", out: "Routing tables\n\nInternet:\n", ownDevice: TunDevice},
		{name: "empty", out: "", ownDevice: TunDevice},
		{name: "garbage", out: "not a routing table at all\njust words\n", ownDevice: TunDevice},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			taken, by := TunRouteConflict(tc.out, tc.ownDevice)
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
	plain, err := RuntimeConfig([]byte(tunTestSubscription), model.RulesProvider, tunTestSettings(false))
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

	withTun, err := RuntimeConfig([]byte(tunTestSubscription), model.RulesProvider, tunTestSettings(true))
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
