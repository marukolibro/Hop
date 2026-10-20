package service

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"
)

// Hop's template: the groups and rules a subscription runs with when the
// user chose Hop's rules over the provider's. The nodes are the provider's;
// everything else is decided here, from the geodata that ships with Hop, so
// it works offline and the same for every provider.
const (
	groupSelect    = "节点选择"
	groupAuto      = "自动选择"
	groupOther     = "其他地区"
	groupAI        = "AI"
	groupMedia     = "流媒体"
	groupTelegram  = "Telegram"
	groupGoogle    = "谷歌"
	groupApple     = "苹果"
	groupMicrosoft = "微软"

	// configSelect and configURLTest are the group types as a config file
	// writes them, which differ from the names the controller reports
	// (model.GroupSelector, model.GroupURLTest).
	configSelect  = "select"
	configURLTest = "url-test"

	healthCheckURL      = "https://www.gstatic.com/generate_204"
	healthCheckInterval = 300
	urlTestTolerance    = 50
)

// templateRegion is one region group of the template: the nodes whose names
// carry one of its keywords. Names lead with the flag, so the dashboard
// shows them as region groups.
type templateRegion struct {
	group    string
	keywords []string
}

// templateRegions are tried in order; a node joins the first that matches.
// Short ASCII keywords ("hk", "us") must be a word of the name by itself,
// the way ServiceOf matches them.
var templateRegions = []templateRegion{
	{"🇭🇰 香港", []string{"🇭🇰", "香港", "hong kong", "hongkong", "hk"}},
	// Named with the flag Hop shows for Taiwan (see the handler's
	// flagReplacements); nodes still match by either flag or the name.
	{"🇨🇳 台湾", []string{"🇹🇼", "台湾", "臺灣", "taiwan", "tw"}},
	{"🇯🇵 日本", []string{"🇯🇵", "日本", "东京", "大阪", "japan", "tokyo", "osaka", "jp"}},
	{"🇸🇬 新加坡", []string{"🇸🇬", "新加坡", "狮城", "singapore", "sg"}},
	{"🇺🇸 美国", []string{"🇺🇸", "美国", "united states", "america", "los angeles", "san jose", "seattle", "new york", "dallas", "chicago", "usa", "us"}},
}

// aiPreference is the AI group's options in order, the first that has
// nodes chosen by default: Singapore first. Hong Kong is left out, since the
// AI services refuse it.
var aiPreference = []string{"🇸🇬 新加坡", "🇺🇸 美国", "🇯🇵 日本", "🇨🇳 台湾", groupOther}

// templateGroup is one proxy group of the template, in the core's keys.
type templateGroup struct {
	Name      string   `yaml:"name"`
	Type      string   `yaml:"type"`
	Proxies   []string `yaml:"proxies,omitempty"`
	Use       []string `yaml:"use,omitempty"`
	URL       string   `yaml:"url,omitempty"`
	Interval  int      `yaml:"interval,omitempty"`
	Tolerance int      `yaml:"tolerance,omitempty"`
	Lazy      bool     `yaml:"lazy,omitempty"`
}

// templateRegionOf names the template region group a node belongs to, or ""
// when its name says no region the template has a group for.
func templateRegionOf(name string) string {
	lower := strings.ToLower(name)
	words := strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, region := range templateRegions {
		for _, keyword := range region.keywords {
			if matchesKeyword(lower, words, keyword) {
				return region.group
			}
		}
	}
	return ""
}

// hopGroups builds the template's groups over the profile's nodes and proxy
// providers: 节点选择 first (the dashboard's main group), 自动选择, a
// url-test group per region that has nodes, then the service groups.
// Entries providers add to show account details are kept out of the
// url-test groups: they are not proxies.
func hopGroups(nodeNames []string, providerNames []string) []templateGroup {
	byRegion := map[string][]string{}
	var real, other []string
	for _, name := range nodeNames {
		if isPlanNote(name) {
			continue
		}
		real = append(real, name)
		if region := templateRegionOf(name); region != "" {
			byRegion[region] = append(byRegion[region], name)
		} else {
			other = append(other, name)
		}
	}
	urlTest := func(name string, proxies []string, use []string) templateGroup {
		return templateGroup{
			Name: name, Type: configURLTest, Proxies: proxies, Use: use,
			URL: healthCheckURL, Interval: healthCheckInterval, Tolerance: urlTestTolerance, Lazy: true,
		}
	}

	var regions []templateGroup
	for _, region := range templateRegions {
		if nodes := byRegion[region.group]; len(nodes) > 0 {
			regions = append(regions, urlTest(region.group, nodes, nil))
		}
	}
	// Without any region group, 其他地区 would only repeat 自动选择.
	if len(regions) > 0 && len(other) > 0 {
		regions = append(regions, urlTest(groupOther, other, nil))
	}
	regionNames := make([]string, 0, len(regions))
	for _, g := range regions {
		regionNames = append(regionNames, g.Name)
	}

	selectOptions := append(append([]string{groupAuto}, regionNames...), nodeNames...)
	selectOptions = append(selectOptions, "DIRECT")
	groups := []templateGroup{
		{Name: groupSelect, Type: configSelect, Proxies: selectOptions, Use: providerNames},
		urlTest(groupAuto, real, providerNames),
	}
	groups = append(groups, regions...)

	routes := append([]string{groupSelect}, regionNames...)
	var aiRoutes []string
	for _, name := range aiPreference {
		if slices.Contains(regionNames, name) {
			aiRoutes = append(aiRoutes, name)
		}
	}
	aiRoutes = append(aiRoutes, groupSelect)
	return append(groups,
		templateGroup{Name: groupAI, Type: configSelect, Proxies: aiRoutes},
		templateGroup{Name: groupMedia, Type: configSelect, Proxies: routes},
		templateGroup{Name: groupTelegram, Type: configSelect, Proxies: routes},
		templateGroup{Name: groupGoogle, Type: configSelect, Proxies: routes},
		templateGroup{Name: groupApple, Type: configSelect, Proxies: []string{"DIRECT", groupSelect}},
		templateGroup{Name: groupMicrosoft, Type: configSelect, Proxies: []string{"DIRECT", groupSelect}},
	)
}

// hopRules are the template's rules: LAN direct, the service groups by the
// shipped geosite and geoip databases; then blocked and known foreign sites
// through a node, known Chinese sites direct, and anything no list knows by
// the country of its address.
var hopRules = []string{
	"GEOSITE,private,DIRECT",
	"GEOIP,private,DIRECT,no-resolve",
	"GEOSITE,openai," + groupAI,
	"GEOSITE,anthropic," + groupAI,
	"GEOSITE,google-gemini," + groupAI,
	"GEOSITE,category-ai-!cn," + groupAI,
	"GEOSITE,youtube," + groupMedia,
	"GEOSITE,netflix," + groupMedia,
	"GEOSITE,disney," + groupMedia,
	"GEOSITE,tiktok," + groupMedia,
	"GEOSITE,telegram," + groupTelegram,
	"GEOIP,telegram," + groupTelegram + ",no-resolve",
	"GEOSITE,google," + groupGoogle,
	"GEOSITE,apple," + groupApple,
	"GEOSITE,microsoft," + groupMicrosoft,
	"GEOSITE,gfw," + groupSelect,
	"GEOSITE,geolocation-!cn," + groupSelect,
	"GEOSITE,cn,DIRECT",
	"GEOIP,CN,DIRECT",
	"MATCH," + groupSelect,
}

// applyHopTemplate puts the template's groups and rules under cfg, over the
// profile's nodes; the provider's own groups, rules and rule providers are
// left out.
func applyHopTemplate(cfg *runtimeConfig, p subscriptionProfile) error {
	cfg.Proxies, cfg.ProxyProviders = p.proxies, p.providers
	if err := cfg.ProxyGroups.Encode(hopGroups(p.nodeNames, p.providerNames)); err != nil {
		return fmt.Errorf("build the template's proxy groups: %w", err)
	}
	if err := cfg.Rules.Encode(hopRules); err != nil {
		return fmt.Errorf("build the template's rules: %w", err)
	}
	cfg.RuleProviders, cfg.SubRules = yaml.Node{}, yaml.Node{}
	return nil
}
