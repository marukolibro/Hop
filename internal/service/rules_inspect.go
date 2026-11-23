package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strings"

	"hop/internal/model"
)

// RuleInspector reads the core's active rules and remote providers, and
// refreshes providers on demand.
type RuleInspector interface {
	Rules(ctx context.Context) ([]model.ActiveRule, error)
	ProxyProviders(ctx context.Context) ([]model.ProviderSummary, error)
	RuleProviders(ctx context.Context) ([]model.ProviderSummary, error)
	UpdateProxyProvider(ctx context.Context, name string) error
	UpdateRuleProvider(ctx context.Context, name string) error
}

// RuleInspectService exposes the core's active rule table and remote providers.
type RuleInspectService struct {
	inspector RuleInspector
}

// NewRuleInspectService returns a service backed by inspector.
func NewRuleInspectService(inspector RuleInspector) *RuleInspectService {
	return &RuleInspectService{inspector: inspector}
}

// ListRules returns all active rules in the core's evaluation order.
func (s *RuleInspectService) ListRules(ctx context.Context) ([]model.ActiveRule, error) {
	return s.inspector.Rules(ctx)
}

// ListProviders returns all remote/file proxy and rule providers currently
// loaded by the core.
func (s *RuleInspectService) ListProviders(ctx context.Context) ([]model.ProviderSummary, error) {
	proxies, err := s.inspector.ProxyProviders(ctx)
	if err != nil {
		return nil, err
	}
	rules, err := s.inspector.RuleProviders(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]model.ProviderSummary, 0, len(proxies)+len(rules))
	out = append(out, proxies...)
	out = append(out, rules...)
	return out, nil
}

// UpdateProvider refreshes one named proxy or rule provider.
func (s *RuleInspectService) UpdateProvider(ctx context.Context, kind string, name string) error {
	switch kind {
	case model.ProviderKindProxy:
		return s.inspector.UpdateProxyProvider(ctx, name)
	case model.ProviderKindRule:
		return s.inspector.UpdateRuleProvider(ctx, name)
	default:
		return fmt.Errorf("unknown provider kind %q", kind)
	}
}

// UpdateAllProviders refreshes every listed provider and returns all errors
// joined.
func (s *RuleInspectService) UpdateAllProviders(ctx context.Context, providers []model.ProviderSummary) error {
	var errs []error
	for _, p := range providers {
		if err := s.UpdateProvider(ctx, p.Kind, p.Name); err != nil {
			errs = append(errs, fmt.Errorf("%s %q: %w", p.Kind, p.Name, err))
		}
	}
	return errors.Join(errs...)
}

// DiagnoseRule evaluates query (a domain, URL, or IP address) against rules in
// order and resolves the winning rule's target through groups. Rules that
// require the core's compiled databases (GeoSite, GeoIP, RuleSet) cannot be
// evaluated offline; if any sit ahead of the matched rule, SkippedDynamic is
// set so the UI can note that those rules may match at runtime.
func DiagnoseRule(query string, rules []model.ActiveRule, groups []model.GroupView) model.RuleDiagnostic {
	host, addr, isIP := normalizeRuleQuery(query)
	if host == "" {
		return model.RuleDiagnostic{}
	}
	lowerHost := strings.ToLower(host)
	skippedDynamic := 0
	for _, rule := range rules {
		if rule.Disabled {
			continue
		}
		typ := strings.ToUpper(strings.ReplaceAll(rule.Type, "-", ""))
		switch typ {
		case "DOMAIN":
			if !isIP && strings.EqualFold(host, rule.Payload) {
				return finishDiagnostic(host, rule, groups, skippedDynamic)
			}
		case "DOMAINSUFFIX":
			p := strings.ToLower(rule.Payload)
			if !isIP && (lowerHost == p || strings.HasSuffix(lowerHost, "."+p)) {
				return finishDiagnostic(host, rule, groups, skippedDynamic)
			}
		case "DOMAINKEYWORD":
			if !isIP && rule.Payload != "" && strings.Contains(lowerHost, strings.ToLower(rule.Payload)) {
				return finishDiagnostic(host, rule, groups, skippedDynamic)
			}
		case "DOMAINREGEX":
			if !isIP && rule.Payload != "" {
				if re, err := regexp.Compile(rule.Payload); err == nil && re.MatchString(host) {
					return finishDiagnostic(host, rule, groups, skippedDynamic)
				}
			}
		case "IPCIDR", "IPCIDR6":
			if isIP {
				if prefix, err := netip.ParsePrefix(rule.Payload); err == nil && prefix.Contains(addr) {
					return finishDiagnostic(host, rule, groups, skippedDynamic)
				}
			}
		case "MATCH":
			return finishDiagnostic(host, rule, groups, skippedDynamic)
		case "GEOSITE", "GEOIP", "RULESET", "SUBRULE", "AND", "OR", "NOT":
			skippedDynamic++
		}
	}
	return model.RuleDiagnostic{Query: host, SkippedDynamic: skippedDynamic}
}

func finishDiagnostic(query string, rule model.ActiveRule, groups []model.GroupView, skippedDynamic int) model.RuleDiagnostic {
	chain, exit := resolveTargetChain(rule.Proxy, groups)
	return model.RuleDiagnostic{
		Query:          query,
		Matched:        true,
		Rule:           rule,
		Chain:          chain,
		Exit:           exit,
		SkippedDynamic: skippedDynamic,
	}
}

// resolveTargetChain follows group selections from target down to the final
// outbound proxy or built-in action (DIRECT/REJECT).
func resolveTargetChain(target string, groups []model.GroupView) ([]string, string) {
	byName := make(map[string]model.GroupView, len(groups))
	for _, g := range groups {
		byName[g.Name] = g
	}
	var chain []string
	seen := make(map[string]bool)
	cur := target
	for cur != "" && !seen[cur] {
		seen[cur] = true
		chain = append(chain, cur)
		g, ok := byName[cur]
		if !ok || g.Selected == "" {
			break
		}
		cur = g.Selected
	}
	exit := ""
	if len(chain) > 0 {
		exit = chain[len(chain)-1]
	}
	return chain, exit
}

// normalizeRuleQuery extracts a clean hostname or IP address from a raw user
// input that may include a scheme, path, or port.
func normalizeRuleQuery(raw string) (host string, addr netip.Addr, isIP bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", netip.Addr{}, false
	}
	if strings.Contains(s, "://") {
		if u, err := url.Parse(s); err == nil && u.Hostname() != "" {
			s = u.Hostname()
		}
	} else {
		if idx := strings.IndexByte(s, '/'); idx >= 0 {
			s = s[:idx]
		}
		if h, _, err := net.SplitHostPort(s); err == nil && h != "" {
			s = h
		}
	}
	s = strings.TrimSuffix(strings.Trim(s, "[]"), ".")
	if s == "" {
		return "", netip.Addr{}, false
	}
	if parsed, err := netip.ParseAddr(s); err == nil {
		return parsed.String(), parsed, true
	}
	return s, netip.Addr{}, false
}
