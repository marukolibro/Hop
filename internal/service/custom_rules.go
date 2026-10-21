package service

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"unicode"

	"hop/internal/model"
)

// CustomRuleTypes are the rule types the settings offer, in the order they
// show.
var CustomRuleTypes = []string{
	model.CustomRuleDomainSuffix,
	model.CustomRuleDomain,
	model.CustomRuleDomainKeyword,
	model.CustomRuleIPCIDR,
	model.CustomRuleProcessName,
}

// CustomRuleTargets are where a custom rule can send traffic, in the order
// the settings offer them.
var CustomRuleTargets = []string{
	model.CustomRuleDirect,
	model.CustomRuleProxy,
	model.CustomRuleReject,
}

// CustomRuleStore keeps the user's custom rules on disk.
type CustomRuleStore interface {
	LoadCustomRules() ([]model.CustomRule, error)
	SaveCustomRules(rules []model.CustomRule) error
}

// RuntimeReloader rebuilds and applies the config the running core serves,
// which is what a changed rule list must reach.
type RuntimeReloader interface {
	ReloadRuntime(ctx context.Context) error
}

// CustomRulesService keeps the rules the user puts ahead of every profile's
// own, and makes the running core use a changed list.
type CustomRulesService struct {
	store   CustomRuleStore
	runtime RuntimeReloader
}

// NewCustomRulesService returns a service keeping rules in store and applying
// a change through runtime.
func NewCustomRulesService(store CustomRuleStore, runtime RuntimeReloader) *CustomRulesService {
	return &CustomRulesService{store: store, runtime: runtime}
}

// List returns the stored rules in match order. Stored rules are checked
// again, so a hand-edited file cannot build a config nobody validated.
func (s *CustomRulesService) List() ([]model.CustomRule, error) {
	rules, err := s.store.LoadCustomRules()
	if err != nil {
		return nil, err
	}
	normalized, err := NormalizeCustomRules(rules)
	if err != nil {
		return nil, fmt.Errorf("stored custom rules: %w", err)
	}
	return normalized, nil
}

// Save checks rules, keeps them, and applies a freshly built config to the
// running core. The list is validated before anything is written; a config
// the core rejects puts the previous list back, so a failed change leaves the
// rules the user had. With no subscription in use there is no runtime config
// to rebuild: the list only waits for the next one.
func (s *CustomRulesService) Save(ctx context.Context, rules []model.CustomRule) error {
	normalized, err := NormalizeCustomRules(rules)
	if err != nil {
		return err
	}
	previous, err := s.store.LoadCustomRules()
	if err != nil {
		return err
	}
	if err := s.store.SaveCustomRules(normalized); err != nil {
		return err
	}
	if err := s.runtime.ReloadRuntime(ctx); err != nil {
		if undoErr := s.store.SaveCustomRules(previous); undoErr != nil {
			return errors.Join(err, fmt.Errorf("put the previous custom rules back in the store: %w", undoErr))
		}
		return err
	}
	return nil
}

// NormalizeCustomRules trims and checks every rule, in order, and refuses
// duplicates: the first rule that matches wins, so a second rule for the same
// type and value could never match and is a mistake, not a choice.
func NormalizeCustomRules(rules []model.CustomRule) ([]model.CustomRule, error) {
	normalized := make([]model.CustomRule, 0, len(rules))
	seen := make(map[string]int, len(rules))
	for i, rule := range rules {
		next, err := normalizeCustomRule(rule)
		if err != nil {
			return nil, fmt.Errorf("rule %d: %w", i+1, err)
		}
		key := strings.ToLower(next.Type) + "\x00" + strings.ToLower(next.Value)
		if first, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("rule %d (%s %q) duplicates rule %d", i+1, next.Type, next.Value, first)
		}
		seen[key] = i + 1
		normalized = append(normalized, next)
	}
	return normalized, nil
}

// normalizeCustomRule checks one rule and returns it in the form the config
// writes: upper-case type and target, trimmed value, and a canonical CIDR.
func normalizeCustomRule(rule model.CustomRule) (model.CustomRule, error) {
	rule.Type = strings.ToUpper(strings.TrimSpace(rule.Type))
	rule.Target = strings.ToUpper(strings.TrimSpace(rule.Target))
	rule.Value = strings.TrimSpace(rule.Value)
	if !slices.Contains(CustomRuleTypes, rule.Type) {
		return model.CustomRule{}, fmt.Errorf("unknown type %q (want one of %v)", rule.Type, CustomRuleTypes)
	}
	if !slices.Contains(CustomRuleTargets, rule.Target) {
		return model.CustomRule{}, fmt.Errorf("unknown target %q (want one of %v)", rule.Target, CustomRuleTargets)
	}
	if rule.Value == "" {
		return model.CustomRule{}, errors.New("the value is empty")
	}
	if strings.ContainsAny(rule.Value, ",\r\n") {
		return model.CustomRule{}, errors.New("the value contains a comma or line break")
	}
	switch rule.Type {
	case model.CustomRuleDomainSuffix, model.CustomRuleDomain:
		if err := validateDomain(rule.Value); err != nil {
			return model.CustomRule{}, err
		}
	case model.CustomRuleDomainKeyword:
		if strings.IndexFunc(rule.Value, unicode.IsSpace) >= 0 {
			return model.CustomRule{}, errors.New("a domain keyword cannot contain whitespace")
		}
	case model.CustomRuleIPCIDR:
		prefix, err := netip.ParsePrefix(rule.Value)
		if err != nil {
			return model.CustomRule{}, fmt.Errorf("%q is not a CIDR address such as 192.0.2.0/24: %w", rule.Value, err)
		}
		rule.Value = prefix.Masked().String()
	case model.CustomRuleProcessName:
		// A process name may contain spaces ("Google Chrome"); the trim above
		// keeps them away from its ends.
	}
	return rule, nil
}

// validateDomain checks the hostname syntax a DOMAIN or DOMAIN-SUFFIX rule
// needs. ASCII only: a rule the core cannot match is refused here, not stored.
func validateDomain(value string) error {
	if len(value) > 253 {
		return fmt.Errorf("the domain is longer than 253 characters")
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" {
			return fmt.Errorf("%q has an empty label", value)
		}
		if len(label) > 63 {
			return fmt.Errorf("the label %q is longer than 63 characters", label)
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("the label %q starts or ends with a hyphen", label)
		}
		for _, r := range label {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			default:
				return fmt.Errorf("the label %q contains %q, which a domain name cannot", label, r)
			}
		}
	}
	return nil
}

// customRuleLines writes rules as the config's rule lines, in order. A rule
// aimed at the proxy uses mainGroup, the group the dashboard treats as the
// main route; a profile with no such group is refused rather than guessed at.
func customRuleLines(rules []model.CustomRule, mainGroup string) ([]string, error) {
	lines := make([]string, 0, len(rules))
	for i, rule := range rules {
		target := rule.Target
		switch target {
		case model.CustomRuleProxy:
			if mainGroup == "" {
				return nil, fmt.Errorf("custom rule %d (%s %q) targets 代理, but the profile declares no main proxy group", i+1, rule.Type, rule.Value)
			}
			if strings.ContainsAny(mainGroup, ",\r\n") {
				return nil, fmt.Errorf("the main proxy group %q cannot be a rule target: it contains a comma or line break", mainGroup)
			}
			target = mainGroup
		case model.CustomRuleDirect, model.CustomRuleReject:
		default:
			return nil, fmt.Errorf("custom rule %d has unknown target %q", i+1, rule.Target)
		}
		if rule.Value == "" || strings.ContainsAny(rule.Value, ",\r\n") {
			return nil, fmt.Errorf("custom rule %d (%s) has an empty value or one with a comma or line break", i+1, rule.Type)
		}
		switch rule.Type {
		case model.CustomRuleIPCIDR:
			lines = append(lines, rule.Type+","+rule.Value+","+target+",no-resolve")
		case model.CustomRuleDomainSuffix, model.CustomRuleDomain, model.CustomRuleDomainKeyword, model.CustomRuleProcessName:
			lines = append(lines, rule.Type+","+rule.Value+","+target)
		default:
			return nil, fmt.Errorf("custom rule %d has unknown type %q", i+1, rule.Type)
		}
	}
	return lines, nil
}
