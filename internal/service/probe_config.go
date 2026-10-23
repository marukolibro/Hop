package service

import (
	"errors"
	"fmt"
	"slices"

	"go.yaml.in/yaml/v3"

	"hop/internal/model"
)

// probeListenerName names the core's listener the unlock checks go through.
const probeListenerName = "hop-probe"

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

// probeGroup is a select group over every node and provider, where the check
// picks the node under test.
type probeGroup struct {
	Name       string `yaml:"name"`
	Type       string `yaml:"type"`
	IncludeAll bool   `yaml:"include-all"`
}

// addProbe gives cfg the probe listener on bindAddress:port and the group it
// sends through, after the profile's own groups. Without a port the config
// has neither.
func addProbe(cfg *runtimeConfig, bindAddress string, port int) error {
	if port == 0 {
		return nil
	}
	groups, err := appendMapping(cfg.ProxyGroups, probeGroup{Name: model.ProbeGroup, Type: configSelect, IncludeAll: true})
	if err != nil {
		return fmt.Errorf("add the probe group: %w", err)
	}
	cfg.ProxyGroups = groups
	cfg.Listeners = []probeListener{{
		Name: probeListenerName, Type: "http", Listen: bindAddress, Port: port, Proxy: model.ProbeGroup,
	}}
	return nil
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
		return yaml.Node{}, errors.New("the subscription's proxy groups are not a list")
	}
	var encoded yaml.Node
	if err := encoded.Encode(item); err != nil {
		return yaml.Node{}, err
	}
	out.Content = append(slices.Clone(out.Content), &encoded)
	return out, nil
}
