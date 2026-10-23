package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"hop/internal/model"
)

// UnlockController is what the unlock check needs from the core: the nodes,
// and the probe group's choice.
type UnlockController interface {
	Snapshot(ctx context.Context) (model.Snapshot, error)
	SelectProxy(ctx context.Context, group string, proxy string) error
	SelectedProxy(ctx context.Context, group string) (string, error)
}

// UnlockChecker asks the services whether they work through the probe
// listener's route.
type UnlockChecker interface {
	Check(ctx context.Context) (model.Unlock, error)
}

// UnlockReport is one node's result and how far the run is.
type UnlockReport struct {
	Node   string
	Result model.Unlock
	// Done counts the nodes checked so far, this one included; Total is how
	// many the run checks.
	Done  int
	Total int
}

// ErrUnlockRunning is returned when a run is asked for while another one is
// going: the probe group holds one node at a time.
var ErrUnlockRunning = errors.New("an unlock check is already running")

// UnlockService finds which services work through which nodes, one node at a
// time through the probe group.
type UnlockService struct {
	controller UnlockController
	checker    UnlockChecker
	running    sync.Mutex
}

// NewUnlockService returns a service picking nodes through controller and
// checking through checker.
func NewUnlockService(controller UnlockController, checker UnlockChecker) *UnlockService {
	return &UnlockService{controller: controller, checker: checker}
}

// Detect checks the nodes behind group, one after the other, and hands each
// result to report as it comes. Nodes whose last delay test got no answer are
// left out: they carry nothing, and each would hold the run until its
// timeout. A service that could not be checked on a node is unknown there, and
// the reason is logged; the run goes on.
//
// The run stops with an error when the core cannot take a node, or when the
// probe group is not on the node a check ran under, which means the core
// reloaded in between and the result would belong to another node.
func (s *UnlockService) Detect(ctx context.Context, group string, report func(UnlockReport)) error {
	if !s.running.TryLock() {
		return ErrUnlockRunning
	}
	defer s.running.Unlock()
	snapshot, err := s.controller.Snapshot(ctx)
	if err != nil {
		return err
	}
	var nodes []string
	for _, node := range leafNodes(snapshot, []string{group}) {
		if delay, tested := snapshot.Delays[node]; tested && delay == 0 {
			continue
		}
		nodes = append(nodes, node)
	}
	for i, node := range nodes {
		if err := s.controller.SelectProxy(ctx, model.ProbeGroup, node); err != nil {
			return fmt.Errorf("pick node %q in the probe group: %w", node, err)
		}
		result, checkErr := s.checker.Check(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		picked, err := s.controller.SelectedProxy(ctx, model.ProbeGroup)
		if err != nil {
			return fmt.Errorf("read the probe group after checking node %q: %w", node, err)
		}
		if picked != node {
			return fmt.Errorf("the probe group moved from node %q to %q during its check: the core reloaded", node, picked)
		}
		if checkErr != nil {
			slog.Warn("unlock check incomplete", "node", node, "error", checkErr)
		}
		report(UnlockReport{Node: node, Result: result, Done: i + 1, Total: len(nodes)})
	}
	return nil
}
