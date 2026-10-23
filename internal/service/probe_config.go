package service

import (
	"fmt"
	"slices"

	"go.yaml.in/yaml/v3"

	"hop/internal/model"
)

// probeListener is a core listener that hands everything it accepts to one
// proxy by name, without asking the rules. Its keys are the pinned core's
// own (listener/inbound BaseOption in v1.19.33).
type probeListener struct {
	Name   string `yaml:"name"`
	Type   string `yaml:"type"`
	Listen string `yaml:"listen"`
	Port   int    `yaml:"port"`
	Proxy  string `yaml:"proxy"`
}

// probeGroup is a select group over the profile's groups, where the check
// picks the group under test.
type probeGroup struct {
	Name    string   `yaml:"name"`
	Type    string   `yaml:"type"`
	Proxies []string `yaml:"proxies"`
}

// addProbe gives cfg model.ProbeLanes listeners from bindAddress:port on, and
// for each the group it sends through, after the profile's own groups. A
// config without a port, or whose profile has no group to check, has none.
func addProbe(cfg *runtimeConfig, bindAddress string, port int) error {
	if port == 0 {
		return nil
	}
	names, err := groupNames(cfg.ProxyGroups)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return nil
	}
	groups := cfg.ProxyGroups
	for lane := range model.ProbeLanes {
		name := model.ProbeGroup(lane)
		groups, err = appendMapping(groups, probeGroup{Name: name, Type: configSelect, Proxies: names})
		if err != nil {
			return fmt.Errorf("add probe group %s: %w", name, err)
		}
		cfg.Listeners = append(cfg.Listeners, probeListener{
			Name: name, Type: "http", Listen: bindAddress, Port: port + lane, Proxy: name,
		})
	}
	cfg.ProxyGroups = groups
	return nil
}

// groupNames lists the names of the groups in a YAML list node, none for a
// missing one.
func groupNames(node yaml.Node) ([]string, error) {
	if node.IsZero() {
		return nil, nil
	}
	var groups []struct {
		Name string `yaml:"name"`
	}
	if err := node.Decode(&groups); err != nil {
		return nil, fmt.Errorf("read the proxy groups to check: %w", err)
	}
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		if g.Name != "" && !slices.Contains(names, g.Name) {
			names = append(names, g.Name)
		}
	}
	return names, nil
}

// appendMapping returns the YAML list node with item added at its end. A
// missing list becomes a list of just item. The input is not modified.
func appendMapping(node yaml.Node, item any) (yaml.Node, error) {
	if node.Kind == yaml.AliasNode && node.Alias != nil {
		node = *node.Alias
	}
	out := node
	if out.IsZero() {
		out = yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	} else if out.Kind != yaml.SequenceNode {
		return yaml.Node{}, fmt.Errorf("the subscription's proxy groups are not a list (kind %d)", node.Kind)
	}
	var encoded yaml.Node
	if err := encoded.Encode(item); err != nil {
		return yaml.Node{}, err
	}
	out.Content = append(slices.Clone(out.Content), &encoded)
	return out, nil
}
