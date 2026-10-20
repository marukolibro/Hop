package service

import (
	"context"
	"fmt"
)

// CoreApplier sends every config to the core that serves Hop's controller:
// the helper's while TUN is on, Hop's own otherwise. A config applied to the
// wrong core would either be refused or silently drop the tun section.
type CoreApplier struct {
	local  ConfigApplier
	helper ConfigApplier
	state  *TunFlag
}

// NewCoreApplier returns an applier that writes to local until TUN turns
// on, then to the helper's core.
func NewCoreApplier(local ConfigApplier, helperConfig ConfigApplier, state *TunFlag) *CoreApplier {
	return &CoreApplier{local: local, helper: helperConfig, state: state}
}

// ApplyConfig makes the core serving Hop's controller now switch to config.
func (a *CoreApplier) ApplyConfig(ctx context.Context, config []byte) error {
	if a.state.On() {
		if err := a.helper.ApplyConfig(ctx, config); err != nil {
			return fmt.Errorf("the helper's core did not accept the config: %w", err)
		}
		return nil
	}
	return a.local.ApplyConfig(ctx, config)
}
