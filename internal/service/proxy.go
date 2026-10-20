// Package service holds Hop's business logic. It depends on the core only
// through the Controller interface.
package service

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"hop/internal/model"
)

// Controller is what the service needs from a running core.
type Controller interface {
	Snapshot(ctx context.Context) (model.Snapshot, error)
	Mode(ctx context.Context) (string, error)
	SelectProxy(ctx context.Context, group string, proxy string) error
	TestProxyDelay(ctx context.Context, proxy string, testURL string, timeout time.Duration) (int, error)
	StreamTraffic(ctx context.Context, handle func(model.Traffic)) error
}

const (
	// delayTestURL answers 204 with no body, so the delay measures the
	// route rather than the page.
	delayTestURL = "https://www.gstatic.com/generate_204"
	// delayTestWorkers is how many nodes are tested at once. The core takes
	// them all at once; Hop bounds it so a profile of hundreds of nodes does
	// not open hundreds of connections together.
	delayTestWorkers = 32
)

// ProxyService exposes the proxy groups and live traffic of the core.
type ProxyService struct {
	controller Controller
}

// NewProxyService returns a service backed by controller.
func NewProxyService(controller Controller) *ProxyService {
	return &ProxyService{controller: controller}
}

// View returns the groups, their nodes by region, the account details and
// the route traffic takes now.
func (s *ProxyService) View(ctx context.Context) (model.ProxyView, error) {
	snapshot, err := s.controller.Snapshot(ctx)
	if err != nil {
		return model.ProxyView{}, err
	}
	mode, err := s.controller.Mode(ctx)
	if err != nil {
		return model.ProxyView{}, err
	}
	return BuildView(snapshot, mode), nil
}

// TestDelay measures every node of group, those of the groups inside it
// too. The results show up in the next View, one node after the other as they
// answer.
// A node that has not answered within timeout counts as down.
func (s *ProxyService) TestDelay(ctx context.Context, group string, timeout time.Duration) error {
	snapshot, err := s.controller.Snapshot(ctx)
	if err != nil {
		return err
	}
	return s.testNodes(ctx, leafNodes(snapshot, []string{group}), timeout)
}

// autoGroupTypes pick their node from delay tests.
var autoGroupTypes = []string{model.GroupURLTest, model.GroupFallback, model.GroupLoadBalance}

// TestAutoGroups measures the nodes of the groups that pick their node by
// delay, so that they pick from fresh results. Before any test such a group
// sits on its first option, which may not be a usable node. Without such
// groups it measures the first group, so its nodes show a delay.
func (s *ProxyService) TestAutoGroups(ctx context.Context, timeout time.Duration) error {
	snapshot, err := s.controller.Snapshot(ctx)
	if err != nil {
		return err
	}
	var targets []string
	for _, g := range snapshot.Groups {
		if slices.Contains(autoGroupTypes, g.Type) {
			targets = append(targets, g.Name)
		}
	}
	if len(targets) == 0 && len(snapshot.Groups) > 0 {
		targets = []string{snapshot.Groups[0].Name}
	}
	return s.testNodes(ctx, leafNodes(snapshot, targets), timeout)
}

// testNodes measures nodes concurrently, at most delayTestWorkers at a time.
// A node that does not answer is a result, not a failure; only a request the
// core could not take is, and the first of those is returned with how many
// there were.
func (s *ProxyService) testNodes(ctx context.Context, nodes []string, timeout time.Duration) error {
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		failed int
		first  error
		slots  = make(chan struct{}, delayTestWorkers)
	)
	for _, node := range nodes {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			if _, err := s.controller.TestProxyDelay(ctx, node, delayTestURL, timeout); err != nil && ctx.Err() == nil {
				mu.Lock()
				defer mu.Unlock()
				failed++
				if first == nil {
					first = fmt.Errorf("test node %q: %w", node, err)
				}
			}
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	if first != nil {
		return fmt.Errorf("%d of %d nodes could not be tested, first: %w", failed, len(nodes), first)
	}
	return nil
}

// leafNodes lists the nodes behind groups, once each and in order: the
// options that are no group, following groups inside groups. Built-in
// outbounds and the account details providers list as nodes are no nodes to
// test: the latter never answer and would hold a test until its timeout.
func leafNodes(snapshot model.Snapshot, groups []string) []string {
	byName := make(map[string]model.ProxyGroup, len(snapshot.Groups))
	for _, g := range snapshot.Groups {
		byName[g.Name] = g
	}
	var leaves []string
	seen := map[string]bool{}
	var walk func(name string)
	walk = func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		if g, isGroup := byName[name]; isGroup {
			for _, option := range g.Options {
				walk(option)
			}
			return
		}
		if name == "DIRECT" || name == "REJECT" || isPlanNote(name) {
			return
		}
		leaves = append(leaves, name)
	}
	for _, name := range groups {
		walk(name)
	}
	return leaves
}

// Select makes group use proxy.
func (s *ProxyService) Select(ctx context.Context, group string, proxy string) error {
	return s.controller.SelectProxy(ctx, group, proxy)
}

// WatchTraffic calls handle for every traffic sample until ctx is done or
// the stream fails.
func (s *ProxyService) WatchTraffic(ctx context.Context, handle func(model.Traffic)) error {
	return s.controller.StreamTraffic(ctx, handle)
}

// PushSample returns history with sample appended, keeping at most limit
// samples. The input is not modified.
func PushSample(history model.TrafficHistory, sample model.Traffic, limit int) model.TrafficHistory {
	return model.TrafficHistory{
		Up:   pushBounded(history.Up, float32(sample.Up), limit),
		Down: pushBounded(history.Down, float32(sample.Down), limit),
	}
}

func pushBounded(values []float32, value float32, limit int) []float32 {
	start := max(0, len(values)+1-limit)
	out := make([]float32, 0, len(values)+1-start)
	out = append(out, values[start:]...)
	return append(out, value)
}
