package service

import (
	"regexp"
	"slices"
	"strings"
	"unicode"

	"hop/internal/model"
)

const (
	otherRegionName  = "其他"
	planNodeLabel    = "机场信息节点"
	globalGroupLabel = "全局"
)

// countryNames names the regions providers use most, by flag.
var countryNames = map[string]string{
	"🇭🇰": "香港", "🇹🇼": "台湾", "🇯🇵": "日本", "🇸🇬": "新加坡", "🇰🇷": "韩国",
	"🇺🇸": "美国", "🇬🇧": "英国", "🇩🇪": "德国", "🇫🇷": "法国", "🇳🇱": "荷兰",
	"🇨🇦": "加拿大", "🇦🇺": "澳大利亚", "🇷🇺": "俄罗斯", "🇮🇳": "印度", "🇹🇷": "土耳其",
	"🇲🇾": "马来西亚", "🇹🇭": "泰国", "🇵🇭": "菲律宾", "🇻🇳": "越南", "🇦🇷": "阿根廷",
	"🇧🇷": "巴西", "🇮🇩": "印度尼西亚", "🇲🇴": "澳门", "🇨🇳": "中国", "🇨🇭": "瑞士",
}

// regionWords are the ways providers write a region at the start of a
// node's name, longest first so "South Korea" wins over "Korea". A name
// keeps the provider's own wording as its region.
var regionWords = sortedLongestFirst([]string{
	"香港", "台湾", "臺灣", "日本", "新加坡", "狮城", "韩国", "美国", "英国", "德国", "法国", "荷兰",
	"加拿大", "澳大利亚", "澳洲", "俄罗斯", "印度", "土耳其", "马来西亚", "泰国", "菲律宾", "越南",
	"阿根廷", "巴西", "印度尼西亚", "印尼", "澳门", "中国", "瑞士",
	"Hong Kong", "Taiwan", "Japan", "Singapore", "South Korea", "Korea", "United States", "USA",
	"United Kingdom", "UK", "Germany", "France", "Netherlands", "Canada", "Australia", "Russia",
	"India", "Turkey", "Malaysia", "Thailand", "Philippines", "Vietnam", "Argentina", "Brazil",
	"Indonesia", "Macau", "Macao", "China", "Switzerland",
})

func sortedLongestFirst(words []string) []string {
	out := slices.Clone(words)
	slices.SortStableFunc(out, func(a, b string) int { return len(b) - len(a) })
	return out
}

var (
	multiplierPattern = regexp.MustCompile(`(?i)^\d+(\.\d+)?\s*[x×]$`)
	// planKeywords mark entries providers add to the proxy list to show
	// account details; they are not proxies.
	planKeywords = []string{"剩余", "流量", "到期", "过期", "重置", "官网", "expire", "traffic", "reset"}
)

// BuildView turns a snapshot of the core into what the dashboard shows for
// mode. In rule mode the route starts from the profile's first group; in
// global mode from the core's GLOBAL group, which then leads the tabs; in
// direct mode traffic skips every proxy.
func BuildView(snapshot model.Snapshot, mode string) model.ProxyView {
	byName := make(map[string]model.ProxyGroup, len(snapshot.Groups)+1)
	for _, g := range snapshot.Groups {
		byName[g.Name] = g
	}
	groups := snapshot.Groups
	if mode == model.ModeGlobal {
		groups = append([]model.ProxyGroup{snapshot.Global}, snapshot.Groups...)
		byName[snapshot.Global.Name] = snapshot.Global
	}
	view := model.ProxyView{Mode: mode, Groups: make([]model.GroupView, len(groups))}
	for i, g := range groups {
		view.Groups[i] = buildGroup(g, byName, snapshot)
		view.Groups[i].Kind = groupKind(g, i, byName)
		view.Groups[i].Pick = pickLabel(g.Selected)
		if exit := finalPick(g.Name, byName); exit != "" {
			view.Groups[i].Exit = describeNode(exit, snapshot)
		}
	}
	if mode == model.ModeGlobal {
		view.Groups[0].Label = globalGroupLabel
	}
	view.Plan = planNotes(snapshot.Groups)
	switch mode {
	case model.ModeDirect:
		view.Route = []model.RouteHop{{Name: "DIRECT", Node: optionNode("DIRECT", byName, snapshot)}}
	default:
		view.Route = resolveRoute(groups, byName, snapshot)
	}
	return view
}

func buildGroup(g model.ProxyGroup, groups map[string]model.ProxyGroup, snapshot model.Snapshot) model.GroupView {
	view := model.GroupView{Name: g.Name, Label: g.Name, Type: g.Type, Selected: g.Selected, Selectable: g.Type == model.GroupSelector, Service: ServiceOf(g.Name)}
	regionIndex := map[string]int{}
	// A profile may list an option twice; it is still one choice.
	seen := map[string]bool{}
	for _, name := range g.Options {
		if isPlanNote(name) || seen[name] {
			continue
		}
		seen[name] = true
		if _, isGroup := groups[name]; isGroup || name == "DIRECT" || name == "REJECT" {
			view.Others = append(view.Others, optionNode(name, groups, snapshot))
			continue
		}
		node := describeNode(name, snapshot)
		flag, isRegional := LeadingFlag(name)
		key, label := flag, node.Region
		if !isRegional {
			key, label = otherRegionName, otherRegionName
		}
		i, known := regionIndex[key]
		if !known {
			i = len(view.Regions)
			regionIndex[key] = i
			view.Regions = append(view.Regions, model.RegionView{Flag: flag, Name: label})
		}
		view.Regions[i].Nodes = append(view.Regions[i].Nodes, node)
	}
	return view
}

// groupKind sorts a group by what it is for: the profile's first group is
// the main route; groups that pick by delay are automatic; a selector over
// other groups only, such as "Netflix", routes a service; a selector named
// after a flag lists a region's nodes.
func groupKind(g model.ProxyGroup, index int, groups map[string]model.ProxyGroup) string {
	switch {
	case index == 0:
		return model.GroupKindMain
	case slices.Contains(autoGroupTypes, g.Type):
		return model.GroupKindAuto
	case g.Type == model.GroupSelector && onlyRoutes(g.Options, groups):
		return model.GroupKindRouting
	}
	if _, isRegional := LeadingFlag(g.Name); isRegional {
		return model.GroupKindRegion
	}
	return model.GroupKindOther
}

// onlyRoutes reports options that are all groups or built-in outbounds.
func onlyRoutes(options []string, groups map[string]model.ProxyGroup) bool {
	if len(options) == 0 {
		return false
	}
	for _, name := range options {
		if _, isGroup := groups[name]; !isGroup && name != "DIRECT" && name != "REJECT" {
			return false
		}
	}
	return true
}

// pickLabel words a group's direct choice for the sidebar.
func pickLabel(selected string) string {
	switch selected {
	case "DIRECT":
		return "直连"
	case "REJECT":
		return "拒绝"
	}
	node := ParseNode(selected, nil)
	if node.Flag == "" {
		return selected
	}
	return strings.TrimSpace(node.Flag + " " + node.Region + " " + node.Title)
}

// optionNode describes an option that is not a proxy: a group shows the
// node it picks now and that node's delay.
func optionNode(name string, groups map[string]model.ProxyGroup, snapshot model.Snapshot) model.NodeView {
	node := model.NodeView{Name: name, Title: name}
	// A region's group names its flag once, in the line under the title.
	if flag, isRegional := LeadingFlag(name); isRegional {
		node.Title = strings.TrimSpace(strings.TrimPrefix(name, flag))
	}
	switch name {
	case "DIRECT":
		node.Via = "不经过代理"
		return node
	case "REJECT":
		node.Via = "拒绝连接"
		return node
	}
	pick := finalPick(name, groups)
	if pick == "" {
		return node
	}
	picked := ParseNode(pick, snapshot.Delays)
	node.Via = strings.TrimSpace(picked.Flag + " " + picked.Region + " " + picked.Title)
	if picked.PlanNote {
		node.Via = planNodeLabel
	}
	node.Delay = picked.Delay
	return node
}

// finalPick follows a group's choice through nested groups to a proxy, ""
// when the chain ends without one.
func finalPick(name string, groups map[string]model.ProxyGroup) string {
	current := name
	for range len(groups) + 1 {
		g, isGroup := groups[current]
		if !isGroup {
			return current
		}
		current = g.Selected
	}
	return ""
}

// describeNode is ParseNode with what the core knows of the proxy: its
// protocol and whether it carries UDP.
func describeNode(name string, snapshot model.Snapshot) model.NodeView {
	node := ParseNode(name, snapshot.Delays)
	facts := snapshot.Facts[name]
	node.Protocol, node.UDP = facts.Protocol, facts.UDP
	return node
}

// ParseNode takes a proxy name apart, as "🇯🇵日本高速01|CTCU|0.5x" into the
// title "高速01", the tag "CTCU" and the multiplier "0.5x".
func ParseNode(name string, delays map[string]int) model.NodeView {
	flag, isRegional := LeadingFlag(name)
	rest := strings.TrimSpace(strings.TrimPrefix(name, flag))
	parts := strings.Split(rest, "|")
	title, region := splitRegion(strings.TrimSpace(parts[0]))
	node := model.NodeView{Name: name, Title: title, PlanNote: isPlanNote(name)}
	if isRegional {
		node.Flag = flag
		node.Region = region
		if region == "" {
			node.Region = regionName(flag, isRegional)
		}
	}
	for _, part := range parts[1:] {
		part = strings.TrimSpace(part)
		switch {
		case part == "":
		case multiplierPattern.MatchString(part):
			node.Multiplier = strings.ToLower(strings.ReplaceAll(part, " ", ""))
		default:
			node.Tags = append(node.Tags, part)
		}
	}
	if ms, ok := delays[name]; ok {
		node.Delay = model.Delay{Millis: ms, Measured: true}
	}
	return node
}

// splitRegion takes the region a title starts with off it, as "Hong Kong
// 01" into "01" and "Hong Kong". A title that is only a region, or names
// none, stays whole and gives no region.
func splitRegion(title string) (string, string) {
	for _, word := range regionWords {
		rest, found := strings.CutPrefix(title, word)
		rest = strings.TrimSpace(rest)
		if found && rest != "" {
			return rest, word
		}
	}
	return title, ""
}

// LeadingFlag returns the flag emoji a name starts with: two regional
// indicator symbols.
func LeadingFlag(name string) (string, bool) {
	runes := []rune(name)
	if len(runes) < 2 || !isRegionalIndicator(runes[0]) || !isRegionalIndicator(runes[1]) {
		return "", false
	}
	return string(runes[:2]), true
}

func isRegionalIndicator(r rune) bool {
	return r >= 0x1F1E6 && r <= 0x1F1FF
}

func regionName(flag string, isRegional bool) string {
	if !isRegional {
		return otherRegionName
	}
	if name, ok := countryNames[flag]; ok {
		return name
	}
	// An unlisted flag still names its region: show its two letters.
	letters := make([]rune, 0, 2)
	for _, r := range flag {
		letters = append(letters, 'A'+(r-0x1F1E6))
	}
	return string(letters)
}

func isPlanNote(name string) bool {
	if _, isRegional := LeadingFlag(name); isRegional {
		return false
	}
	lower := strings.ToLower(name)
	return slices.ContainsFunc(planKeywords, func(k string) bool { return strings.Contains(lower, k) })
}

// planNotes collects the account details providers list as proxies, once
// each, in order.
func planNotes(groups []model.ProxyGroup) []string {
	var notes []string
	for _, g := range groups {
		for _, name := range g.Options {
			if isPlanNote(name) && !slices.Contains(notes, name) {
				notes = append(notes, strings.TrimFunc(name, unicode.IsSpace))
			}
		}
	}
	return notes
}

// resolveRoute follows the first group's choice through nested groups to
// the proxy traffic actually leaves by.
func resolveRoute(groups []model.ProxyGroup, byName map[string]model.ProxyGroup, snapshot model.Snapshot) []model.RouteHop {
	if len(groups) == 0 {
		return nil
	}
	var route []model.RouteHop
	current := groups[0].Name
	for len(route) <= len(byName) {
		g, isGroup := byName[current]
		if !isGroup {
			return append(route, model.RouteHop{Name: current, Node: describeNode(current, snapshot)})
		}
		route = append(route, model.RouteHop{Name: current, Group: true})
		if g.Selected == "" {
			return route
		}
		current = g.Selected
	}
	// A cycle of groups: show the steps found so far.
	return route
}

// QuickRoutes are the choices of group the menu bar panel offers: the
// groups it lists, such as 自动选择 and the regions of Hop's template, or,
// for a profile whose main group lists nodes only, its n fastest nodes.
// DIRECT and REJECT are left out: the outbound mode covers going direct.
func QuickRoutes(group model.GroupView, n int) []model.NodeView {
	var routes []model.NodeView
	for _, option := range group.Others {
		if option.Name != "DIRECT" && option.Name != "REJECT" {
			routes = append(routes, option)
		}
	}
	if len(routes) > 0 {
		return routes
	}
	return FastestNodes(group, n)
}

// FastestNodes returns up to n of group's nodes that answered their last
// delay test, fastest first. Account-detail entries are left out.
func FastestNodes(group model.GroupView, n int) []model.NodeView {
	var measured []model.NodeView
	for _, region := range group.Regions {
		for _, node := range region.Nodes {
			if node.Delay.Measured && node.Delay.Millis > 0 && !node.PlanNote {
				measured = append(measured, node)
			}
		}
	}
	slices.SortStableFunc(measured, func(a, b model.NodeView) int { return a.Delay.Millis - b.Delay.Millis })
	return measured[:min(n, len(measured))]
}
