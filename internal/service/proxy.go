// Package service holds Hop's business logic. It depends on the core only
// through the Controller interface.
package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"hop/internal/model"
)

// Controller is what the service needs from a running core.
type Controller interface {
	Snapshot(ctx context.Context) (model.Snapshot, error)
	Mode(ctx context.Context) (string, error)
	SelectProxy(ctx context.Context, group string, proxy string) error
	TestGroupDelay(ctx context.Context, group string, testURL string, timeout time.Duration) (map[string]int, error)
	StreamTraffic(ctx context.Context, handle func(model.Traffic)) error
}

const (
	// delayTestURL answers 204 with no body, so the delay measures the
	// route rather than the page.
	delayTestURL     = "https://www.gstatic.com/generate_204"
	delayTestTimeout = 5 * time.Second
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

// TestDelay measures every node of group. The results show up in the next
// View.
func (s *ProxyService) TestDelay(ctx context.Context, group string) error {
	_, err := s.controller.TestGroupDelay(ctx, group, delayTestURL, delayTestTimeout)
	return err
}

// autoGroupTypes pick their node from delay tests.
var autoGroupTypes = []string{model.GroupURLTest, model.GroupFallback, model.GroupLoadBalance}

// TestAutoGroups measures the groups that pick their node by delay, which
// also makes them pick again from fresh results. Before any test such a
// group sits on its first option, which may not be a usable node. Without
// such groups it measures the first group, so its nodes show a delay.
func (s *ProxyService) TestAutoGroups(ctx context.Context) error {
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
	var errs []error
	for _, group := range targets {
		if _, err := s.controller.TestGroupDelay(ctx, group, delayTestURL, delayTestTimeout); err != nil {
			errs = append(errs, fmt.Errorf("test group %q: %w", group, err))
		}
	}
	return errors.Join(errs...)
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
