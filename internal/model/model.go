// Package model holds the types shared across Hop's layers.
package model

import "time"

// Group types as the core reports them.
const (
	GroupSelector    = "Selector"
	GroupURLTest     = "URLTest"
	GroupFallback    = "Fallback"
	GroupLoadBalance = "LoadBalance"
	GroupRelay       = "Relay"
)

// ProxyGroup is a policy group of the core.
type ProxyGroup struct {
	Name     string
	Type     string
	Selected string
	Options  []string
}

// Snapshot is the core's groups, in the order the profile declares them,
// and the latest delay measured for each proxy.
type Snapshot struct {
	Groups []ProxyGroup
	// Global is the core's own group that global mode sends all traffic
	// through.
	Global ProxyGroup
	// Delays holds milliseconds by proxy name; 0 means the last test failed
	// and a missing name was never tested.
	Delays map[string]int
	// Facts describes each proxy, by name.
	Facts map[string]ProxyFacts
}

// ProxyFacts is what the core says about a proxy itself.
type ProxyFacts struct {
	// Protocol is the core's name for it, such as "Shadowsocks" or "AnyTLS".
	Protocol string
	// UDP reports whether the proxy carries UDP.
	UDP bool
}

// Traffic is one second of throughput reported by the core, in bytes per
// second.
type Traffic struct {
	Up   int64
	Down int64
}

// TrafficHistory is the most recent traffic samples, oldest first.
type TrafficHistory struct {
	Up   []float32
	Down []float32
}

// Download is what a provider returned for a subscription link: the Clash
// YAML and its subscription-userinfo header, "" when it sent none.
type Download struct {
	Body     []byte
	UserInfo string
}

// Subscription is a profile downloaded from a provider: where it came from,
// the Clash YAML it returned as received, its usage header and when it was
// downloaded.
type Subscription struct {
	URL       string
	Raw       []byte
	UserInfo  string
	UpdatedAt time.Time
}

// Usage is the traffic and term of the account, as the provider reports
// them. Total and Expire are zero when the provider gives no limit.
type Usage struct {
	Upload   int64
	Download int64
	Total    int64
	Expire   time.Time
}

// SubscriptionSummary is what the dashboard shows of the subscription.
type SubscriptionSummary struct {
	Host      string
	UpdatedAt time.Time
	Usage     Usage
	// HasUsage is set when the provider sent usage with the profile.
	HasUsage bool
}

// Delay is a proxy's latest delay test. Millis is 0 when a test ran and got
// no answer.
type Delay struct {
	Millis   int
	Measured bool
}

// NodeView is a proxy as the node grid shows it, its name taken apart.
type NodeView struct {
	// Name is the proxy's full name, which selecting it sends to the core.
	Name string
	// Flag and Region place the node, such as "🇯🇵" and "日本"; both are ""
	// for names without a flag.
	Flag   string
	Region string
	// Title is the name without its flag, region and the lines after "|",
	// such as "高速01" for "🇯🇵日本高速01|CTCU|0.5x".
	Title      string
	Tags       []string
	Multiplier string
	Delay      Delay
	// Protocol and UDP are the core's facts about a proxy; both are zero
	// for groups and built-in outbounds.
	Protocol string
	UDP      bool
	// PlanNote marks an entry whose name is account details, such as traffic
	// left, that the provider added as a proxy.
	PlanNote bool
	// Via says where an option that is not a proxy sends traffic: the node a
	// group picks now, or that DIRECT skips the proxy. "" for proxies.
	Via string
}

// RegionView is the proxies of one country or area.
type RegionView struct {
	Flag  string
	Name  string
	Nodes []NodeView
}

// What a group is for, as the sidebar sorts groups.
const (
	GroupKindMain    = "main"
	GroupKindAuto    = "auto"
	GroupKindRouting = "routing"
	GroupKindRegion  = "region"
	GroupKindOther   = "other"
)

// GroupView is a policy group as the dashboard shows it.
type GroupView struct {
	// Name identifies the group to the core; Label is what the tab shows.
	Name       string
	Label      string
	Type       string
	Selected   string
	Selectable bool
	// Kind is what the group is for, one of the GroupKind constants.
	Kind string
	// Pick words the group's direct choice, and Exit is the proxy that
	// choice leads to through nested groups (zero when none).
	Pick    string
	Exit    NodeView
	Regions []RegionView
	// Others are the options that belong to no region: built-in outbounds
	// and other groups.
	Others []NodeView
}

// RouteHop is one step from the main group to the proxy traffic leaves by.
type RouteHop struct {
	Name  string
	Node  NodeView
	Group bool
}

// ProxyView is everything the dashboard shows about the profile.
type ProxyView struct {
	// Mode is the outbound mode the view was built for.
	Mode   string
	Groups []GroupView
	// Plan is what the provider says about the account, such as traffic
	// left or the expiry date, taken from entries that are not proxies.
	Plan  []string
	Route []RouteHop
}

// Outbound modes of the core.
const (
	ModeRule   = "rule"
	ModeGlobal = "global"
	ModeDirect = "direct"
)

// Connection is one connection the core is carrying.
type Connection struct {
	ID string
	// Host is the domain asked for, or the destination IP without one.
	Host    string
	Port    string
	Network string
	// Process is the name of the local program that opened it, "" when the
	// core could not tell.
	Process string
	// Rule is the rule that matched, with its payload, such as
	// "DomainSuffix(google.com)".
	Rule string
	// Chain is the path the connection takes, from the outermost group to
	// the proxy it leaves by.
	Chain    []string
	Upload   int64
	Download int64
	Start    time.Time
}

// Connections is the core's connection table at one moment.
type Connections struct {
	Active        []Connection
	UploadTotal   int64
	DownloadTotal int64
}

// Log levels of the core, from the most to the least verbose.
const (
	LogDebug   = "debug"
	LogInfo    = "info"
	LogWarning = "warning"
	LogError   = "error"
)

// LogEntry is one line of the core's log.
type LogEntry struct {
	Level   string
	Message string
	Time    time.Time
}
