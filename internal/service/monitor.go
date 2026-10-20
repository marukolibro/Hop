package service

import (
	"context"
	"sort"

	"hop/internal/model"
)

// MonitorCore streams what the core is doing.
type MonitorCore interface {
	StreamConnections(ctx context.Context, handle func(model.Connections)) error
	CloseConnection(ctx context.Context, id string) error
	CloseAllConnections(ctx context.Context) error
	StreamLogs(ctx context.Context, level string, handle func(model.LogEntry)) error
}

// MonitorService shows the core's connections and log.
type MonitorService struct {
	core MonitorCore
}

// NewMonitorService returns a service backed by core.
func NewMonitorService(core MonitorCore) *MonitorService {
	return &MonitorService{core: core}
}

// WatchConnections calls handle with the connection table, newest first,
// until ctx is done or the stream fails.
func (s *MonitorService) WatchConnections(ctx context.Context, handle func(model.Connections)) error {
	return s.core.StreamConnections(ctx, func(conns model.Connections) {
		handle(NewestFirst(conns))
	})
}

// CloseConnection closes one connection.
func (s *MonitorService) CloseConnection(ctx context.Context, id string) error {
	return s.core.CloseConnection(ctx, id)
}

// CloseAllConnections closes every connection.
func (s *MonitorService) CloseAllConnections(ctx context.Context) error {
	return s.core.CloseAllConnections(ctx)
}

// WatchLogs calls handle for every log line at level or above until ctx is
// done or the stream fails.
func (s *MonitorService) WatchLogs(ctx context.Context, level string, handle func(model.LogEntry)) error {
	return s.core.StreamLogs(ctx, level, handle)
}

// NewestFirst returns conns with the most recent connection first. The
// input is not modified.
func NewestFirst(conns model.Connections) model.Connections {
	active := make([]model.Connection, len(conns.Active))
	copy(active, conns.Active)
	sort.SliceStable(active, func(i, j int) bool { return active[i].Start.After(active[j].Start) })
	conns.Active = active
	return conns
}

// AppendLog returns entries with entry appended, keeping at most limit
// lines. The input is not modified.
func AppendLog(entries []model.LogEntry, entry model.LogEntry, limit int) []model.LogEntry {
	start := max(0, len(entries)+1-limit)
	out := make([]model.LogEntry, 0, len(entries)+1-start)
	out = append(out, entries[start:]...)
	return append(out, entry)
}
