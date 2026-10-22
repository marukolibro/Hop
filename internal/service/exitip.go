package service

import (
	"context"

	"hop/internal/model"
)

// ExitIPLookup reads the public address the default route leaves by.
type ExitIPLookup interface {
	Lookup(ctx context.Context) (model.ExitIP, error)
}

// ExitIPService answers which public address Hop's traffic leaves by.
type ExitIPService struct {
	lookup ExitIPLookup
}

// NewExitIPService returns a service reading through lookup.
func NewExitIPService(lookup ExitIPLookup) *ExitIPService {
	return &ExitIPService{lookup: lookup}
}

// Lookup returns the exit IP, or why it could not be read.
func (s *ExitIPService) Lookup(ctx context.Context) (model.ExitIP, error) {
	return s.lookup.Lookup(ctx)
}
