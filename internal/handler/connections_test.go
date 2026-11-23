package handler

import (
	"slices"
	"testing"
	"time"

	"hop/internal/model"
)

func TestConnectionsSortAndPauseSnapshot(t *testing.T) {
	now := time.Now()
	d := &Dashboard{}
	d.connections.data = model.Connections{Active: []model.Connection{
		{ID: "1", Host: "z.example.com", Download: 100, Upload: 50, Start: now.Add(-2 * time.Minute)},
		{ID: "2", Host: "a.example.com", Download: 900, Upload: 100, Start: now.Add(-1 * time.Minute)},
		{ID: "3", Host: "m.example.com", Download: 300, Upload: 0, Start: now},
	}}

	// Default newest first.
	got := idsOf(d.visibleConnections())
	if want := []string{"3", "2", "1"}; !slices.Equal(got, want) {
		t.Fatalf("newest order = %v, want %v", got, want)
	}

	// Highest traffic first.
	d.connections.sort = connSortTraffic
	got = idsOf(d.visibleConnections())
	if want := []string{"2", "3", "1"}; !slices.Equal(got, want) {
		t.Fatalf("traffic order = %v, want %v", got, want)
	}

	// Host A-Z.
	d.connections.sort = connSortHost
	got = idsOf(d.visibleConnections())
	if want := []string{"2", "3", "1"}; !slices.Equal(got, want) {
		t.Fatalf("host order = %v, want %v", got, want)
	}

	// Pause freezes displayed rows even when live data changes, and dropping a
	// closed connection updates the paused snapshot immediately.
	d.toggleConnectionsPause()
	d.connections.data.Active = append(d.connections.data.Active, model.Connection{ID: "4", Host: "new.example.com", Start: now.Add(time.Second)})
	if len(d.visibleConnections()) != 3 {
		t.Fatalf("paused visible len = %d, want 3", len(d.visibleConnections()))
	}
	d.dropPausedConnections("2")
	got = idsOf(d.visibleConnections())
	if want := []string{"3", "1"}; !slices.Equal(got, want) {
		t.Fatalf("after drop paused = %v, want %v", got, want)
	}

	// Resume reflects latest live data.
	d.toggleConnectionsPause()
	if len(d.visibleConnections()) != 4 {
		t.Fatalf("resumed visible len = %d, want 4", len(d.visibleConnections()))
	}
}

func idsOf(conns []model.Connection) []string {
	out := make([]string, len(conns))
	for i, c := range conns {
		out[i] = c.ID
	}
	return out
}
