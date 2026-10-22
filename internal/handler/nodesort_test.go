package handler

import (
	"slices"
	"testing"

	"hop/internal/model"
)

func TestNodesByDelay(t *testing.T) {
	node := func(name string, ms int, measured bool) model.NodeView {
		return model.NodeView{Name: name, Delay: model.Delay{Millis: ms, Measured: measured}}
	}
	regions := []model.RegionView{
		{Nodes: []model.NodeView{node("slow", 300, true), node("down", 0, true), node("untested", 0, false)}},
		{Nodes: []model.NodeView{node("fast", 40, true)}},
	}
	var names []string
	for _, n := range nodesByDelay(regions, func(n model.NodeView) model.Delay { return n.Delay }) {
		names = append(names, n.Name)
	}
	if want := []string{"fast", "slow", "untested", "down"}; !slices.Equal(names, want) {
		t.Errorf("got %v, want %v", names, want)
	}
}
