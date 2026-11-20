package service

import (
	"context"
	"fmt"
)

// CoreApplier sends every config to the core that serves Hop's controller:
// the helper's once the core runs there, Hop's own otherwise. A config
// applied to the wrong core would be refused, as no core would answer it.
type CoreApplier struct {
	local     ConfigApplier
	helper    ConfigApplier
	placement *HelperCoreFlag
}

// NewCoreApplier returns an applier that writes to local until the core
// moves under the helper, then to the helper's core.
func NewCoreApplier(local ConfigApplier, helperConfig ConfigApplier, placement *HelperCoreFlag) *CoreApplier {
	return &CoreApplier{local: local, helper: helperConfig, placement: placement}
}

// ApplyConfig makes the core serving Hop's controller now switch to config.
func (a *CoreApplier) ApplyConfig(ctx context.Context, config []byte) error {
	if a.placement.On() {
		if err := a.helper.ApplyConfig(ctx, config); err != nil {
			return fmt.Errorf("the helper's core did not accept the config: %w", err)
		}
		return nil
	}
	return a.local.ApplyConfig(ctx, config)
}
