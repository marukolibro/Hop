package service

import (
	"testing"
	"time"

	"hop/internal/model"
)

type memoryTrafficStore struct {
	days  []model.TrafficDay
	saved []model.TrafficDay
}

func (m *memoryTrafficStore) LoadTraffic() ([]model.TrafficDay, error) {
	return m.days, nil
}

func (m *memoryTrafficStore) SaveTraffic(days []model.TrafficDay) error {
	m.saved = days
	return nil
}

func newTrafficForTest(t *testing.T, store *memoryTrafficStore) *TrafficService {
	t.Helper()
	s, err := NewTrafficService(store)
	if err != nil {
		t.Fatalf("NewTrafficService: %v", err)
	}
	return s
}

func TestTrafficRecordSplitsAndRollsOver(t *testing.T) {
	source := model.TrafficSource{ID: "sub-a", Name: "机场 A"}
	day := time.Date(2026, 10, 21, 12, 0, 0, 0, time.Local)
	t.Run("deltas scale to the global total", func(t *testing.T) {
		s := newTrafficForTest(t, &memoryTrafficStore{})
		node := model.Connection{ID: "n1", Chain: []string{"GROUP", "node-a"}}
		direct := model.Connection{ID: "d1", Chain: []string{"DIRECT"}}
		s.Record(model.Connections{Active: []model.Connection{node, direct}}, source, day)
		node.Upload, node.Download = 100, 200
		direct.Upload, direct.Download = 50, 400
		s.Record(model.Connections{
			Active:      []model.Connection{node, direct},
			UploadTotal: 200, DownloadTotal: 800,
		}, source, day.Add(time.Second))
		view := s.Summary(day.Add(time.Second))
		want := model.TrafficSplit{ProxyUp: 133, ProxyDown: 267, DirectUp: 67, DirectDown: 533}
		if view.Today != want {
			t.Fatalf("today = %+v, want %+v", view.Today, want)
		}
		if view.Month != want {
			t.Fatalf("month = %+v, want %+v", view.Month, want)
		}
		if len(view.Sources) != 1 || view.Sources[0].Name != source.Name {
			t.Fatalf("sources = %+v, want one named %q", view.Sources, source.Name)
		}
		if len(view.Days) != trafficChartDays || view.Days[len(view.Days)-1].Split != want {
			t.Fatalf("last chart day = %+v, want %+v", view.Days[len(view.Days)-1], want)
		}
	})

	t.Run("a closed connection cannot lose the total", func(t *testing.T) {
		s := newTrafficForTest(t, &memoryTrafficStore{})
		node := model.Connection{ID: "n1", Chain: []string{"GROUP", "node-a"}}
		direct := model.Connection{ID: "d1", Chain: []string{"DIRECT"}}
		s.Record(model.Connections{Active: []model.Connection{node, direct}}, source, day)
		node.Upload = 100
		s.Record(model.Connections{
			Active:      []model.Connection{node},
			UploadTotal: 300, DownloadTotal: 0,
		}, source, day.Add(time.Second))
		view := s.Summary(day.Add(time.Second))
		if got := trafficTotal(view.Today); got != 300 {
			t.Fatalf("today total = %d, want the global 300", got)
		}
	})

	t.Run("without connection evidence the day's ratio splits the total", func(t *testing.T) {
		s := newTrafficForTest(t, &memoryTrafficStore{})
		node := model.Connection{ID: "n1", Chain: []string{"node-a"}}
		direct := model.Connection{ID: "d1", Chain: []string{"DIRECT"}}
		s.Record(model.Connections{Active: []model.Connection{node, direct}}, source, day)
		node.Upload, node.Download = 100, 200
		direct.Upload, direct.Download = 50, 400
		s.Record(model.Connections{
			Active:      []model.Connection{node, direct},
			UploadTotal: 200, DownloadTotal: 800,
		}, source, day.Add(time.Second))
		// Both connections closed between samples; only the totals grew.
		s.Record(model.Connections{
			UploadTotal: 300, DownloadTotal: 800,
		}, source, day.Add(2*time.Second))
		view := s.Summary(day.Add(2 * time.Second))
		want := model.TrafficSplit{ProxyUp: 200, ProxyDown: 267, DirectUp: 100, DirectDown: 533}
		if view.Today != want {
			t.Fatalf("today = %+v, want %+v", view.Today, want)
		}
	})

	t.Run("a lower total counts from zero", func(t *testing.T) {
		s := newTrafficForTest(t, &memoryTrafficStore{})
		node := model.Connection{ID: "n1", Chain: []string{"node-a"}}
		node.Upload, node.Download = 1000, 2000
		s.Record(model.Connections{
			Active:      []model.Connection{node},
			UploadTotal: 1000, DownloadTotal: 2000,
		}, source, day)
		node.Upload, node.Download = 300, 400
		s.Record(model.Connections{
			Active:      []model.Connection{node},
			UploadTotal: 300, DownloadTotal: 400,
		}, source, day.Add(time.Second))
		view := s.Summary(day.Add(time.Second))
		want := model.TrafficSplit{ProxyUp: 300, ProxyDown: 400}
		if view.Today != want {
			t.Fatalf("today = %+v, want %+v", view.Today, want)
		}
	})

	t.Run("midnight starts a new bucket", func(t *testing.T) {
		store := &memoryTrafficStore{}
		s := newTrafficForTest(t, store)
		late := time.Date(2026, 10, 21, 23, 59, 59, 0, time.Local)
		early := late.Add(2 * time.Second)
		node := model.Connection{ID: "n1", Chain: []string{"node-a"}}
		s.Record(model.Connections{Active: []model.Connection{node}}, source, late)
		node.Upload, node.Download = 500, 600
		s.Record(model.Connections{
			Active:      []model.Connection{node},
			UploadTotal: 500, DownloadTotal: 600,
		}, source, early)
		if view := s.Summary(late); trafficTotal(view.Today) != 0 {
			t.Fatalf("traffic landed on the day before midnight: %+v", view.Today)
		}
		if view := s.Summary(early); view.Today != (model.TrafficSplit{ProxyUp: 500, ProxyDown: 600}) {
			t.Fatalf("today after midnight = %+v", view.Today)
		}
		if err := s.Flush(); err != nil {
			t.Fatalf("Flush: %v", err)
		}
		if len(store.saved) != 1 || store.saved[0].Date != "2026-10-22" {
			t.Fatalf("saved = %+v, want one bucket on 2026-10-22", store.saved)
		}
	})
}

func TestTrafficPrunesOldBuckets(t *testing.T) {
	old := time.Now().AddDate(0, 0, -500).Format(trafficDateLayout)
	recent := time.Now().AddDate(0, 0, -10).Format(trafficDateLayout)
	store := &memoryTrafficStore{days: []model.TrafficDay{
		{Date: old, SourceID: "sub-a", Split: model.TrafficSplit{ProxyUp: 1}},
		{Date: recent, SourceID: "sub-a", Split: model.TrafficSplit{ProxyUp: 2}},
	}}
	s := newTrafficForTest(t, store)
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if len(store.saved) != 1 || store.saved[0].Date != recent {
		t.Fatalf("saved = %+v, want only %s", store.saved, recent)
	}
}
