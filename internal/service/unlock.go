package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"hop/internal/model"
)

// UnlockChecker asks the services whether they work through the mixed port,
// where the rules send each one as they send the user's own traffic. It hands
// each service's result to report as it comes, one at a time.
type UnlockChecker interface {
	Check(ctx context.Context, report func(model.UnlockResult)) error
}

// ErrUnlockRunning is returned when a check is asked for while another one is
// going.
var ErrUnlockRunning = errors.New("an unlock check is already running")

// UnlockService finds which services work through the rules, as the user's
// own traffic goes.
type UnlockService struct {
	checker UnlockChecker
	running sync.Mutex
}

// NewUnlockService returns a service checking through checker.
func NewUnlockService(checker UnlockChecker) *UnlockService {
	return &UnlockService{checker: checker}
}

// CheckServices checks every service and hands each result to report as it
// comes. A service that could not be checked is reported unknown, and the
// reason is logged.
func (s *UnlockService) CheckServices(ctx context.Context, report func(model.UnlockResult)) error {
	if !s.running.TryLock() {
		return ErrUnlockRunning
	}
	defer s.running.Unlock()
	err := s.checker.Check(ctx, report)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		slog.Warn("service check incomplete", "error", err)
	}
	return nil
}
