package service

import (
	"slices"
	"testing"

	"hop/internal/model"
)

func TestCustomRulesJSONAndMerge(t *testing.T) {
	initial := []model.CustomRule{
		{Type: model.CustomRuleDomainSuffix, Value: "claude.ai", Target: "AI"},
		{Type: model.CustomRuleIPCIDR, Value: "192.0.2.0/24", Target: model.CustomRuleDirect, Disabled: true},
	}

	raw, err := ExportCustomRulesJSON(initial)
	if err != nil {
		t.Fatalf("ExportCustomRulesJSON: %v", err)
	}
	parsed, err := ParseCustomRulesJSON(raw)
	if err != nil {
		t.Fatalf("ParseCustomRulesJSON: %v", err)
	}
	if !slices.Equal(parsed, initial) {
		t.Fatalf("round-trip = %+v, want %+v", parsed, initial)
	}

	// Bare JSON array is also accepted.
	bare := []byte(`[{"type":"domain","value":"api.github.com","target":"proxy"}]`)
	fromBare, err := ParseCustomRulesJSON(bare)
	if err != nil || len(fromBare) != 1 || fromBare[0].Target != model.CustomRuleProxy {
		t.Fatalf("bare array parse = %+v err=%v", fromBare, err)
	}

	// Merge updates matching rule in place and appends new rules.
	imported := []model.CustomRule{
		{Type: model.CustomRuleDomainSuffix, Value: "claude.ai", Target: "US"},
		{Type: model.CustomRuleProcessName, Value: "Telegram", Target: model.CustomRuleProxy},
	}
	merged, added, updated := MergeCustomRules(initial, imported)
	if added != 1 || updated != 1 || len(merged) != 3 {
		t.Fatalf("merge got len=%d added=%d updated=%d, want 3, 1, 1", len(merged), added, updated)
	}
	if merged[0].Target != "US" || merged[2].Value != "Telegram" {
		t.Fatalf("merged = %+v", merged)
	}
}
