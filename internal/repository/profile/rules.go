package profile

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"hop/internal/model"
)

const customRulesFileName = "custom-rules.json"

// customRulesFile is the custom rules as kept on disk.
type customRulesFile struct {
	Rules []customRuleFile `json:"rules"`
}

// customRuleFile is one rule as kept on disk.
type customRuleFile struct {
	Type   string `json:"type"`
	Value  string `json:"value"`
	Target string `json:"target"`
}

// LoadCustomRules returns the rules the user put ahead of every profile's
// own, in the order they match. No file yet means none were added.
func (s *Store) LoadCustomRules() ([]model.CustomRule, error) {
	path := filepath.Join(s.dir, customRulesFileName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read custom rules (path=%q): %w", path, err)
	}
	var file customRulesFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("parse custom rules (path=%q): %w", path, err)
	}
	rules := make([]model.CustomRule, 0, len(file.Rules))
	for _, rule := range file.Rules {
		rules = append(rules, model.CustomRule{Type: rule.Type, Value: rule.Value, Target: rule.Target})
	}
	return rules, nil
}

// SaveCustomRules keeps the rules for the next config and the next start.
func (s *Store) SaveCustomRules(rules []model.CustomRule) error {
	file := customRulesFile{Rules: make([]customRuleFile, 0, len(rules))}
	for _, rule := range rules {
		file.Rules = append(file.Rules, customRuleFile{Type: rule.Type, Value: rule.Value, Target: rule.Target})
	}
	raw, err := json.Marshal(file)
	if err != nil {
		return fmt.Errorf("encode custom rules: %w", err)
	}
	path := filepath.Join(s.dir, customRulesFileName)
	if err := writeAtomic(path, raw); err != nil {
		return err
	}
	return nil
}
