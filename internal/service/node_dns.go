package service

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// nodeEntries is how a subscription sends its nodes through the provider's
// nearest entry points, which public DNS does not return: hosts entries
// that point a node's server at another name (or at addresses), and the
// provider's own encrypted resolvers, the only ones that answer those
// names with the near entries. Measured on one provider, the same nodes
// were 24-28 ms away this way and 77-111 ms through public DNS.
type nodeEntries struct {
	// hosts maps a node's server to another name or to addresses, as the
	// subscription wrote it. Only names the subscription's own proxies use
	// as their server are kept, so a subscription cannot redirect a site.
	hosts map[string]yaml.Node
	// aliases are the names hosts points at; they resolve through resolvers.
	aliases []string
	// resolvers are the subscription's encrypted DNS servers (DoH, DoT,
	// DoQ), the ones that know the aliases.
	resolvers []string
}

// readNodeEntries reads a Clash profile's node entries. A link list, or a
// profile without hosts for its nodes, has none.
func readNodeEntries(p subscriptionProfile) (nodeEntries, error) {
	node, ok := p.doc["hosts"]
	if p.isLinkList() || !ok || p.proxies.IsZero() {
		return nodeEntries{}, nil
	}
	var hosts map[string]yaml.Node
	if err := node.Decode(&hosts); err != nil {
		return nodeEntries{}, fmt.Errorf("read the subscription's hosts: %w", err)
	}
	var proxies []struct {
		Server string `yaml:"server"`
	}
	if err := p.proxies.Decode(&proxies); err != nil {
		return nodeEntries{}, fmt.Errorf("read the subscription's proxy servers: %w", err)
	}
	servers := make(map[string]bool, len(proxies))
	for _, proxy := range proxies {
		servers[proxy.Server] = true
	}
	entries := nodeEntries{hosts: map[string]yaml.Node{}}
	for _, name := range sortedKeys(hosts) {
		value := hosts[name]
		if !servers[name] {
			continue
		}
		if alias, ok := hostsAlias(value); ok {
			entries.hosts[name] = value
			if !slices.Contains(entries.aliases, alias) {
				entries.aliases = append(entries.aliases, alias)
			}
			continue
		}
		if hostsAddresses(value) {
			entries.hosts[name] = value
		}
	}
	if len(entries.hosts) == 0 {
		return nodeEntries{}, nil
	}
	resolvers, err := encryptedResolvers(p.doc["dns"])
	if err != nil {
		return nodeEntries{}, err
	}
	entries.resolvers = resolvers
	return entries, nil
}

// applyNodeEntries puts entries under cfg. Addresses work through hosts
// alone; an alias needs the core's DNS server, the only part of the core
// that follows a name to another name, so node servers are then looked up
// through the core's own listener on listen, and the aliases through the
// provider's resolvers. Under TUN the names must keep real addresses.
func applyNodeEntries(cfg *runtimeConfig, entries nodeEntries, servers []string, listen string) error {
	if len(entries.hosts) == 0 {
		return nil
	}
	cfg.Hosts = entries.hosts
	if len(entries.aliases) == 0 {
		return nil
	}
	if listen == "" {
		return errors.New("the subscription points its nodes at other names, but Hop has no DNS listener for them")
	}
	cfg.DNS.Listen = listen
	cfg.DNS.ProxyServerNameserver = []string{"udp://" + listen}
	if len(entries.resolvers) > 0 {
		cfg.DNS.NameserverPolicy = make(map[string][]string, len(entries.aliases))
		for _, alias := range entries.aliases {
			cfg.DNS.NameserverPolicy[alias] = entries.resolvers
		}
	}
	if cfg.DNS.EnhancedMode == "fake-ip" {
		filter := slices.Clone(cfg.DNS.FakeIPFilter)
		for _, name := range append(slices.Clone(servers), entries.aliases...) {
			if !slices.Contains(filter, name) {
				filter = append(filter, name)
			}
		}
		cfg.DNS.FakeIPFilter = filter
	}
	return nil
}

// proxyServers names every server the profile's proxies connect to.
func proxyServers(p subscriptionProfile) ([]string, error) {
	if p.proxies.IsZero() {
		return nil, nil
	}
	var proxies []struct {
		Server string `yaml:"server"`
	}
	if err := p.proxies.Decode(&proxies); err != nil {
		return nil, fmt.Errorf("read the subscription's proxy servers: %w", err)
	}
	var servers []string
	for _, proxy := range proxies {
		if proxy.Server == "" || slices.Contains(servers, proxy.Server) {
			continue
		}
		if _, err := netip.ParseAddr(proxy.Server); err == nil {
			continue
		}
		servers = append(servers, proxy.Server)
	}
	return servers, nil
}

// hostsAlias reads a hosts value that names another domain.
func hostsAlias(value yaml.Node) (string, bool) {
	if value.Kind != yaml.ScalarNode || value.Value == "" {
		return "", false
	}
	if _, err := netip.ParseAddr(value.Value); err == nil {
		return "", false
	}
	if strings.ContainsAny(value.Value, " /:*+") {
		return "", false
	}
	return value.Value, true
}

// hostsAddresses says whether a hosts value is one IP address or a list of
// them.
func hostsAddresses(value yaml.Node) bool {
	values := []*yaml.Node{&value}
	if value.Kind == yaml.SequenceNode {
		values = value.Content
	}
	if len(values) == 0 {
		return false
	}
	for _, v := range values {
		if v.Kind != yaml.ScalarNode {
			return false
		}
		if _, err := netip.ParseAddr(v.Value); err != nil {
			return false
		}
	}
	return true
}

// encryptedResolvers reads the subscription's dns.nameserver entries that
// reach a remote server over an encrypted transport. Plain UDP, the system
// resolver and anything on this machine are left out: the subscription
// does not choose how Hop's DNS reaches the network.
func encryptedResolvers(node yaml.Node) ([]string, error) {
	if node.IsZero() {
		return nil, nil
	}
	var dns struct {
		Nameserver []string `yaml:"nameserver"`
	}
	if err := node.Decode(&dns); err != nil {
		return nil, fmt.Errorf("read the subscription's dns: %w", err)
	}
	var resolvers []string
	for _, server := range dns.Nameserver {
		u, err := url.Parse(server)
		if err != nil || u.Hostname() == "" {
			continue
		}
		switch u.Scheme {
		case "https", "tls", "quic":
		default:
			continue
		}
		if addr, err := netip.ParseAddr(u.Hostname()); (err == nil && (addr.IsLoopback() || addr.IsPrivate() || addr.IsUnspecified())) || u.Hostname() == "localhost" {
			continue
		}
		resolvers = append(resolvers, server)
	}
	return resolvers, nil
}
