package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"hop/internal/model"
)

// ErrNoSubscription is returned when an update is asked for before any
// subscription was imported.
var ErrNoSubscription = errors.New("no subscription has been imported yet")

// Downloader fetches the profile behind a subscription link.
type Downloader interface {
	Fetch(ctx context.Context, rawURL string) (model.Download, error)
}

// SubscriptionStore keeps the imported subscription across runs.
type SubscriptionStore interface {
	Save(sub model.Subscription) error
	Load() (model.Subscription, bool, error)
}

// ConfigApplier makes the running core use a new config.
type ConfigApplier interface {
	ApplyConfig(ctx context.Context, config []byte) error
}

// ProfileService imports and refreshes the subscription the core runs.
type ProfileService struct {
	downloader Downloader
	store      SubscriptionStore
	applier    ConfigApplier
	settings   RuntimeSettings
}

// NewProfileService returns a service that builds every config under
// settings.
func NewProfileService(downloader Downloader, store SubscriptionStore, applier ConfigApplier, settings RuntimeSettings) *ProfileService {
	return &ProfileService{downloader: downloader, store: store, applier: applier, settings: settings}
}

// Import downloads the subscription at link, makes the core run it, and
// keeps it. Nothing is kept when any step fails, so the core and the store
// never disagree.
func (s *ProfileService) Import(ctx context.Context, link string) error {
	link = strings.TrimSpace(link)
	download, err := s.downloader.Fetch(ctx, link)
	if err != nil {
		return err
	}
	config, err := RuntimeConfig(download.Body, s.settings)
	if err != nil {
		return err
	}
	if err := s.applier.ApplyConfig(ctx, config); err != nil {
		return fmt.Errorf("the core did not accept the subscription: %w", err)
	}
	return s.store.Save(model.Subscription{URL: link, Raw: download.Body, UserInfo: download.UserInfo, UpdatedAt: time.Now()})
}

// Update downloads the stored subscription's link again.
func (s *ProfileService) Update(ctx context.Context) error {
	saved, found, err := s.store.Load()
	if err != nil {
		return err
	}
	if !found {
		return ErrNoSubscription
	}
	return s.Import(ctx, saved.URL)
}

// Summary returns what the dashboard shows of the stored subscription,
// and false when none was imported. Only the link's host is shown: the
// rest of it is a credential.
func (s *ProfileService) Summary() (model.SubscriptionSummary, bool, error) {
	saved, found, err := s.store.Load()
	if err != nil || !found {
		return model.SubscriptionSummary{}, false, err
	}
	u, err := url.Parse(saved.URL)
	if err != nil {
		return model.SubscriptionSummary{}, false, fmt.Errorf("stored subscription link is not a valid URL: %w", errors.Unwrap(err))
	}
	summary := model.SubscriptionSummary{Host: u.Host, UpdatedAt: saved.UpdatedAt}
	if saved.UserInfo != "" {
		usage, err := ParseUsage(saved.UserInfo)
		if err != nil {
			return model.SubscriptionSummary{}, false, err
		}
		summary.Usage, summary.HasUsage = usage, true
	}
	return summary, true, nil
}

// StartupConfig returns the config the core should start with: the stored
// subscription if there is one, otherwise the bootstrap profile.
func (s *ProfileService) StartupConfig() ([]byte, error) {
	saved, found, err := s.store.Load()
	if err != nil {
		return nil, err
	}
	if !found {
		return BootstrapConfig(s.settings)
	}
	return RuntimeConfig(saved.Raw, s.settings)
}
