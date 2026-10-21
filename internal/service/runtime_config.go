package service

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"

	"go.yaml.in/yaml/v3"

	"hop/internal/model"
)

// RuntimeSettings are the values Hop owns in every config it hands to the
// core. A subscription can never change them.
type RuntimeSettings struct {
	MixedPort          int
	ControllerAddr     string
	ControllerSecret   string
	BindAddress        string
	LogLevel           string
	BootstrapGroupName string
	// Mode is the outbound mode the core starts in.
	Mode string
	// Tun says the core runs with the TUN device on, so every packet goes
	// through it, not only the apps that honour a proxy. The core then runs
	// under Hop's privileged helper, as root, which owns the device.
	Tun bool
}

// TunDevice is the interface name Hop's TUN claims. It is fixed, so the
// routes Hop's own TUN adds are recognisable against any other client's
// (darwin accepts utun followed by a number, which the pinned core's tun
// stack requires).
const TunDevice = "utun199"

// subscriptionSections are the only top-level keys taken from a
// subscription. Everything else (ports, tun, dns, listeners, the external
// controller, ...) decides how the core touches the machine, so it stays
// under Hop's control.
var subscriptionSections = []string{
	"proxies",
	"proxy-groups",
	"proxy-providers",
	"rules",
	"rule-providers",
	"sub-rules",
}

// runtimeConfig is the config Hop hands the core: its own keys, filled from
// RuntimeSettings, with the allowed sections of a subscription under them.
type runtimeConfig struct {
	MixedPort   int    `yaml:"mixed-port"`
	BindAddress string `yaml:"bind-address"`
	AllowLAN    bool   `yaml:"allow-lan"`
	// Tun is the core's TUN listener, nil unless TUN is on: without it the
	// config carries no tun key at all.
	Tun *tunConfig `yaml:"tun,omitempty"`
	// Sniffer is nil unless TUN is on: see hopSniffer.
	Sniffer *snifferConfig `yaml:"sniffer,omitempty"`
	// UnifiedDelay measures a delay without the handshake, so a node's
	// first test is as low as its later ones.
	UnifiedDelay bool `yaml:"unified-delay"`
	// TCPConcurrent dials every address a name resolves to at once and
	// keeps the first that answers.
	TCPConcurrent      bool      `yaml:"tcp-concurrent"`
	Mode               string    `yaml:"mode"`
	LogLevel           string    `yaml:"log-level"`
	ExternalController string    `yaml:"external-controller"`
	Secret             string    `yaml:"secret"`
	DNS                dnsConfig `yaml:"dns"`

	Proxies        yaml.Node `yaml:"proxies,omitempty"`
	ProxyGroups    yaml.Node `yaml:"proxy-groups,omitempty"`
	ProxyProviders yaml.Node `yaml:"proxy-providers,omitempty"`
	Rules          yaml.Node `yaml:"rules,omitempty"`
	RuleProviders  yaml.Node `yaml:"rule-providers,omitempty"`
	SubRules       yaml.Node `yaml:"sub-rules,omitempty"`
}

// baseConfig is the config under any subscription: Hop's own keys from s,
// with the TUN section and the fake-ip resolver it brings when s asks for
// TUN, and the plain resolver otherwise.
func baseConfig(s RuntimeSettings) runtimeConfig {
	cfg := runtimeConfig{
		MixedPort:          s.MixedPort,
		BindAddress:        s.BindAddress,
		AllowLAN:           false,
		UnifiedDelay:       true,
		TCPConcurrent:      true,
		Mode:               s.Mode,
		LogLevel:           s.LogLevel,
		ExternalController: s.ControllerAddr,
		Secret:             s.ControllerSecret,
	}
	if s.Tun {
		cfg.Tun = hopTun()
		cfg.Sniffer = hopSniffer()
		cfg.DNS = tunDNS()
	} else {
		cfg.DNS = hopDNS()
	}
	return cfg
}

// tunConfig is the core's TUN listener. Its keys are the pinned core's own
// (config.RawTun in v1.19.32), checked against its source.
type tunConfig struct {
	Enable              bool     `yaml:"enable"`
	Device              string   `yaml:"device"`
	Stack               string   `yaml:"stack"`
	AutoRoute           bool     `yaml:"auto-route"`
	AutoDetectInterface bool     `yaml:"auto-detect-interface"`
	DNSHijack           []string `yaml:"dns-hijack"`
	// RouteExcludeAddress keeps the private networks out of the TUN, so LAN
	// traffic and the routers' own DNS keep their direct path.
	RouteExcludeAddress []string `yaml:"route-exclude-address"`
}

// snifferConfig reads the domain a connection is for out of its first
// bytes. Its keys are the pinned core's own (config.RawSniffer in v1.19.32).
type snifferConfig struct {
	Enable bool `yaml:"enable"`
	// OverrideDestination replaces the address an app connected to with the
	// domain sniffed from its traffic.
	OverrideDestination bool `yaml:"override-destination"`
	// ParsePureIP sniffs connections made to a bare IP too.
	ParsePureIP bool                       `yaml:"parse-pure-ip"`
	Sniff       map[string]sniffedProtocol `yaml:"sniff"`
}

// sniffedProtocol names the ports a protocol is sniffed on.
type sniffedProtocol struct {
	Ports []string `yaml:"ports"`
}

// hopSniffer sniffs HTTP Host, TLS SNI and QUIC under TUN. The system's
// resolver can sit outside the TUN (a router's link-local IPv6 address is
// reached on the LAN interface, where dns-hijack never sees it) and answer
// with a poisoned address; the sniffed domain replaces that address, so
// rules match by domain and the node resolves the name itself.
func hopSniffer() *snifferConfig {
	return &snifferConfig{
		Enable:              true,
		OverrideDestination: true,
		ParsePureIP:         true,
		Sniff: map[string]sniffedProtocol{
			"HTTP": {Ports: []string{"80", "8080-8880"}},
			"TLS":  {Ports: []string{"443", "8443"}},
			"QUIC": {Ports: []string{"443", "8443"}},
		},
	}
}

// hopTun is the TUN listener Hop runs: the mixed stack, routes taken over
// and answered by the core itself, DNS on port 53 included, with the local
// networks excluded.
func hopTun() *tunConfig {
	return &tunConfig{
		Enable:              true,
		Device:              TunDevice,
		Stack:               "mixed",
		AutoRoute:           true,
		AutoDetectInterface: true,
		DNSHijack:           []string{"any:53", "tcp://any:53"},
		RouteExcludeAddress: []string{
			"10.0.0.0/8",
			"172.16.0.0/12",
			"192.168.0.0/16",
			"169.254.0.0/16",
			"100.64.0.0/10",
			// Multicast and broadcast carry the LAN's discovery (mDNS,
			// AirDrop, AirPlay, printers); IPv6's local ranges likewise.
			"224.0.0.0/4",
			"255.255.255.255/32",
			"fc00::/7",
			"fe80::/10",
			"ff00::/8",
		},
	}
}

// dnsConfig is the core's own resolver, which it uses to match rules by IP
// and to reach DIRECT targets and the nodes themselves. It never listens:
// the system's DNS is left as it is.
type dnsConfig struct {
	Enable bool `yaml:"enable"`
	// IPv6 off spares every lookup an AAAA query the network rarely uses.
	IPv6           bool   `yaml:"ipv6"`
	CacheAlgorithm string `yaml:"cache-algorithm"`
	// EnhancedMode is the answering mode under TUN: fake-ip hands every
	// client a mapped address from a reserved range ("" when off).
	EnhancedMode string `yaml:"enhanced-mode,omitempty"`
	// FakeIPFilter names what must never resolve to a fake IP under TUN;
	// the listed names keep their real addresses.
	FakeIPFilter []string `yaml:"fake-ip-filter,omitempty"`
	// DefaultNameserver resolves the DoH servers' own names: plain IPs.
	DefaultNameserver []string `yaml:"default-nameserver"`
	// Nameserver answers rule matching and DIRECT targets over DoH, which
	// the network can neither read nor tamper with.
	Nameserver []string `yaml:"nameserver"`
	// ProxyServerNameserver resolves the nodes' own names.
	ProxyServerNameserver []string `yaml:"proxy-server-nameserver"`
}

// hopDNS resolves over DoH at the two largest public resolvers in China,
// which answer quickly from there, instead of the system's plain UDP DNS.
func hopDNS() dnsConfig {
	doh := []string{"https://dns.alidns.com/dns-query", "https://doh.pub/dns-query"}
	return dnsConfig{
		Enable:                true,
		IPv6:                  false,
		CacheAlgorithm:        "arc",
		DefaultNameserver:     []string{"223.5.5.5", "119.29.29.29"},
		Nameserver:            doh,
		ProxyServerNameserver: doh,
	}
}

// tunDNS is the resolver under TUN: the same DoH upstreams, answering every
// client in fake-ip so destinations are matched by name however the packets
// arrive, with the names that must stay real filtered out.
func tunDNS() dnsConfig {
	cfg := hopDNS()
	cfg.EnhancedMode = "fake-ip"
	cfg.FakeIPFilter = tunFakeIPFilter
	return cfg
}

// tunFakeIPFilter names what must never resolve to a fake IP: the LAN's own
// names, and the services that reach the network by address — push, time,
// NTP, STUN — which a mapped address breaks.
var tunFakeIPFilter = []string{
	"*.lan",
	"*.local",
	"*.push.apple.com",
	"time.*.apple.com",
	"time-ios.apple.com",
	"time-macos.apple.com",
	"time.*.com",
	"time.*.gov",
	"ntp.*.*",
	"stun.*.*",
	// Captive portal checks must reach the real portal, or a hotel's or an
	// airport's Wi-Fi never shows its sign-in page.
	"captive.apple.com",
	"+.msftconnecttest.com",
	"+.msftncsi.com",
	// Apple's location services.
	"+.ls.apple.com",
}

type bootstrapGroup struct {
	Name    string   `yaml:"name"`
	Type    string   `yaml:"type"`
	Proxies []string `yaml:"proxies"`
}

// BootstrapConfig is the profile Hop runs before any subscription is
// imported: loopback only, and one selector over the built-in outbounds.
func BootstrapConfig(s RuntimeSettings) ([]byte, error) {
	cfg := baseConfig(s)
	groups := []bootstrapGroup{{Name: s.BootstrapGroupName, Type: "select", Proxies: []string{"DIRECT", "REJECT"}}}
	if err := cfg.ProxyGroups.Encode(groups); err != nil {
		return nil, fmt.Errorf("build bootstrap proxy groups: %w", err)
	}
	if err := cfg.Rules.Encode([]string{"MATCH," + s.BootstrapGroupName}); err != nil {
		return nil, fmt.Errorf("build bootstrap rules: %w", err)
	}
	return marshalConfig(cfg)
}

// RuntimeConfig builds the config for the core from a subscription under
// Hop's own settings: its nodes always, and the groups and rules its rule
// source names — the provider's own, or Hop's template — with the user's
// custom rules matched before either. A list of node links carries no groups
// or rules, so it runs only with Hop's.
func RuntimeConfig(subscription []byte, rules model.RuleSource, custom []model.CustomRule, s RuntimeSettings) ([]byte, error) {
	p, err := readProfile(subscription)
	if err != nil {
		return nil, err
	}
	cfg := baseConfig(s)
	if rules == model.RulesProvider {
		if p.isLinkList() {
			return nil, errors.New("a list of node links has no rules of its own; run it with Hop's rules")
		}
		logDropped(p.doc)
		cfg.Proxies = p.doc["proxies"]
		cfg.ProxyGroups = p.doc["proxy-groups"]
		cfg.ProxyProviders = p.doc["proxy-providers"]
		cfg.RuleProviders = p.doc["rule-providers"]
		cfg.SubRules = p.doc["sub-rules"]
		mainGroup, err := providerMainGroup(p, custom)
		if err != nil {
			return nil, err
		}
		lines, err := customRuleLines(custom, mainGroup)
		if err != nil {
			return nil, err
		}
		cfg.Rules, err = prependRuleLines(p.doc["rules"], lines)
		if err != nil {
			return nil, err
		}
		return marshalConfig(cfg)
	}
	if rules != model.RulesHop {
		return nil, fmt.Errorf("unknown rule source %q", rules)
	}
	if err := applyHopTemplate(&cfg, p, custom); err != nil {
		return nil, err
	}
	return marshalConfig(cfg)
}

// providerMainGroup names the group the dashboard treats as the main route:
// the first proxy group the profile declares, in the order the dashboard
// reads them from the core (view.go's groupKind calls index 0 the main
// group). It is asked for only when a custom rule targets 代理; a profile
// without such a group is an error, never a guess.
func providerMainGroup(p subscriptionProfile, custom []model.CustomRule) (string, error) {
	if !slices.ContainsFunc(custom, func(rule model.CustomRule) bool { return rule.Target == model.CustomRuleProxy }) {
		return "", nil
	}
	var groups []struct {
		Name string `yaml:"name"`
	}
	node := p.doc["proxy-groups"]
	if err := node.Decode(&groups); err != nil {
		return "", fmt.Errorf("read the subscription's proxy groups for custom rules: %w", err)
	}
	if len(groups) == 0 || groups[0].Name == "" {
		return "", errors.New("a custom rule targets 代理, but the subscription declares no proxy group to send it through")
	}
	return groups[0].Name, nil
}

// prependRuleLines puts lines ahead of a profile's own rules. A profile
// without rules gets a list of just the custom ones.
func prependRuleLines(node yaml.Node, lines []string) (yaml.Node, error) {
	if len(lines) == 0 {
		return node, nil
	}
	if node.Kind == yaml.AliasNode && node.Alias != nil {
		node = *node.Alias
	}
	out := node
	if out.IsZero() {
		out = yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	} else if out.Kind != yaml.SequenceNode {
		return yaml.Node{}, errors.New("the subscription's rules are not a list")
	}
	custom := make([]*yaml.Node, 0, len(lines))
	for _, line := range lines {
		custom = append(custom, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: line})
	}
	out.Content = append(custom, out.Content...)
	return out, nil
}

// RulesFor is the rule source a subscription runs with: the one chosen,
// except that a list of node links, which has no rules of its own, runs with
// Hop's template.
func RulesFor(subscription []byte, chosen model.RuleSource) (model.RuleSource, error) {
	if !slices.Contains(RuleSources, chosen) {
		return "", fmt.Errorf("unknown rule source %q", chosen)
	}
	p, err := readProfile(subscription)
	if err != nil {
		return "", err
	}
	if p.isLinkList() && chosen == model.RulesProvider {
		return model.RulesHop, nil
	}
	return chosen, nil
}

// RuleSources are the rule sources a subscription can choose, in the order
// the editor shows them.
var RuleSources = []model.RuleSource{model.RulesProvider, model.RulesHop}

func requireProxies(doc map[string]yaml.Node) error {
	_, hasProxies := doc["proxies"]
	_, hasProviders := doc["proxy-providers"]
	if !hasProxies && !hasProviders {
		return fmt.Errorf("subscription has neither proxies nor proxy-providers (top-level keys: %v)", sortedKeys(doc))
	}
	return nil
}

func logDropped(doc map[string]yaml.Node) {
	var dropped []string
	for _, key := range sortedKeys(doc) {
		if !slices.Contains(subscriptionSections, key) {
			dropped = append(dropped, key)
		}
	}
	slog.Info("subscription keys managed by hop were ignored", "keys", dropped)
}

func sortedKeys(doc map[string]yaml.Node) []string {
	keys := make([]string, 0, len(doc))
	for key := range doc {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func marshalConfig(cfg runtimeConfig) ([]byte, error) {
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("encode runtime config: %w", err)
	}
	return out, nil
}
