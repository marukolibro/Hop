package service

import (
	"context"
	"fmt"
	"slices"

	"hop/internal/model"
)

// Modes are the outbound modes Hop offers, in the order it shows them.
var Modes = []string{model.ModeRule, model.ModeGlobal, model.ModeDirect}

// ModeController reads and changes the running core's outbound mode.
type ModeController interface {
	Mode(ctx context.Context) (string, error)
	SetMode(ctx context.Context, mode string) error
}

// ModeStore keeps the outbound mode across runs.
type ModeStore interface {
	SaveMode(mode string) error
	LoadMode() (string, bool, error)
}

// ModeService switches the outbound mode and remembers it.
type ModeService struct {
	core  ModeController
	store ModeStore
}

// NewModeService returns a service that switches core's mode and keeps it
// in store.
func NewModeService(core ModeController, store ModeStore) *ModeService {
	return &ModeService{core: core, store: store}
}

// Current returns the mode the core runs in.
func (s *ModeService) Current(ctx context.Context) (string, error) {
	return s.core.Mode(ctx)
}

// Set switches the core to mode and keeps it for the next start.
func (s *ModeService) Set(ctx context.Context, mode string) error {
	if !slices.Contains(Modes, mode) {
		return fmt.Errorf("unknown outbound mode %q (want one of %v)", mode, Modes)
	}
	if err := s.core.SetMode(ctx, mode); err != nil {
		return err
	}
	return s.store.SaveMode(mode)
}

// StartupMode returns the mode saved last time, and rule mode on the first
// start.
func StartupMode(store ModeStore) (string, error) {
	mode, found, err := store.LoadMode()
	if err != nil {
		return "", err
	}
	if !found {
		return model.ModeRule, nil
	}
	if !slices.Contains(Modes, mode) {
		return "", fmt.Errorf("saved outbound mode %q is not one of %v", mode, Modes)
	}
	return mode, nil
}
