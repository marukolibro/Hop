package service

import (
	"errors"
	"fmt"

	"github.com/metacubex/mihomo/common/convert"
	"go.yaml.in/yaml/v3"
)

// subscriptionProfile is a downloaded subscription, read for building a
// config: a Clash profile with its own sections, or a list of node links
// (base64 or plain ss://, vmess://, trojan://, ...) turned into proxies.
type subscriptionProfile struct {
	// doc holds a Clash profile's top-level sections; nil for a link list.
	doc map[string]yaml.Node
	// proxies and providers are the nodes, whichever form they came in.
	proxies   yaml.Node
	providers yaml.Node
	// nodeNames are the proxies' names, providerNames the proxy providers':
	// what Hop's template groups the nodes by.
	nodeNames     []string
	providerNames []string
}

// isLinkList says whether the profile came as node links: it has no groups
// or rules of its own.
func (p subscriptionProfile) isLinkList() bool {
	return p.doc == nil
}

// readProfile reads a subscription's body. A Clash profile is a YAML map
// with proxies or proxy providers; anything else is read as node links, with
// the converter the pinned core itself uses for link-list providers, so Hop
// accepts exactly what the core would.
func readProfile(raw []byte) (subscriptionProfile, error) {
	var doc map[string]yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err == nil && doc != nil {
		if err := requireProxies(doc); err != nil {
			return subscriptionProfile{}, err
		}
		if err := expandAnchors(doc); err != nil {
			return subscriptionProfile{}, err
		}
		return clashProfile(doc)
	}
	return linkListProfile(raw)
}

// Limits on expanding a profile's YAML aliases, which a hand-written profile
// stays far under: they stop a profile whose aliases refer to themselves, or
// multiply each other, from filling memory.
const (
	maxAliasDepth = 64
	maxAliasNodes = 1 << 20
)

// expandAnchors replaces every alias in the profile's sections with a copy of
// the node it names, and drops the anchors. Hop writes each section it keeps
// on its own, without the sections it drops, so an alias left in place would
// name an anchor that no longer exists: a template under a key of the
// profile's own, merged into the groups with "<<: *name", is the usual case.
func expandAnchors(doc map[string]yaml.Node) error {
	nodes := 0
	for key, node := range doc {
		if err := expandAliases(&node, 0, &nodes); err != nil {
			return fmt.Errorf("read the YAML anchors under %q: %w", key, err)
		}
		doc[key] = node
	}
	return nil
}

// expandAliases expands the aliases in node and below it, in place. depth is
// how many aliases deep node is, and nodes counts the nodes expanded so far.
func expandAliases(node *yaml.Node, depth int, nodes *int) error {
	*nodes++
	if *nodes > maxAliasNodes {
		return errors.New("the aliases expand to too many nodes")
	}
	if node.Kind == yaml.AliasNode {
		if node.Alias == nil {
			return fmt.Errorf("the alias %q names no anchor", node.Value)
		}
		if depth >= maxAliasDepth {
			return errors.New("the aliases refer to each other too deeply")
		}
		*node = copyNode(node.Alias)
		return expandAliases(node, depth+1, nodes)
	}
	node.Anchor = ""
	for _, child := range node.Content {
		if err := expandAliases(child, depth, nodes); err != nil {
			return err
		}
	}
	return nil
}

// copyNode is a deep copy of node, so expanding an alias never changes the
// node it names.
func copyNode(node *yaml.Node) yaml.Node {
	out := *node
	if len(node.Content) > 0 {
		out.Content = make([]*yaml.Node, len(node.Content))
		for i, child := range node.Content {
			c := copyNode(child)
			out.Content[i] = &c
		}
	}
	return out
}

// clashProfile reads the nodes of a Clash profile.
func clashProfile(doc map[string]yaml.Node) (subscriptionProfile, error) {
	p := subscriptionProfile{doc: doc, proxies: doc["proxies"], providers: doc["proxy-providers"]}
	if !p.proxies.IsZero() {
		var named []struct {
			Name string `yaml:"name"`
		}
		if err := p.proxies.Decode(&named); err != nil {
			return subscriptionProfile{}, fmt.Errorf("read the subscription's proxies: %w", err)
		}
		for _, n := range named {
			p.nodeNames = append(p.nodeNames, n.Name)
		}
	}
	if !p.providers.IsZero() {
		var providers map[string]yaml.Node
		if err := p.providers.Decode(&providers); err != nil {
			return subscriptionProfile{}, fmt.Errorf("read the subscription's proxy providers: %w", err)
		}
		p.providerNames = sortedKeys(providers)
	}
	return p, nil
}

// linkListProfile turns node links into proxies.
func linkListProfile(raw []byte) (subscriptionProfile, error) {
	proxies, err := convert.ConvertsV2Ray(raw)
	if err != nil {
		return subscriptionProfile{}, fmt.Errorf("the subscription is neither a Clash profile nor a list of node links: %w", err)
	}
	if len(proxies) == 0 {
		return subscriptionProfile{}, fmt.Errorf("the subscription is neither a Clash profile nor a list of node links (ss://, vmess://, vless://, trojan://, hysteria2://, tuic://, anytls://)")
	}
	var p subscriptionProfile
	if err := p.proxies.Encode(proxies); err != nil {
		return subscriptionProfile{}, fmt.Errorf("encode the converted nodes: %w", err)
	}
	for _, proxy := range proxies {
		name, ok := proxy["name"].(string)
		if !ok {
			return subscriptionProfile{}, fmt.Errorf("a converted node has no name (type=%v server=%v)", proxy["type"], proxy["server"])
		}
		p.nodeNames = append(p.nodeNames, name)
	}
	return p, nil
}
