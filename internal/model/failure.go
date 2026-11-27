package model

// FailureKind names why an action failed, as far as its error tells.
type FailureKind string

const (
	// FailureUnknown is an error that says nothing the user can act on; only
	// its own text is shown, as the details.
	FailureUnknown FailureKind = ""
	// FailureExplained is an error that already says what happened in the
	// user's words.
	FailureExplained FailureKind = "explained"
	// FailureCoreDown is a request to Hop's own core that nothing answered on
	// loopback: the core is not running, or not yet.
	FailureCoreDown FailureKind = "core-down"
	// FailureTimeout is a request that got no answer in time.
	FailureTimeout FailureKind = "timeout"
	// FailureProviderRefused is a subscription provider answering 4xx.
	FailureProviderRefused FailureKind = "provider-refused"
	// FailureProviderDown is a subscription provider answering 5xx.
	FailureProviderDown FailureKind = "provider-down"
	// FailureHostNotFound is a server name that did not resolve.
	FailureHostNotFound FailureKind = "host-not-found"
	// FailureNetwork is a server that could not be reached.
	FailureNetwork FailureKind = "network"
)

// FailureReason is why an action failed: its kind and what the kind's words
// name.
type FailureReason struct {
	Kind FailureKind
	// Text is an explained error's own words.
	Text string
	// Status is the provider's HTTP status.
	Status int
	// Host is the server whose name did not resolve.
	Host string
	// Detail is the error's own text under the reason, for a report; empty
	// when the reason says it all.
	Detail string
}
