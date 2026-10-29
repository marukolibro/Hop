package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
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

// UnlockChecker asks the services whether they work through one proxy: a
// probe lane's listener or the mixed port. It hands each service's result to
// report as it comes, one at a time.
type UnlockChecker interface {
	Check(ctx context.Context, report func(model.UnlockResult)) error
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

// UnlockService finds which services work: through the rules, as the user's
// own traffic goes, and through each group's current node, as traffic sent to
// the group would take. It checks as many groups at once as it has lanes, one
// checker each.
type UnlockService struct {
	controller UnlockController
	lanes      []UnlockChecker
	routed     UnlockChecker
	running    sync.Mutex
	// routedRunning is the run through the rules, which needs no lane and
	// so goes beside a run over the groups.
	routedRunning sync.Mutex
}

// NewUnlockService returns a service that picks groups through controller and
// checks through lanes, where lane i is the one model.ProbeGroup(i) belongs
// to, and through the rules with routed.
func NewUnlockService(controller UnlockController, lanes []UnlockChecker, routed UnlockChecker) *UnlockService {
	return &UnlockService{controller: controller, lanes: lanes, routed: routed}
}

// CheckServices checks every service through the mixed port, where the rules
// send each one the way they send the user's own traffic, and hands each
// result to report as it comes. A service that could not be checked is
// reported unknown, and the reason is logged.
func (s *UnlockService) CheckServices(ctx context.Context, report func(model.UnlockResult)) error {
	if !s.routedRunning.TryLock() {
		return ErrUnlockRunning
	}
	defer s.routedRunning.Unlock()
	err := s.routed.Check(ctx, report)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		slog.Warn("service check incomplete", "error", err)
	}
	return nil
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
	var result model.Unlock
	checkErr := checker.Check(ctx, func(r model.UnlockResult) {
		result.Results = append(result.Results, r)
	})
	result.Results = inServiceOrder(result.Results)
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

// inServiceOrder puts results, which come as their checks finish, in the
// order the services show.
func inServiceOrder(results []model.UnlockResult) []model.UnlockResult {
	out := slices.Clone(results)
	slices.SortStableFunc(out, func(a, b model.UnlockResult) int {
		return slices.Index(model.UnlockServices, a.Service) - slices.Index(model.UnlockServices, b.Service)
	})
	return out
}
