package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"hop/internal/model"
)

const (
	// TrafficBootstrapID is the source of traffic carried while no
	// subscription is in use.
	TrafficBootstrapID = "bootstrap"
	// trafficKeepDays is how many local days of history Hop keeps.
	trafficKeepDays = 400
	// trafficFlushEvery is how often dirty history reaches the disk; the
	// quit path flushes too.
	trafficFlushEvery = 30 * time.Second
	// trafficDateLayout is the local calendar day a bucket is keyed by.
	trafficDateLayout = "2006-01-02"
	// trafficChartDays is how many days the statistics page charts.
	trafficChartDays = 30
)

// TrafficStore keeps the daily traffic history across runs.
type TrafficStore interface {
	LoadTraffic() ([]model.TrafficDay, error)
	SaveTraffic(days []model.TrafficDay) error
}

// trafficConn is the bytes of one connection at the previous sample.
type trafficConn struct {
	up, down int64
}

// trafficKey identifies one daily bucket: a local day and a source.
type trafficKey struct {
	date     string
	sourceID string
}

// TrafficService records how much traffic the core carried, per local day
// and source, and answers the statistics page. The core's global totals are
// the day's total; the connection table's per-connection deltas only split
// it between proxy and direct, so connections that close between samples
// never lose bytes from the total.
type TrafficService struct {
	store TrafficStore

	// flushMu serializes writes: the quit path and the ticker can flush at
	// the same time, and a store writes through one temporary file.
	flushMu   sync.Mutex
	mu        sync.Mutex
	buckets   map[trafficKey]model.TrafficDay
	dirty     bool
	gen       uint64
	seen      bool
	prevUp    int64
	prevDown  int64
	prevConns map[string]trafficConn
}

// NewTrafficService returns a service on the stored history, dropping
// buckets older than the days Hop keeps.
func NewTrafficService(store TrafficStore) (*TrafficService, error) {
	loaded, err := store.LoadTraffic()
	if err != nil {
		return nil, fmt.Errorf("load traffic history: %w", err)
	}
	kept := pruneTraffic(loaded, time.Now())
	s := &TrafficService{
		store:     store,
		buckets:   make(map[trafficKey]model.TrafficDay, len(kept)),
		prevConns: make(map[string]trafficConn),
	}
	for _, day := range kept {
		s.buckets[trafficKey{date: day.Date, sourceID: day.SourceID}] = day
	}
	if len(kept) != len(loaded) {
		s.dirty = true
		s.gen++
	}
	return s, nil
}

// Start flushes dirty history every trafficFlushEvery until ctx is done, and
// once more as it ends. The goroutine belongs to the caller: it ends with
// ctx.
func (s *TrafficService) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(trafficFlushEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				if err := s.Flush(); err != nil {
					slog.Error("flush traffic history at quit", "error", err)
				}
				return
			case <-ticker.C:
				if err := s.Flush(); err != nil {
					slog.Error("flush traffic history", "error", err)
				}
			}
		}
	}()
}

// Record accounts one connection-table sample to source under now's local
// day. It must be called on the UI thread, once per sample.
func (s *TrafficService) Record(conns model.Connections, source model.TrafficSource, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if source.ID == "" {
		source.ID = TrafficBootstrapID
	}
	split := model.TrafficSplit{}
	if s.seen {
		upDelta, upReset := trafficDelta(conns.UploadTotal, s.prevUp)
		downDelta, downReset := trafficDelta(conns.DownloadTotal, s.prevDown)
		// A total going down means a new core started, counted from zero.
		fromZero := upReset || downReset
		proxyUp, proxyDown, directUp, directDown := estimateTraffic(conns.Active, s.prevConns, fromZero)
		today := now.Format(trafficDateLayout)
		if proxyUp+directUp == 0 {
			// No connection says who carried it: keep today's ratio.
			todaySplit := s.splitForDateLocked(today)
			proxyUp, directUp = splitByRatio(upDelta, todaySplit.ProxyUp, todaySplit.DirectUp)
		} else {
			proxyUp, directUp = splitByRatio(upDelta, proxyUp, directUp)
		}
		if proxyDown+directDown == 0 {
			todaySplit := s.splitForDateLocked(today)
			proxyDown, directDown = splitByRatio(downDelta, todaySplit.ProxyDown, todaySplit.DirectDown)
		} else {
			proxyDown, directDown = splitByRatio(downDelta, proxyDown, directDown)
		}
		split = model.TrafficSplit{ProxyUp: proxyUp, ProxyDown: proxyDown, DirectUp: directUp, DirectDown: directDown}
		if trafficTotal(split) > 0 {
			s.addLocked(today, source, split)
		}
	}
	s.prevUp, s.prevDown = conns.UploadTotal, conns.DownloadTotal
	s.prevConns = trafficConns(conns.Active)
	s.seen = true
}

// Summary returns what the statistics page shows as of now.
func (s *TrafficService) Summary(now time.Time) model.TrafficView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return summarizeTraffic(s.snapshotLocked(), now)
}

// Flush writes the history when it changed since the last write. A failure
// leaves it dirty, so the next flush tries again.
func (s *TrafficService) Flush() error {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return nil
	}
	days := pruneTraffic(s.snapshotLocked(), time.Now())
	gen := s.gen
	s.mu.Unlock()
	if err := s.store.SaveTraffic(days); err != nil {
		return fmt.Errorf("save traffic history: %w", err)
	}
	s.mu.Lock()
	if s.gen == gen {
		s.dirty = false
	}
	s.mu.Unlock()
	return nil
}

// addLocked adds split to source's bucket on date.
func (s *TrafficService) addLocked(date string, source model.TrafficSource, split model.TrafficSplit) {
	key := trafficKey{date: date, sourceID: source.ID}
	bucket, found := s.buckets[key]
	if !found {
		bucket = model.TrafficDay{Date: date, SourceID: source.ID}
	}
	if source.Name != "" {
		bucket.SourceName = source.Name
	}
	bucket.Split.ProxyUp += split.ProxyUp
	bucket.Split.ProxyDown += split.ProxyDown
	bucket.Split.DirectUp += split.DirectUp
	bucket.Split.DirectDown += split.DirectDown
	s.buckets[key] = bucket
	s.dirty = true
	s.gen++
}

// splitForDateLocked sums every source's traffic on date.
func (s *TrafficService) splitForDateLocked(date string) model.TrafficSplit {
	var split model.TrafficSplit
	for key, bucket := range s.buckets {
		if key.date == date {
			addTrafficSplit(&split, bucket.Split)
		}
	}
	return split
}

// snapshotLocked returns the buckets in a stable order.
func (s *TrafficService) snapshotLocked() []model.TrafficDay {
	days := make([]model.TrafficDay, 0, len(s.buckets))
	for _, bucket := range s.buckets {
		days = append(days, bucket)
	}
	sort.Slice(days, func(i, j int) bool {
		if days[i].Date != days[j].Date {
			return days[i].Date < days[j].Date
		}
		return days[i].SourceID < days[j].SourceID
	})
	return days
}

// trafficDelta returns how much a core total grew, and whether it went down,
// which means another core started and counted from zero.
func trafficDelta(next, previous int64) (delta int64, reset bool) {
	if next < previous {
		return next, true
	}
	return next - previous, false
}

// estimateTraffic sums the byte deltas of the connection table since prev,
// split by the exit each connection leaves by. fromZero counts every
// connection's bytes, for the first sample after a core restarted. A
// connection that closed since prev is not in the table: its last bytes are
// missing here, and the caller spreads the global total over what is left.
func estimateTraffic(conns []model.Connection, prev map[string]trafficConn, fromZero bool) (proxyUp, proxyDown, directUp, directDown int64) {
	for _, conn := range conns {
		kind := trafficKindOf(conn.Chain)
		if kind == trafficDropped {
			continue
		}
		up, down := conn.Upload, conn.Download
		if !fromZero {
			if last, found := prev[conn.ID]; found {
				up = conn.Upload - last.up
				if up < 0 {
					up = conn.Upload
				}
				down = conn.Download - last.down
				if down < 0 {
					down = conn.Download
				}
			}
		}
		switch kind {
		case trafficDirect:
			directUp += up
			directDown += down
		default:
			proxyUp += up
			proxyDown += down
		}
	}
	return proxyUp, proxyDown, directUp, directDown
}

// trafficKind is how a connection leaves the core.
type trafficKind int

const (
	trafficProxy trafficKind = iota
	trafficDirect
	trafficDropped
)

// trafficKindOf reads a connection's exit: a chain ending in DIRECT skips
// the proxies, REJECT drops it, and anything else is a node.
func trafficKindOf(chain []string) trafficKind {
	if len(chain) == 0 {
		return trafficProxy
	}
	switch chain[len(chain)-1] {
	case "DIRECT":
		return trafficDirect
	case "REJECT", "REJECT-DROP":
		return trafficDropped
	default:
		return trafficProxy
	}
}

// trafficConns snapshots the table's byte counts by connection.
func trafficConns(conns []model.Connection) map[string]trafficConn {
	out := make(map[string]trafficConn, len(conns))
	for _, conn := range conns {
		out[conn.ID] = trafficConn{up: conn.Upload, down: conn.Download}
	}
	return out
}

// splitByRatio spreads total over proxy and direct in the ratio of their
// evidence; with no evidence at all the traffic counts as direct.
func splitByRatio(total, proxy, direct int64) (int64, int64) {
	if total <= 0 {
		return 0, 0
	}
	proxy, direct = max(proxy, 0), max(direct, 0)
	if proxy+direct == 0 {
		return 0, total
	}
	p := int64(math.Round(float64(total) * float64(proxy) / float64(proxy+direct)))
	p = min(max(p, 0), total)
	return p, total - p
}

// summarizeTraffic builds the statistics page's view from days as of now.
func summarizeTraffic(days []model.TrafficDay, now time.Time) model.TrafficView {
	today := now.Format(trafficDateLayout)
	month := today[:len("2006-01")]
	cutoff := now.AddDate(0, 0, -(trafficChartDays - 1)).Format(trafficDateLayout)
	chartSplits := make(map[string]model.TrafficSplit, trafficChartDays)
	sources := make(map[string]model.TrafficSourceView)
	sourceDates := make(map[string]string)
	var view model.TrafficView
	for _, day := range days {
		if day.Date >= cutoff {
			split := chartSplits[day.Date]
			addTrafficSplit(&split, day.Split)
			chartSplits[day.Date] = split
		}
		if day.Date == today {
			addTrafficSplit(&view.Today, day.Split)
		}
		if !strings.HasPrefix(day.Date, month) {
			continue
		}
		addTrafficSplit(&view.Month, day.Split)
		source := sources[day.SourceID]
		source.ID = day.SourceID
		addTrafficSplit(&source.Split, day.Split)
		if day.SourceName != "" && day.Date >= sourceDates[day.SourceID] {
			source.Name = day.SourceName
			sourceDates[day.SourceID] = day.Date
		}
		sources[day.SourceID] = source
	}
	view.Days = make([]model.TrafficDayView, 0, trafficChartDays)
	for i := trafficChartDays - 1; i >= 0; i-- {
		date := now.AddDate(0, 0, -i).Format(trafficDateLayout)
		view.Days = append(view.Days, model.TrafficDayView{Date: date, Split: chartSplits[date]})
	}
	view.Sources = make([]model.TrafficSourceView, 0, len(sources))
	for _, source := range sources {
		if trafficTotal(source.Split) == 0 {
			continue
		}
		view.Sources = append(view.Sources, source)
	}
	sort.Slice(view.Sources, func(i, j int) bool {
		left, right := view.Sources[i], view.Sources[j]
		if lp, rp := proxyTotal(left.Split), proxyTotal(right.Split); lp != rp {
			return lp > rp
		}
		if ld, rd := directTotal(left.Split), directTotal(right.Split); ld != rd {
			return ld > rd
		}
		return left.Name < right.Name
	})
	return view
}

// pruneTraffic keeps the buckets inside Hop's window of local days.
func pruneTraffic(days []model.TrafficDay, now time.Time) []model.TrafficDay {
	cutoff := now.AddDate(0, 0, -(trafficKeepDays - 1)).Format(trafficDateLayout)
	kept := make([]model.TrafficDay, 0, len(days))
	for _, day := range days {
		if day.Date >= cutoff {
			kept = append(kept, day)
		}
	}
	return kept
}

func addTrafficSplit(dst *model.TrafficSplit, src model.TrafficSplit) {
	dst.ProxyUp += src.ProxyUp
	dst.ProxyDown += src.ProxyDown
	dst.DirectUp += src.DirectUp
	dst.DirectDown += src.DirectDown
}

func trafficTotal(split model.TrafficSplit) int64 {
	return split.ProxyUp + split.ProxyDown + split.DirectUp + split.DirectDown
}

func proxyTotal(split model.TrafficSplit) int64 {
	return split.ProxyUp + split.ProxyDown
}

func directTotal(split model.TrafficSplit) int64 {
	return split.DirectUp + split.DirectDown
}
