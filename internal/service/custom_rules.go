package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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

// CustomRuleTargets are the built-in targets a custom rule can send traffic
// to, in the order the settings offer them. A rule may also name any policy
// group declared by the profile.
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

// RuleConnectionCloser reads and closes active connections on the core so
// changed rules take effect immediately on existing keep-alive flows.
type RuleConnectionCloser interface {
	Connections(ctx context.Context) (model.Connections, error)
	CloseConnection(ctx context.Context, id string) error
}

// CustomRulesService keeps the rules the user puts ahead of every profile's
// own, and makes the running core use a changed list.
type CustomRulesService struct {
	store   CustomRuleStore
	runtime RuntimeReloader
	closer  RuleConnectionCloser
}

// NewCustomRulesService returns a service keeping rules in store, applying a
// change through runtime, and optionally closing active connections matched by
// changed rules through closer.
func NewCustomRulesService(store CustomRuleStore, runtime RuntimeReloader, closer RuleConnectionCloser) *CustomRulesService {
	return &CustomRulesService{store: store, runtime: runtime, closer: closer}
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
// to rebuild: the list only waits for the next one. After a successful reload,
// active connections matching changed rules are closed so new traffic obeys
// the updated routing right away.
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
	s.closeAffectedConnections(ctx, previous, normalized)
	return nil
}

// closeAffectedConnections closes active connections whose match was added,
// removed, retargeted, toggled, or reordered by a rule save. Failures are
// logged as warnings since the config reload itself already succeeded.
func (s *CustomRulesService) closeAffectedConnections(ctx context.Context, previous, next []model.CustomRule) {
	if s.closer == nil {
		return
	}
	affected := changedCustomRules(previous, next)
	if len(affected) == 0 {
		return
	}
	conns, err := s.closer.Connections(ctx)
	if err != nil {
		slog.Warn("could not read active connections after rule change", "error", err)
		return
	}
	ids := MatchingRuleConnections(conns, affected)
	for _, id := range ids {
		if err := s.closer.CloseConnection(ctx, id); err != nil {
			slog.Warn("could not close connection after rule change", "id", id, "error", err)
		}
	}
}

// changedCustomRules returns the enabled rules whose routing effect changed
// between previous and next: added, removed, toggled, retargeted, or reordered
// relative to another enabled rule.
func changedCustomRules(previous, next []model.CustomRule) []model.CustomRule {
	ruleKey := func(r model.CustomRule) string {
		return strings.ToLower(r.Type) + "\x00" + strings.ToLower(r.Value)
	}
	prevOn := make([]model.CustomRule, 0, len(previous))
	prevMap := make(map[string]model.CustomRule, len(previous))
	for _, r := range previous {
		if r.Disabled {
			continue
		}
		prevOn = append(prevOn, r)
		prevMap[ruleKey(r)] = r
	}
	nextOn := make([]model.CustomRule, 0, len(next))
	nextMap := make(map[string]model.CustomRule, len(next))
	for _, r := range next {
		if r.Disabled {
			continue
		}
		nextOn = append(nextOn, r)
		nextMap[ruleKey(r)] = r
	}

	var affected []model.CustomRule
	seen := make(map[string]struct{})
	add := func(r model.CustomRule) {
		k := ruleKey(r)
		if _, ok := seen[k]; ok {
			return
		}
		seen[k] = struct{}{}
		affected = append(affected, r)
	}

	var prevCommon, nextCommon []model.CustomRule
	for _, r := range prevOn {
		k := ruleKey(r)
		nr, ok := nextMap[k]
		if !ok || nr.Target != r.Target {
			add(r)
			continue
		}
		prevCommon = append(prevCommon, r)
	}
	for _, r := range nextOn {
		k := ruleKey(r)
		pr, ok := prevMap[k]
		if !ok || pr.Target != r.Target {
			add(r)
			continue
		}
		nextCommon = append(nextCommon, r)
	}
	for i := range prevCommon {
		if i < len(nextCommon) && ruleKey(prevCommon[i]) != ruleKey(nextCommon[i]) {
			add(prevCommon[i])
			add(nextCommon[i])
		}
	}
	return affected
}

// MatchingRuleConnections returns the IDs of active connections matching any
// rule in rules, preserving the order in conns.Active.
func MatchingRuleConnections(conns model.Connections, rules []model.CustomRule) []string {
	if len(rules) == 0 || len(conns.Active) == 0 {
		return nil
	}
	var ids []string
	for _, conn := range conns.Active {
		for _, rule := range rules {
			if connectionMatchesRule(conn, rule) {
				ids = append(ids, conn.ID)
				break
			}
		}
	}
	return ids
}

func connectionMatchesRule(conn model.Connection, rule model.CustomRule) bool {
	switch rule.Type {
	case model.CustomRuleDomain:
		return conn.Host != "" && strings.EqualFold(conn.Host, rule.Value)
	case model.CustomRuleDomainSuffix:
		if conn.Host == "" {
			return false
		}
		host := strings.ToLower(conn.Host)
		val := strings.ToLower(rule.Value)
		return host == val || strings.HasSuffix(host, "."+val)
	case model.CustomRuleDomainKeyword:
		if conn.Host == "" {
			return false
		}
		return strings.Contains(strings.ToLower(conn.Host), strings.ToLower(rule.Value))
	case model.CustomRuleIPCIDR:
		addr, err := netip.ParseAddr(conn.Host)
		if err != nil {
			return false
		}
		prefix, err := netip.ParsePrefix(rule.Value)
		if err != nil {
			return false
		}
		return prefix.Contains(addr)
	case model.CustomRuleProcessName:
		return conn.Process != "" && strings.EqualFold(conn.Process, rule.Value)
	default:
		return false
	}
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
// writes: upper-case type, canonical built-in target or preserved group name,
// trimmed value, and a canonical CIDR.
func normalizeCustomRule(rule model.CustomRule) (model.CustomRule, error) {
	rule.Type = strings.ToUpper(strings.TrimSpace(rule.Type))
	rule.Target = strings.TrimSpace(rule.Target)
	rule.Value = strings.TrimSpace(rule.Value)
	if !slices.Contains(CustomRuleTypes, rule.Type) {
		return model.CustomRule{}, fmt.Errorf("unknown type %q (want one of %v)", rule.Type, CustomRuleTypes)
	}
	if rule.Target == "" {
		return model.CustomRule{}, errors.New("the target is empty")
	}
	if strings.ContainsAny(rule.Target, ",\r\n") {
		return model.CustomRule{}, errors.New("the target contains a comma or line break")
	}
	switch {
	case strings.EqualFold(rule.Target, model.CustomRuleDirect):
		rule.Target = model.CustomRuleDirect
	case strings.EqualFold(rule.Target, model.CustomRuleProxy):
		rule.Target = model.CustomRuleProxy
	case strings.EqualFold(rule.Target, model.CustomRuleReject):
		rule.Target = model.CustomRuleReject
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

// customRuleLines writes enabled rules as the config's rule lines, in order.
// A rule aimed at the proxy uses mainGroup, the group the dashboard treats as
// the main route. A rule targeting a named policy group uses that group when
// declared in availableGroups, or falls back to mainGroup with a warning when
// the active profile does not declare that group.
func customRuleLines(rules []model.CustomRule, mainGroup string, availableGroups []string) ([]string, error) {
	lines := make([]string, 0, len(rules))
	for i, rule := range rules {
		if rule.Disabled {
			continue
		}
		target := rule.Target
		switch target {
		case model.CustomRuleProxy:
			if mainGroup == "" {
				return nil, fmt.Errorf("custom rule %d (%s %q) targets proxy, but the profile declares no proxy group", i+1, rule.Type, rule.Value)
			}
			if strings.ContainsAny(mainGroup, ",\r\n") {
				return nil, fmt.Errorf("the main proxy group %q cannot be a rule target: it contains a comma or line break", mainGroup)
			}
			target = mainGroup
		case model.CustomRuleDirect, model.CustomRuleReject:
		default:
			if target == "" || strings.ContainsAny(target, ",\r\n") {
				return nil, fmt.Errorf("custom rule %d has invalid target %q", i+1, rule.Target)
			}
			if !slices.Contains(availableGroups, target) {
				if mainGroup == "" {
					return nil, fmt.Errorf("custom rule %d (%s %q) targets group %q, which is missing and the profile declares no proxy group", i+1, rule.Type, rule.Value, target)
				}
				if strings.ContainsAny(mainGroup, ",\r\n") {
					return nil, fmt.Errorf("the main proxy group %q cannot be a rule target: it contains a comma or line break", mainGroup)
				}
				slog.Warn("custom rule target group not found in active profile; routing through main group", "target", target, "mainGroup", mainGroup)
				target = mainGroup
			}
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

type exportedCustomRules struct {
	Version int                  `json:"version"`
	Rules   []exportedCustomRule `json:"rules"`
}

type exportedCustomRule struct {
	Type     string `json:"type"`
	Value    string `json:"value"`
	Target   string `json:"target"`
	Disabled bool   `json:"disabled,omitempty"`
}

// ExportCustomRulesJSON serializes rules into pretty-printed, versioned JSON
// for backup and sharing across machines.
func ExportCustomRulesJSON(rules []model.CustomRule) ([]byte, error) {
	normalized, err := NormalizeCustomRules(rules)
	if err != nil {
		return nil, err
	}
	payload := exportedCustomRules{
		Version: 1,
		Rules:   make([]exportedCustomRule, 0, len(normalized)),
	}
	for _, r := range normalized {
		payload.Rules = append(payload.Rules, exportedCustomRule{
			Type:     r.Type,
			Value:    r.Value,
			Target:   r.Target,
			Disabled: r.Disabled,
		})
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode custom rules JSON: %w", err)
	}
	return append(raw, '\n'), nil
}

// ParseCustomRulesJSON reads custom rules from either Hop's versioned export
// object ({"version": 1, "rules": [...]}) or a bare JSON array ([...]) and
// validates every entry with NormalizeCustomRules.
func ParseCustomRulesJSON(raw []byte) ([]model.CustomRule, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, errors.New("the JSON file is empty")
	}
	var items []exportedCustomRule
	if trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil, fmt.Errorf("parse custom rules array: %w", err)
		}
	} else {
		var wrapper exportedCustomRules
		if err := json.Unmarshal(trimmed, &wrapper); err != nil {
			return nil, fmt.Errorf("parse custom rules JSON: %w", err)
		}
		if wrapper.Rules == nil {
			return nil, errors.New("the JSON file has no rules array")
		}
		items = wrapper.Rules
	}
	rules := make([]model.CustomRule, 0, len(items))
	for _, r := range items {
		rules = append(rules, model.CustomRule{
			Type:     r.Type,
			Value:    r.Value,
			Target:   r.Target,
			Disabled: r.Disabled,
		})
	}
	return NormalizeCustomRules(rules)
}

// MergeCustomRules merges imported rules into existing rules without dropping
// existing entries: matching (type, value) rules are updated in place, and new
// rules are appended in order. It returns the merged slice plus counts of
// newly added and updated rules.
func MergeCustomRules(existing, imported []model.CustomRule) ([]model.CustomRule, int, int) {
	merged := slices.Clone(existing)
	keyOf := func(r model.CustomRule) string {
		return strings.ToLower(r.Type) + "\x00" + strings.ToLower(r.Value)
	}
	indexByKey := make(map[string]int, len(merged))
	for i, r := range merged {
		indexByKey[keyOf(r)] = i
	}
	added, updated := 0, 0
	for _, r := range imported {
		k := keyOf(r)
		if idx, exists := indexByKey[k]; exists {
			if merged[idx] != r {
				merged[idx] = r
				updated++
			}
			continue
		}
		indexByKey[k] = len(merged)
		merged = append(merged, r)
		added++
	}
	return merged, added, updated
}
