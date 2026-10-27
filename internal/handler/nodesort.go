package handler

import (
	"cmp"
	"slices"

	"hop/internal/model"
	"hop/locales"
)

// The orders the node grid offers: the subscription's own, grouped by
// region, or one list from the fastest node to the slowest.
const (
	nodeOrderSubscription = iota
	nodeOrderDelay
)

func nodeOrderLabels() []string {
	return []string{locales.NodesOrderDefault(), locales.NodesOrderLatency()}
}

// Where a node ranks when ordered by delay: answered first, then nodes not
// tested yet, then those that timed out.
const (
	rankAnswered = iota
	rankUntested
	rankTimedOut
)

func delayRank(delay model.Delay) int {
	switch {
	case !delay.Measured:
		return rankUntested
	case delay.Millis == 0:
		return rankTimedOut
	default:
		return rankAnswered
	}
}

// nodesByDelay flattens regions into one list, the fastest node first. Nodes
// that rank alike keep their order in the subscription. The input is not
// modified.
func nodesByDelay(regions []model.RegionView, delayOf func(model.NodeView) model.Delay) []model.NodeView {
	var nodes []model.NodeView
	for _, region := range regions {
		nodes = append(nodes, region.Nodes...)
	}
	slices.SortStableFunc(nodes, func(a model.NodeView, b model.NodeView) int {
		da, db := delayOf(a), delayOf(b)
		if byRank := cmp.Compare(delayRank(da), delayRank(db)); byRank != 0 {
			return byRank
		}
		if delayRank(da) == rankAnswered {
			return cmp.Compare(da.Millis, db.Millis)
		}
		return 0
	})
	return nodes
}

// nodeDelays lists the delays every node of view shows now.
func nodeDelays(view model.ProxyView) map[string]model.Delay {
	delays := map[string]model.Delay{}
	for _, group := range view.Groups {
		for _, region := range group.Regions {
			for _, node := range region.Nodes {
				delays[node.Name] = node.Delay
			}
		}
	}
	return delays
}

// orderDelay is the delay a node is ranked by. While a test runs it is the
// delay from before the test, so tiles do not shuffle as answers come in;
// the order catches up when the test ends.
func (d *Dashboard) orderDelay(node model.NodeView) model.Delay {
	if d.testing == "" {
		return node.Delay
	}
	return d.delaysBeforeTest[node.Name]
}
