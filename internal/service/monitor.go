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
	StreamMemory(ctx context.Context, handle func(uint64)) error
}

// ProcessMemory reads the memory Hop's own process holds.
type ProcessMemory interface {
	Resident() (uint64, error)
}

// MonitorService shows the core's connections, log and memory, and Hop's.
type MonitorService struct {
	core MonitorCore
	self ProcessMemory
}

// NewMonitorService returns a service backed by core, reading Hop's own
// memory through self.
func NewMonitorService(core MonitorCore, self ProcessMemory) *MonitorService {
	return &MonitorService{core: core, self: self}
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

// WatchMemory calls handle with the memory of Hop and of its core whenever
// the core reports its own, about once a second, until ctx is done or the
// stream fails. Hop's is read at the same moment, so the two belong
// together.
func (s *MonitorService) WatchMemory(ctx context.Context, handle func(model.Memory)) error {
	var readErr error
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	err := s.core.StreamMemory(ctx, func(core uint64) {
		app, err := s.self.Resident()
		if err != nil {
			readErr = err
			cancel()
			return
		}
		handle(model.Memory{App: app, Core: core})
	})
	if readErr != nil {
		return readErr
	}
	return err
}
