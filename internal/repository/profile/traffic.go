package profile

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"hop/internal/model"
)

const (
	trafficFileName = "traffic.json"
	trafficVersion  = 1
	// trafficDateLayout is the local calendar day a bucket is keyed by.
	trafficDateLayout = "2006-01-02"
)

// trafficFile is the statistics as kept on disk: one bucket per local day
// and source.
type trafficFile struct {
	Version int              `json:"version"`
	Days    []trafficDayFile `json:"days"`
}

// trafficDayFile is one bucket of the file.
type trafficDayFile struct {
	Date       string `json:"date"`
	SourceID   string `json:"sourceID"`
	SourceName string `json:"sourceName,omitempty"`
	ProxyUp    int64  `json:"proxyUp"`
	ProxyDown  int64  `json:"proxyDown"`
	DirectUp   int64  `json:"directUp"`
	DirectDown int64  `json:"directDown"`
}

// LoadTraffic returns the stored daily traffic, oldest bucket first. A file
// written by a newer Hop, a malformed entry or a duplicate day and source is
// an error: the file is only ever replaced as a whole, so anything else means
// it was not Hop that wrote it.
func (s *Store) LoadTraffic() ([]model.TrafficDay, error) {
	path := filepath.Join(s.dir, trafficFileName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read traffic history (path=%q): %w", path, err)
	}
	var file trafficFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("decode traffic history (path=%q): %w", path, err)
	}
	if file.Version != trafficVersion {
		return nil, fmt.Errorf("traffic history version is %d, hop writes %d (path=%q)", file.Version, trafficVersion, path)
	}
	days := make([]model.TrafficDay, 0, len(file.Days))
	seen := make(map[string]bool, len(file.Days))
	for _, day := range file.Days {
		if _, err := time.Parse(trafficDateLayout, day.Date); err != nil {
			return nil, fmt.Errorf("traffic history (path=%q) has an invalid date %q: %w", path, day.Date, err)
		}
		if day.SourceID == "" {
			return nil, fmt.Errorf("traffic history (path=%q) has a bucket for %s without a source", path, day.Date)
		}
		if day.ProxyUp < 0 || day.ProxyDown < 0 || day.DirectUp < 0 || day.DirectDown < 0 {
			return nil, fmt.Errorf("traffic history (path=%q) has negative bytes for %s on %s", path, day.SourceID, day.Date)
		}
		key := day.Date + "\x00" + day.SourceID
		if seen[key] {
			return nil, fmt.Errorf("traffic history (path=%q) lists source %q on %s twice", path, day.SourceID, day.Date)
		}
		seen[key] = true
		days = append(days, model.TrafficDay{
			Date:       day.Date,
			SourceID:   day.SourceID,
			SourceName: day.SourceName,
			Split: model.TrafficSplit{
				ProxyUp:    day.ProxyUp,
				ProxyDown:  day.ProxyDown,
				DirectUp:   day.DirectUp,
				DirectDown: day.DirectDown,
			},
		})
	}
	sort.Slice(days, func(i, j int) bool {
		if days[i].Date != days[j].Date {
			return days[i].Date < days[j].Date
		}
		return days[i].SourceID < days[j].SourceID
	})
	return days, nil
}

// SaveTraffic replaces the stored daily traffic atomically, owner-only, in a
// stable order.
func (s *Store) SaveTraffic(days []model.TrafficDay) error {
	sorted := make([]model.TrafficDay, len(days))
	copy(sorted, days)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Date != sorted[j].Date {
			return sorted[i].Date < sorted[j].Date
		}
		return sorted[i].SourceID < sorted[j].SourceID
	})
	file := trafficFile{Version: trafficVersion, Days: make([]trafficDayFile, 0, len(sorted))}
	for _, day := range sorted {
		file.Days = append(file.Days, trafficDayFile{
			Date:       day.Date,
			SourceID:   day.SourceID,
			SourceName: day.SourceName,
			ProxyUp:    day.Split.ProxyUp,
			ProxyDown:  day.Split.ProxyDown,
			DirectUp:   day.Split.DirectUp,
			DirectDown: day.Split.DirectDown,
		})
	}
	raw, err := json.Marshal(file)
	if err != nil {
		return fmt.Errorf("encode traffic history: %w", err)
	}
	return writeAtomic(filepath.Join(s.dir, trafficFileName), raw)
}
