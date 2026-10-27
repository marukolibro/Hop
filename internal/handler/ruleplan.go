package handler

import (
	"net/netip"
	"slices"
	"strings"

	"hop/internal/model"
	"hop/internal/service"
	"hop/locales"
)

// ruleSuggestion is a rule the add dialog offers for a connection: the type
// and the value it would match, and the words on its button.
type ruleSuggestion struct {
	typeIndex int
	value     string
	label     string
}

// secondLevelLabels are the labels that, right before a two-letter country
// code, are part of the registry's suffix, as in example.co.uk.
var secondLevelLabels = []string{"co", "com", "net", "org", "gov", "edu", "ac", "ne", "or", "go"}

// suggestRules lists the rules that would catch conn, the likeliest first:
// its site as a domain suffix, the host itself, then the program that opened
// it. A connection with no domain gets its address instead. Hop has no public
// suffix list, so the site is a guess from the host's last labels that the
// dialog lets the user edit.
func suggestRules(conn model.Connection) []ruleSuggestion {
	var out []ruleSuggestion
	host := strings.ToLower(strings.TrimSpace(conn.Host))
	if addr, err := netip.ParseAddr(host); err == nil {
		out = append(out, ruleSuggestion{ruleTypeIndex(model.CustomRuleIPCIDR), netip.PrefixFrom(addr, addr.BitLen()).String(), locales.RuleSuggestAddress(host)})
	} else if host != "" {
		site := siteOf(host)
		out = append(out, ruleSuggestion{ruleTypeIndex(model.CustomRuleDomainSuffix), site, locales.RuleSuggestSite(site)})
		if host != site {
			out = append(out, ruleSuggestion{ruleTypeIndex(model.CustomRuleDomain), host, locales.RuleSuggestDomain(host)})
		}
	}
	if conn.Process != "" {
		out = append(out, ruleSuggestion{ruleTypeIndex(model.CustomRuleProcessName), conn.Process, locales.RuleSuggestApp(conn.Process)})
	}
	return out
}

// siteOf is the registrable part of a host: its last two labels, or three
// where a country code follows a second-level label.
func siteOf(host string) string {
	labels := strings.Split(host, ".")
	n := len(labels)
	if n <= 2 {
		return host
	}
	keep := 2
	if n >= 3 && len(labels[n-1]) == 2 && slices.Contains(secondLevelLabels, labels[n-2]) {
		keep = 3
	}
	return strings.Join(labels[n-keep:], ".")
}

// ruleTypeIndex is the place of a rule type in the dialog's type switch.
func ruleTypeIndex(typ string) int {
	return slices.Index(service.CustomRuleTypes, typ)
}
