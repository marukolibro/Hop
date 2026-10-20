package service

import (
	"fmt"
	"log/slog"
	"slices"
	"sort"

	"go.yaml.in/yaml/v3"
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
}

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

type runtimeConfig struct {
	MixedPort   int    `yaml:"mixed-port"`
	BindAddress string `yaml:"bind-address"`
	AllowLAN    bool   `yaml:"allow-lan"`
	// UnifiedDelay measures a delay without the handshake, so a node's
	// first test is as low as its later ones.
	UnifiedDelay       bool   `yaml:"unified-delay"`
	Mode               string `yaml:"mode"`
	LogLevel           string `yaml:"log-level"`
	ExternalController string `yaml:"external-controller"`
	Secret             string `yaml:"secret"`

	Proxies        yaml.Node `yaml:"proxies,omitempty"`
	ProxyGroups    yaml.Node `yaml:"proxy-groups,omitempty"`
	ProxyProviders yaml.Node `yaml:"proxy-providers,omitempty"`
	Rules          yaml.Node `yaml:"rules,omitempty"`
	RuleProviders  yaml.Node `yaml:"rule-providers,omitempty"`
	SubRules       yaml.Node `yaml:"sub-rules,omitempty"`
}

func baseConfig(s RuntimeSettings) runtimeConfig {
	return runtimeConfig{
		MixedPort:          s.MixedPort,
		BindAddress:        s.BindAddress,
		AllowLAN:           false,
		UnifiedDelay:       true,
		Mode:               s.Mode,
		LogLevel:           s.LogLevel,
		ExternalController: s.ControllerAddr,
		Secret:             s.ControllerSecret,
	}
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

// RuntimeConfig builds the config for the core from a subscription: its
// proxies, groups and rules, under Hop's own settings.
func RuntimeConfig(subscription []byte, s RuntimeSettings) ([]byte, error) {
	var doc map[string]yaml.Node
	if err := yaml.Unmarshal(subscription, &doc); err != nil {
		return nil, fmt.Errorf("subscription is not a Clash YAML profile (a base64 link list is not supported): %w", err)
	}
	if err := requireProxies(doc); err != nil {
		return nil, err
	}
	logDropped(doc)

	cfg := baseConfig(s)
	cfg.Proxies = doc["proxies"]
	cfg.ProxyGroups = doc["proxy-groups"]
	cfg.ProxyProviders = doc["proxy-providers"]
	cfg.Rules = doc["rules"]
	cfg.RuleProviders = doc["rule-providers"]
	cfg.SubRules = doc["sub-rules"]
	return marshalConfig(cfg)
}

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
