package integration

import (
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"hop/internal/model"
	"hop/internal/service"
)

// profileWithoutGroups declares nodes but no policy group, so a custom rule
// aimed at 代理 has nowhere to go.
const profileWithoutGroups = `
proxies:
  - {name: "node", type: ss, server: 192.0.2.1, port: 8388, cipher: aes-128-gcm, password: x}
rules:
  - MATCH,DIRECT
`

// TestCustomRulesPrecedeProfileRules checks where the user's rules land in
// the generated config for both rule sources: ahead of the profile's own
// rules, and, under Hop's template, after the private-network rules that keep
// the LAN direct. It also checks that 代理 resolves to the group the dashboard
// treats as the main route, and that a profile with no such group is refused
// rather than guessed at.
func TestCustomRulesPrecedeProfileRules(t *testing.T) {
	settings := service.RuntimeSettings{
		MixedPort: testMixedPort, ControllerAddr: testController, ControllerSecret: testSecret,
		BindAddress: "127.0.0.1", LogLevel: "warning", BootstrapGroupName: "Hop", Mode: model.ModeRule,
	}
	custom := []model.CustomRule{
		{Type: model.CustomRuleDomainSuffix, Value: "example.com", Target: model.CustomRuleProxy},
		{Type: model.CustomRuleIPCIDR, Value: "192.0.2.0/24", Target: model.CustomRuleDirect},
		{Type: model.CustomRuleProcessName, Value: "Telegram", Target: model.CustomRuleReject},
	}
	providerLines := []string{
		"DOMAIN-SUFFIX,example.com,Proxy",
		"IP-CIDR,192.0.2.0/24,DIRECT,no-resolve",
		"PROCESS-NAME,Telegram,REJECT",
		"MATCH,Proxy",
	}

	got := rulesOf(t, []byte(profileA), model.RulesProvider, custom, settings)
	if !slices.Equal(got, providerLines) {
		t.Fatalf("provider rules = %v, want %v", got, providerLines)
	}

	got = rulesOf(t, []byte(profileA), model.RulesHop, custom, settings)
	wantHop := []string{
		"GEOSITE,private,DIRECT",
		"GEOIP,private,DIRECT,no-resolve",
		"DOMAIN-SUFFIX,example.com,节点选择",
		"IP-CIDR,192.0.2.0/24,DIRECT,no-resolve",
		"PROCESS-NAME,Telegram,REJECT",
		"GEOSITE,openai,AI",
	}
	if len(got) < len(wantHop) || !slices.Equal(got[:len(wantHop)], wantHop) {
		t.Fatalf("Hop rules = %v, want them to start with %v", got, wantHop)
	}

	_, err := service.RuntimeConfig([]byte(profileWithoutGroups), model.RulesProvider,
		[]model.CustomRule{{Type: model.CustomRuleDomain, Value: "example.com", Target: model.CustomRuleProxy}}, settings)
	if err == nil || !strings.Contains(err.Error(), "no proxy group") {
		t.Fatalf("config without a proxy group = %v, want a clear error", err)
	}
}

func rulesOf(t *testing.T, subscription []byte, source model.RuleSource, custom []model.CustomRule, settings service.RuntimeSettings) []string {
	t.Helper()
	config, err := service.RuntimeConfig(subscription, source, custom, settings)
	if err != nil {
		t.Fatalf("build config: %v", err)
	}
	var doc struct {
		Rules []string `yaml:"rules"`
	}
	if err := yaml.Unmarshal(config, &doc); err != nil {
		t.Fatalf("parse generated config: %v", err)
	}
	return doc.Rules
}
