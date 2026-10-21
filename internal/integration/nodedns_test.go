package integration

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"hop/internal/model"
	"hop/internal/repository/mihomo"
	"hop/internal/service"
)

// profileWithNodeEntries points its node's server at the provider's entry
// name through hosts, as providers do to send nodes through their nearest
// entry, and also tries to pin a site and to choose resolvers Hop must not
// take: plain UDP and one on this machine.
const profileWithNodeEntries = `
proxies:
  - {name: "HK 01", type: ss, server: hk.node.example, port: 8388, cipher: aes-128-gcm, password: x}
  - {name: "SG 01", type: ss, server: 192.0.2.7, port: 8388, cipher: aes-128-gcm, password: x}
proxy-groups:
  - {name: Proxy, type: select, proxies: ["HK 01", "SG 01"]}
hosts:
  hk.node.example: entry.provider.example
  www.google.com: 192.0.2.66
dns:
  nameserver:
    - https://doh.provider.example/dns-query
    - 114.114.114.114
    - udp://127.0.0.1:7874
  proxy-server-nameserver:
    - udp://127.0.0.1:7874
rules:
  - MATCH,Proxy
`

// TestNodeEntriesLimitedToNodes checks what Hop takes from a subscription's
// hosts and dns for both rule sources, with TUN off and on: only the entry
// for its own node's server, the provider's encrypted resolver for the name
// it points at, looked up through Hop's own listener and never as a fake IP;
// the pinned site and the plain or local resolvers stay out. The real core
// must accept the result.
func TestNodeEntriesLimitedToNodes(t *testing.T) {
	binary := os.Getenv("HOP_MIHOMO_BINARY")
	if binary == "" {
		t.Skip("HOP_MIHOMO_BINARY is not set")
	}
	validateDir := t.TempDir()
	if err := mihomo.SeedGeodata(filepath.Join("..", "..", "resources", "geodata"), validateDir); err != nil {
		t.Fatal(err)
	}
	validator := mihomo.NewConfigValidator(binary, validateDir, 30*time.Second)
	const listen = "127.0.0.1:17854"
	for _, rules := range []model.RuleSource{model.RulesProvider, model.RulesHop} {
		for _, tun := range []bool{false, true} {
			config, err := service.RuntimeConfig([]byte(profileWithNodeEntries), rules, nil, service.RuntimeSettings{
				MixedPort: testMixedPort, ControllerAddr: testController, ControllerSecret: testSecret,
				BindAddress: "127.0.0.1", LogLevel: "warning", BootstrapGroupName: "Hop",
				Mode: model.ModeRule, Tun: tun, NodeDNSListen: listen,
			})
			if err != nil {
				t.Fatalf("%s tun=%v: %v", rules, tun, err)
			}
			var got struct {
				Hosts map[string]string `yaml:"hosts"`
				DNS   struct {
					Listen                string              `yaml:"listen"`
					ProxyServerNameserver []string            `yaml:"proxy-server-nameserver"`
					NameserverPolicy      map[string][]string `yaml:"nameserver-policy"`
					FakeIPFilter          []string            `yaml:"fake-ip-filter"`
				} `yaml:"dns"`
			}
			if err := yaml.Unmarshal(config, &got); err != nil {
				t.Fatal(err)
			}
			if len(got.Hosts) != 1 || got.Hosts["hk.node.example"] != "entry.provider.example" {
				t.Errorf("%s tun=%v: hosts %v, want only the node's server", rules, tun, got.Hosts)
			}
			if got.DNS.Listen != listen || !slices.Equal(got.DNS.ProxyServerNameserver, []string{"udp://" + listen}) {
				t.Errorf("%s tun=%v: node servers resolve through %v (listen %q), want Hop's listener", rules, tun, got.DNS.ProxyServerNameserver, got.DNS.Listen)
			}
			if want := []string{"https://doh.provider.example/dns-query"}; !slices.Equal(got.DNS.NameserverPolicy["entry.provider.example"], want) || len(got.DNS.NameserverPolicy) != 1 {
				t.Errorf("%s tun=%v: nameserver-policy %v, want only the provider's encrypted resolver for its entry name", rules, tun, got.DNS.NameserverPolicy)
			}
			if tun && (!slices.Contains(got.DNS.FakeIPFilter, "hk.node.example") || !slices.Contains(got.DNS.FakeIPFilter, "entry.provider.example")) {
				t.Errorf("tun: the node server and its entry name must keep real addresses, fake-ip-filter %v", got.DNS.FakeIPFilter)
			}
			if err := validator.Validate(context.Background(), config); err != nil {
				t.Errorf("%s tun=%v: the core rejected the config: %v", rules, tun, err)
			}
		}
	}
}
