package service

import "hop/internal/model"

// Conditions lists what has gone wrong that the user did not do and would not
// see: something they wanted Hop to hold is held by another app. A switch the
// user turned off is not a condition, and neither is one whose state is not
// known yet.
func Conditions(in model.ConditionInput) []model.Condition {
	var found []model.Condition
	if in.WantSystemProxy && in.SystemProxyKnown && in.SystemProxy.Owner == model.ProxyOwnerOther {
		found = append(found, model.Condition{Kind: model.ConditionSystemProxyTaken, Holder: in.SystemProxy.Other})
	}
	if in.WantTun && in.TunKnown && in.Tun.Taken && !in.Tun.CoreRunning {
		found = append(found, model.Condition{Kind: model.ConditionTunTaken, Holder: in.Tun.TakenBy})
	}
	return found
}
