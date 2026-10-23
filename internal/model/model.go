// Package model holds the types shared across Hop's layers.
package model

import (
	"fmt"
	"net/netip"
	"time"
)

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

// ExitIP is the public address the default route leaves by, and the ISO 3166
// alpha-2 country that address is registered in. It is the user's address:
// keep it in memory only.
type ExitIP struct {
	Address netip.Addr
	Country string
}

// TrafficSplit is how much traffic Hop carried, through a proxy or direct,
// in each direction.
type TrafficSplit struct {
	ProxyUp    int64
	ProxyDown  int64
	DirectUp   int64
	DirectDown int64
}

// TrafficSource names who carried traffic: the subscription in use, or the
// bootstrap config when none is.
type TrafficSource struct {
	ID   string
	Name string
}

// TrafficDay is one local day's traffic for one source, as stored.
type TrafficDay struct {
	// Date is the local calendar day, as "2006-01-02".
	Date       string
	SourceID   string
	SourceName string
	Split      TrafficSplit
}

// TrafficDayView is one of the last thirty days, for the chart.
type TrafficDayView struct {
	Date  string
	Split TrafficSplit
}

// TrafficSourceView is one source's traffic this month.
type TrafficSourceView struct {
	ID    string
	Name  string
	Split TrafficSplit
}

// TrafficView is what the statistics page shows: today, this month, the last
// thirty days and this month per source.
type TrafficView struct {
	Today   TrafficSplit
	Month   TrafficSplit
	Days    []TrafficDayView
	Sources []TrafficSourceView
}

// Download is what a provider returned for a subscription link: the Clash
// YAML and its subscription-userinfo header, "" when it sent none.
type Download struct {
	Body     []byte
	UserInfo string
}

// SubscriptionSource is where a profile's content comes from: a provider's
// link, which refreshes, or a file or pasted text the user added, which does
// not.
type SubscriptionSource string

const (
	// SourceRemote is a subscription downloaded from a provider's link.
	SourceRemote SubscriptionSource = "remote"
	// SourceLocal is a config the user added from a file or pasted text. Hop
	// keeps a copy, so the file may move or vanish.
	SourceLocal SubscriptionSource = "local"
)

// SubscriptionInput is what the user typed about a profile: where it comes
// from, the name to show for it, the link it is downloaded from or the content
// read from a local file or pasted text, and whose rules it runs with. Content
// is empty when a stored local profile keeps its content.
type SubscriptionInput struct {
	Source  SubscriptionSource
	Name    string
	URL     string
	Content []byte
	Rules   RuleSource
}

// RuleSource is where a subscription's policy groups and rules come from:
// the provider's own, or Hop's template in one of its two modes. Either way
// the nodes are the provider's.
type RuleSource string

const (
	// RulesProvider keeps the groups and rules the provider wrote.
	RulesProvider RuleSource = "provider"
	// RulesHop is Hop's template: blocked and known foreign sites through a
	// node, known Chinese sites direct, and the rest by where their address
	// is.
	RulesHop RuleSource = "hop"
)

// Subscription is a stored profile: its stable identity and name, where it
// came from, the Clash YAML or node links it holds as received, its usage
// header (a provider's, empty for a local profile), when it was downloaded,
// and why its last refresh failed ("" when it worked, always "" for a local
// profile, which never refreshes).
type Subscription struct {
	ID        string
	Name      string
	Source    SubscriptionSource
	URL       string
	Raw       []byte
	UserInfo  string
	UpdatedAt time.Time
	// LastError is why the last refresh failed, cleared by a success.
	LastError string
	// AttemptedAt is when a refresh was last tried, successful or not: the
	// automatic refresh is due by this and UpdatedAt, so a failure waits a
	// full interval instead of retrying at every check.
	AttemptedAt time.Time
	// Rules says whose groups and rules the core runs this subscription with.
	Rules RuleSource
}

// Usage is the traffic and term of the account, as the provider reports
// them. Total and Expire are zero when the provider gives no limit.
type Usage struct {
	Upload   int64
	Download int64
	Total    int64
	Expire   time.Time
}

// SubscriptionSummary is what the subscriptions page shows of one stored
// profile. Neither a remote profile's link nor a local profile's content is
// part of it: both are credentials.
type SubscriptionSummary struct {
	ID     string
	Name   string
	Source SubscriptionSource
	// Host is the link's host for a remote profile, "" for a local one.
	Host      string
	UpdatedAt time.Time
	Usage     Usage
	// HasUsage is set when the provider sent usage with the profile.
	HasUsage bool
	// LastError is why the last refresh failed, cleared by a success.
	LastError string
	Rules     RuleSource
}

// SubscriptionsView is all the subscriptions page shows: one summary per stored
// subscription, and the ID of the one the core runs.
type SubscriptionsView struct {
	Items    []SubscriptionSummary
	ActiveID string
}

// Delay is a proxy's latest delay test. Millis is 0 when a test ran and got
// no answer.
type Delay struct {
	Millis   int
	Measured bool
}

// ProbeLanes is how many probe listeners every config carries, so that many
// groups are checked at once. Lane i listens on the instance's probe port plus
// i and sends everything to the group ProbeGroup(i), a select group over every
// group of the profile, in which the check picks the group under test. They
// are Hop's own, so they never show as groups.
const ProbeLanes = 8

// ProbeGroupPrefix starts the name of every probe lane's group.
const ProbeGroupPrefix = "hop-probe-"

// ProbeGroup is the name of probe lane's group.
func ProbeGroup(lane int) string {
	return fmt.Sprintf("%s%d", ProbeGroupPrefix, lane)
}

// UnlockState is what a check found about a service through a node.
type UnlockState uint8

const (
	// UnlockUnknown: the check could not tell, such as a request that failed.
	UnlockUnknown UnlockState = iota
	// UnlockBlocked: the service refuses this node's address.
	UnlockBlocked
	// UnlockPartial: the service works in part, such as Netflix showing only
	// its own productions.
	UnlockPartial
	// UnlockAvailable: the service works.
	UnlockAvailable
)

// UnlockResult is one service's check through a node. Region is the ISO 3166
// alpha-2 code the service placed the node in, "" when it gave none.
type UnlockResult struct {
	State  UnlockState
	Region string
}

// Unlock is what the checks found about the services Hop looks at, through
// one node.
type Unlock struct {
	ChatGPT UnlockResult
	Netflix UnlockResult
	YouTube UnlockResult
}

// NodeView is a proxy as the node grid shows it, its name taken apart.
type NodeView struct {
	// Name is the proxy's full name, which selecting it sends to the core.
	Name string
	// Flag and Region place the node, such as "🇯🇵" and "日本"; both are "" for
	// names without a flag.
	Flag   string
	Region string
	// Title is the name without its flag, region and the lines after "|", such
	// as "高速01" for "🇯🇵日本高速01|CTCU|0.5x".
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
	// Service is the kind of service a group routes, one of the Service
	// constants, "" when its name says none.
	Service string
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

// CoreOrigin says where the running mihomo core came from.
type CoreOrigin string

const (
	// CoreBundled is the core that ships inside Hop.
	CoreBundled CoreOrigin = "bundled"
	// CoreInstalled is a core Hop downloaded when the user upgraded.
	CoreInstalled CoreOrigin = "installed"
	// CoreOverride is a core named by HOP_MIHOMO_BINARY, for development.
	CoreOverride CoreOrigin = "override"
)

// CoreInfo describes the mihomo core that is running.
type CoreInfo struct {
	// Version is what the core itself reports, such as "v1.19.33".
	Version string
	Origin  CoreOrigin
}

// CoreRelease is the newest mihomo release for this platform, with the one
// asset Hop would install and the digest GitHub published for it.
type CoreRelease struct {
	Tag       string
	AssetName string
	AssetURL  string
	Size      int64
	SHA256    string
}

// AppUpdate is what checking for a newer Hop found with mygo's signed
// updater: whether one is newer than the running version, its version and
// release notes.
type AppUpdate struct {
	Available bool
	Version   string
	Notes     string
}

// CoreStage is a step of a core upgrade.
type CoreStage string

const (
	CoreStageDownload CoreStage = "download"
	CoreStageVerify   CoreStage = "verify"
	CoreStageRestart  CoreStage = "restart"
)

// CoreProgress is how far a core upgrade has come. Done and Total are bytes
// while downloading and 0 otherwise.
type CoreProgress struct {
	Stage CoreStage
	Done  int64
	Total int64
}

// CoreRestart says what Hop did after its core left on its own: either it
// started the core again (InARow is which crash in a row this was, Exit why
// the core left), or it gave up (GaveUp) and no core runs.
type CoreRestart struct {
	InARow int
	Exit   string
	GaveUp bool
}

// Services a policy group can route, as its name says.
const (
	ServiceVideo     = "video"
	ServiceMusic     = "music"
	ServiceAI        = "ai"
	ServiceSearch    = "search"
	ServiceApple     = "apple"
	ServiceMicrosoft = "microsoft"
	ServiceGame      = "game"
	ServiceChat      = "chat"
	ServiceTelegram  = "telegram"
	ServiceSocial    = "social"
	ServiceCloud     = "cloud"
	ServiceDev       = "dev"
	ServiceShop      = "shop"
	ServiceDownload  = "download"
	ServiceAdBlock   = "adblock"
	ServiceDirect    = "direct"
	ServiceFinal     = "final"
)

// Appearance is the look the user chose for Hop's windows.
type Appearance string

const (
	AppearanceSystem Appearance = "system"
	AppearanceLight  Appearance = "light"
	AppearanceDark   Appearance = "dark"
)

// ColorTheme is the material palette chosen independently of appearance.
type ColorTheme string

const (
	ThemeOcean ColorTheme = "ocean"
	ThemeDusk  ColorTheme = "dusk"
	ThemePaper ColorTheme = "paper"
)

// DefaultSubscriptionUpdateHours is how often subscriptions refresh on their
// own before the user chooses another interval. A value of 0 turns the
// automatic refresh off.
const DefaultSubscriptionUpdateHours = 12

// DefaultMenuBarSpeed is whether the menu bar shows the live speed before the
// user chooses.
const DefaultMenuBarSpeed = false

// Window clarity controls how much themed backdrop shows through the base
// glass veil: 20 is the most opaque, 85 the most transparent. The default
// matches the approved glass design.
const (
	WindowClarityMin     = 20
	WindowClarityMax     = 85
	DefaultWindowClarity = 68
)

// Settings are the user's preferences, kept between starts.
type Settings struct {
	Appearance Appearance
	// Theme selects the ocean or dusk material palette.
	Theme ColorTheme
	// DelayTimeoutSeconds is how long a delay test waits for one node.
	DelayTimeoutSeconds int
	// SubscriptionUpdateHours is how often stored subscriptions refresh on
	// their own; 0 leaves them to the manual refresh.
	SubscriptionUpdateHours int
	// SystemProxy says the user wants the system proxy pointed at Hop; Hop
	// applies it at start unless another app holds the system proxy.
	SystemProxy bool
	// Tun says the user wants the core run under the privileged helper with
	// TUN on; Hop applies it at start unless another TUN holds the default
	// routes.
	Tun bool
	// MenuBarSpeed shows the live speed beside the menu bar mark.
	MenuBarSpeed bool
	// WindowClarity is how much backdrop shows through the base veil, from
	// WindowClarityMin to WindowClarityMax. It applies only where the window
	// has a material; solid windows ignore it.
	WindowClarity int
}

// Custom rule types, as the config file writes them.
const (
	CustomRuleDomainSuffix  = "DOMAIN-SUFFIX"
	CustomRuleDomain        = "DOMAIN"
	CustomRuleDomainKeyword = "DOMAIN-KEYWORD"
	CustomRuleIPCIDR        = "IP-CIDR"
	CustomRuleProcessName   = "PROCESS-NAME"
)

// Custom rule targets. CustomRuleProxy is a placeholder: the config writes
// the dashboard's main proxy group there.
const (
	CustomRuleDirect = "DIRECT"
	CustomRuleProxy  = "PROXY"
	CustomRuleReject = "REJECT"
)

// CustomRule is one rule the user puts ahead of a profile's own rules.
type CustomRule struct {
	Type   string
	Value  string
	Target string
}

// GeodataStatus is what the settings page shows of the geo databases the
// core's rules load. UpdatedAt is zero while they are the ones the app
// ships.
type GeodataStatus struct {
	UpdatedAt time.Time
}

// TunStatus is what the dashboard shows of TUN mode: whether the privileged
// helper is installed and its version, whether its core runs, and which
// interface holds the default routes when another client's TUN does.
type TunStatus struct {
	HelperInstalled bool
	HelperVersion   string
	// HelperOutdated says the installed helper is another version than this
	// app's: it must be replaced before TUN can use it.
	HelperOutdated bool
	CoreRunning    bool
	Taken          bool
	TakenBy        string
}

// ProxyEndpoint is one proxy the system sends traffic to.
type ProxyEndpoint struct {
	Enabled bool
	Host    string
	Port    int
}

// SystemProxyState is what the system proxy is set to now. AutoConfig is a
// PAC script in use, which only another app sets.
type SystemProxyState struct {
	HTTP       ProxyEndpoint
	HTTPS      ProxyEndpoint
	SOCKS      ProxyEndpoint
	AutoConfig bool
}

// ProxyOwner is who the system proxy points at.
type ProxyOwner string

const (
	// ProxyOwnerNone: no system proxy is set.
	ProxyOwnerNone ProxyOwner = "none"
	// ProxyOwnerHop: every proxy set points at Hop.
	ProxyOwnerHop ProxyOwner = "hop"
	// ProxyOwnerOther: some proxy points elsewhere, so another app holds it.
	ProxyOwnerOther ProxyOwner = "other"
)

// SystemProxyStatus is who holds the system proxy, and where it points when
// another app holds it ("" otherwise).
type SystemProxyStatus struct {
	Owner ProxyOwner
	Other string
}
