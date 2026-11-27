package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"

	"hop/internal/model"
	"hop/internal/repository/subscription"
)

// ExplainedError is an error whose Reason the user reads as it is, as the
// words that say why an action failed; Cause, when there is one, is the
// detail under it.
type ExplainedError struct {
	Reason string
	Cause  error
}

func (e *ExplainedError) Error() string {
	if e.Cause == nil {
		return e.Reason
	}
	return e.Reason + e.Cause.Error()
}

func (e *ExplainedError) Unwrap() error { return e.Cause }

// FailureReasonOf reads from err why an action failed, in terms the user can
// act on, with the error's own text as the detail. An error it cannot read is
// FailureUnknown, never a guess: the detail still says what happened.
func FailureReasonOf(err error) model.FailureReason {
	if explained, ok := errors.AsType[*ExplainedError](err); ok {
		reason := model.FailureReason{Kind: model.FailureExplained, Text: explained.Reason}
		if explained.Cause != nil {
			reason.Detail = explained.Cause.Error()
		}
		return reason
	}
	reason := failureKindOf(err)
	reason.Detail = err.Error()
	return reason
}

// failureKindOf is FailureReasonOf without the detail.
func failureKindOf(err error) model.FailureReason {
	if errors.Is(err, context.Canceled) {
		return model.FailureReason{}
	}
	if status, ok := errors.AsType[*subscription.StatusError](err); ok {
		if status.Status >= http.StatusInternalServerError {
			return model.FailureReason{Kind: model.FailureProviderDown, Status: status.Status}
		}
		return model.FailureReason{Kind: model.FailureProviderRefused, Status: status.Status}
	}
	if dns, ok := errors.AsType[*net.DNSError](err); ok && dns.IsNotFound {
		return model.FailureReason{Kind: model.FailureHostNotFound, Host: dns.Name}
	}
	if timedOut(err) {
		return model.FailureReason{Kind: model.FailureTimeout}
	}
	if op, ok := errors.AsType[*net.OpError](err); ok && op.Op == "dial" {
		if dialedLoopback(op) {
			return model.FailureReason{Kind: model.FailureCoreDown}
		}
		return model.FailureReason{Kind: model.FailureNetwork}
	}
	return model.FailureReason{}
}

// timedOut reports whether err is a deadline that passed or a network
// operation that timed out.
func timedOut(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	netErr, ok := errors.AsType[net.Error](err)
	return ok && netErr.Timeout()
}

// dialedLoopback reports whether op dialed a loopback address: every server
// Hop dials there is its own core, its controller or its mixed port.
func dialedLoopback(op *net.OpError) bool {
	if op.Addr == nil {
		return false
	}
	addr, err := netip.ParseAddrPort(op.Addr.String())
	return err == nil && addr.Addr().IsLoopback()
}
