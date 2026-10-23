package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"hop/internal/model"
)

// UnlockController is what the unlock check needs from the core: the groups,
// and each probe lane's choice.
type UnlockController interface {
	Snapshot(ctx context.Context) (model.Snapshot, error)
	SelectProxy(ctx context.Context, group string, proxy string) error
	SelectedProxy(ctx context.Context, group string) (string, error)
}

// UnlockChecker asks the services whether they work through one probe lane's
// listener.
type UnlockChecker interface {
	Check(ctx context.Context) (model.Unlock, error)
}

// UnlockReport is one group's result and how far the run is.
type UnlockReport struct {
	Group  string
	Result model.Unlock
	// Done counts the groups checked so far, this one included; Total is how
	// many the run checks.
	Done  int
	Total int
}

// ErrUnlockRunning is returned when a run is asked for while another one is
// going: the lanes hold one group each.
var ErrUnlockRunning = errors.New("an unlock check is already running")

// UnlockService finds which services work through which groups: each group's
// current node, as traffic sent to the group would take. It checks as many
// groups at once as it has lanes, one checker each.
type UnlockService struct {
	controller UnlockController
	lanes      []UnlockChecker
	running    sync.Mutex
}

// NewUnlockService returns a service that picks groups through controller and
// checks through lanes; lane i is the one model.ProbeGroup(i) belongs to.
func NewUnlockService(controller UnlockController, lanes []UnlockChecker) *UnlockService {
	return &UnlockService{controller: controller, lanes: lanes}
}

// Detect checks every group of the profile, lanes at a time, and hands each
// result to report as it comes (from the lane's goroutine, one at a time). A
// service that could not be checked on a group is unknown there, and the
// reason is logged; the run goes on.
//
// The run stops with an error when the core cannot take a group, or when a lane
// is not on the group a check ran under, which means the core reloaded in
// between and the result would belong to another group.
func (s *UnlockService) Detect(ctx context.Context, report func(UnlockReport)) error {
	if !s.running.TryLock() {
		return ErrUnlockRunning
	}
	defer s.running.Unlock()
	snapshot, err := s.controller.Snapshot(ctx)
	if err != nil {
		return err
	}
	groups := make(chan string, len(snapshot.Groups))
	for _, g := range snapshot.Groups {
		groups <- g.Name
	}
	close(groups)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		done     int
		firstErr error
		total    = len(snapshot.Groups)
	)
	for lane, checker := range s.lanes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for group := range groups {
				result, err := s.checkOn(ctx, lane, checker, group)
				mu.Lock()
				if err != nil {
					if firstErr == nil && ctx.Err() == nil {
						firstErr = err
						cancel()
					}
					mu.Unlock()
					return
				}
				done++
				report(UnlockReport{Group: group, Result: result, Done: done, Total: total})
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

// checkOn picks group in lane and checks the services through it.
func (s *UnlockService) checkOn(ctx context.Context, lane int, checker UnlockChecker, group string) (model.Unlock, error) {
	laneGroup := model.ProbeGroup(lane)
	if err := s.controller.SelectProxy(ctx, laneGroup, group); err != nil {
		return model.Unlock{}, fmt.Errorf("pick group %q in probe lane %d: %w", group, lane, err)
	}
	result, checkErr := checker.Check(ctx)
	if ctx.Err() != nil {
		return model.Unlock{}, ctx.Err()
	}
	picked, err := s.controller.SelectedProxy(ctx, laneGroup)
	if err != nil {
		return model.Unlock{}, fmt.Errorf("read probe lane %d after checking group %q: %w", lane, group, err)
	}
	if picked != group {
		return model.Unlock{}, fmt.Errorf("probe lane %d moved from group %q to %q during its check: the core reloaded", lane, group, picked)
	}
	if checkErr != nil {
		slog.Warn("unlock check incomplete", "group", group, "error", checkErr)
	}
	return result, nil
}
