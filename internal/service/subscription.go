package service

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"hop/internal/model"
	"hop/internal/repository/profile"
	"hop/internal/repository/subscription"
)

// rollbackTimeout bounds the cleanup that puts the previous config back: it runs
// on its own fresh context, untouched by the caller's cancel.
const rollbackTimeout = 10 * time.Second

// ErrNoSubscription is returned when an update is asked for before any
// subscription was added.
var ErrNoSubscription = errors.New("no subscription has been added yet")

// ErrLocalProfile is returned when a refresh is asked of a profile the user
// added from a file or pasted text: it has no link to download.
var ErrLocalProfile = errors.New("a local profile has no link to refresh")

// LocalProfileName names a local profile the user left unnamed and did not
// read from a file.
const LocalProfileName = "本地配置"

// MaxProfileSize is the largest profile Hop accepts, from a link or from a
// local file or pasted text, in bytes.
const MaxProfileSize = subscription.MaxSize

// ProfileSources are where a profile can come from, in the order the editor
// shows them.
var ProfileSources = []model.SubscriptionSource{model.SourceRemote, model.SourceLocal}

// Downloader fetches the profile behind a subscription link.
type Downloader interface {
	Fetch(ctx context.Context, rawURL string) (model.Download, error)
}

// SubscriptionStore keeps the added subscriptions across runs. Entries and
// Get read the committed state; StageBody prepares a new body and Commit
// publishes it.
type SubscriptionStore interface {
	// Entries returns the stored subscriptions without their raw bodies, and the
	// active subscription's ID ("" when none is stored).
	Entries() ([]model.Subscription, string, error)
	// Get returns one stored subscription, its raw body included, and false when
	// no subscription has that ID.
	Get(id string) (model.Subscription, bool, error)
	// StageBody writes raw as the body the subscription will reference once
	// committed; until Commit publishes it, staging changes nothing the store
	// reports.
	StageBody(id string, raw []byte) error
	// Commit atomically publishes entries as the store's whole state, activeID
	// included; bodies nothing references any more are removed.
	Commit(entries []model.Subscription, activeID string) error
}

// ConfigApplier makes the running core use a new config.
type ConfigApplier interface {
	ApplyConfig(ctx context.Context, config []byte) error
}

// ConfigValidator has the core itself check a runtime config, which Hop asks
// before committing one.
type ConfigValidator interface {
	Validate(ctx context.Context, config []byte) error
}

// ModeReader reads the outbound mode the core runs in right now.
type ModeReader interface {
	Mode(ctx context.Context) (string, error)
}

// SubscriptionService keeps the downloaded subscriptions and decides which
// one the core runs.
type SubscriptionService struct {
	downloader  Downloader
	store       SubscriptionStore
	customRules CustomRuleStore
	applier     ConfigApplier
	validator   ConfigValidator
	core        ModeReader
	settings    RuntimeSettings
	// tun says whether TUN is on: every config built while a core runs keeps
	// the tun section, or a reload would quietly drop TUN.
	tun *TunFlag
	// mu serializes every read and write: the core and the store are only ever
	// changed one operation at a time, and no read races a commit sweeping old
	// bodies.
	mu sync.Mutex
}

// NewSubscriptionService returns a service that builds every config under
// settings, validates it with validator, applies it through applier and
// reads the mode the core runs in through core. customRules are matched
// ahead of every profile's own rules. tun is the flag saying whether TUN is
// on.
func NewSubscriptionService(downloader Downloader, store SubscriptionStore, customRules CustomRuleStore, applier ConfigApplier, validator ConfigValidator, core ModeReader, settings RuntimeSettings, tun *TunFlag) *SubscriptionService {
	return &SubscriptionService{
		downloader: downloader, store: store, customRules: customRules,
		applier: applier, validator: validator, core: core, settings: settings, tun: tun,
	}
}

// List returns the subscriptions page: one summary per stored
// subscription and the one the core runs.
func (s *SubscriptionService) List(ctx context.Context) (model.SubscriptionsView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, activeID, err := s.store.Entries()
	if err != nil {
		return model.SubscriptionsView{}, err
	}
	items := make([]model.SubscriptionSummary, 0, len(entries))
	for _, entry := range entries {
		summary, err := summarize(entry)
		if err != nil {
			return model.SubscriptionsView{}, err
		}
		items = append(items, summary)
	}
	return model.SubscriptionsView{Items: items, ActiveID: activeID}, nil
}

// Source returns what the edit dialog starts from: the row's name, source and
// rule source, and a remote profile's link. The link is a credential, so it
// goes only to the dialog that edits it. A local profile's content stays in
// the store: the dialog replaces it with what the user pastes or picks.
func (s *SubscriptionService) Source(ctx context.Context, id string) (model.SubscriptionInput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, _, err := s.store.Entries()
	if err != nil {
		return model.SubscriptionInput{}, err
	}
	index := entryIndex(entries, id)
	if index < 0 {
		return model.SubscriptionInput{}, unknownSubscription(id)
	}
	entry := entries[index]
	input := model.SubscriptionInput{Name: entry.Name, Source: entry.Source, Rules: entry.Rules}
	if entry.Source == model.SourceRemote {
		input.URL = entry.URL
	}
	return input, nil
}

// Add stores a new profile: a subscription downloaded from input's link, or a
// config the user read from a local file or pasted. The first one also becomes
// the line the core runs; after that, adding only stores — it never switches
// the running profile.
func (s *SubscriptionService) Add(ctx context.Context, input model.SubscriptionInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	normalized, err := normalizeInput(input)
	if err != nil {
		return err
	}
	entries, activeID, err := s.store.Entries()
	if err != nil {
		return err
	}
	if normalized.Source == model.SourceLocal {
		return s.addLocal(ctx, normalized, entries, activeID)
	}
	return s.addRemote(ctx, normalized, entries, activeID)
}

// addRemote downloads input's link and stores it. The first profile also
// becomes the one the core runs.
func (s *SubscriptionService) addRemote(ctx context.Context, input model.SubscriptionInput, entries []model.Subscription, activeID string) error {
	for _, entry := range entries {
		if entry.URL == input.URL {
			return fmt.Errorf("the subscription from %s is already added", hostOf(entry.URL))
		}
	}
	download, err := s.downloader.Fetch(ctx, input.URL)
	if err != nil {
		return redactedError(err, input.URL)
	}
	if err := checkUsage(download.UserInfo); err != nil {
		return err
	}
	id, err := profile.NewID()
	if err != nil {
		return err
	}
	rules, err := RulesFor(download.Body, input.Rules)
	if err != nil {
		return err
	}
	now := time.Now()
	entry := model.Subscription{
		ID: id, Name: input.Name, Source: model.SourceRemote, URL: input.URL,
		Raw: download.Body, UserInfo: download.UserInfo, UpdatedAt: now, AttemptedAt: now, Rules: rules,
	}
	return s.importProfile(ctx, entry, entries, activeID)
}

// addLocal stores a config the user read from a file or pasted. It goes
// through the same reading, filter and core test as a download; nothing is
// fetched afterwards.
func (s *SubscriptionService) addLocal(ctx context.Context, input model.SubscriptionInput, entries []model.Subscription, activeID string) error {
	if err := checkLocalContent(input.Content); err != nil {
		return err
	}
	id, err := profile.NewID()
	if err != nil {
		return err
	}
	rules, err := RulesFor(input.Content, input.Rules)
	if err != nil {
		return err
	}
	entry := model.Subscription{
		ID: id, Name: input.Name, Source: model.SourceLocal,
		Raw: input.Content, UpdatedAt: time.Now(), Rules: rules,
	}
	return s.importProfile(ctx, entry, entries, activeID)
}

// importProfile checks a new entry against the core and stores it, making it
// the one the core runs when nothing is stored yet. The caller holds the
// service lock.
func (s *SubscriptionService) importProfile(ctx context.Context, entry model.Subscription, entries []model.Subscription, activeID string) error {
	settings, err := s.liveSettings(ctx)
	if err != nil {
		return err
	}
	config, err := s.runtimeConfig(entry.Raw, entry.Rules, settings)
	if err != nil {
		return err
	}
	if err := s.validate(ctx, config); err != nil {
		return err
	}
	updated := append(entries, entry)
	if activeID != "" {
		// A profile is already running: store the new one, the core keeps
		// running what it runs.
		return s.commit(updated, activeID, entry.ID, entry.Raw)
	}
	previous, err := BootstrapConfig(settings)
	if err != nil {
		return err
	}
	return s.switchCore(ctx, config, previous, updated, entry.ID, entry.ID, entry.Raw)
}

// checkLocalContent checks content the user added by hand: it must not be
// empty and is held to the same size limit as a download.
func checkLocalContent(content []byte) error {
	if len(bytes.TrimSpace(content)) == 0 {
		return errors.New("the local config is empty")
	}
	if len(content) > MaxProfileSize {
		return fmt.Errorf("the local config is larger than %d bytes", MaxProfileSize)
	}
	return nil
}

// Edit changes a stored profile's name and rules; a remote profile's link, or
// a local profile's content, can be replaced too. A replacement is read,
// filtered and checked like a fresh add before anything is replaced; a name or
// rule change alone replaces nothing. A profile's source cannot change: the
// editor offers no switch, and this refuses one.
func (s *SubscriptionService) Edit(ctx context.Context, id string, input model.SubscriptionInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	normalized, err := normalizeInput(input)
	if err != nil {
		return err
	}
	entries, activeID, err := s.store.Entries()
	if err != nil {
		return err
	}
	index := entryIndex(entries, id)
	if index < 0 {
		return unknownSubscription(id)
	}
	entry := entries[index]
	if entry.Source != normalized.Source {
		return fmt.Errorf("a profile's source cannot change from %q to %q: remove it and add it again", entry.Source, normalized.Source)
	}
	if entry.Source == model.SourceLocal {
		return s.editLocal(ctx, entries, index, activeID, normalized)
	}

	if entry.URL == normalized.URL {
		return s.editStored(ctx, entries, index, activeID, normalized)
	}
	for other := range entries {
		if other != index && entries[other].URL == normalized.URL {
			return fmt.Errorf("the subscription from %s is already added", hostOf(entries[other].URL))
		}
	}

	download, err := s.downloader.Fetch(ctx, normalized.URL)
	if err != nil {
		return s.recordFailure(id, normalized.URL, redactedError(err, normalized.URL))
	}
	if err := checkUsage(download.UserInfo); err != nil {
		return s.recordFailure(id, normalized.URL, err)
	}
	rules, err := RulesFor(download.Body, normalized.Rules)
	if err != nil {
		return s.recordFailure(id, normalized.URL, err)
	}
	settings, err := s.liveSettings(ctx)
	if err != nil {
		return s.recordFailure(id, normalized.URL, err)
	}
	config, err := s.runtimeConfig(download.Body, rules, settings)
	if err != nil {
		return s.recordFailure(id, normalized.URL, err)
	}
	if err := s.validate(ctx, config); err != nil {
		return s.recordFailure(id, normalized.URL, err)
	}
	now := time.Now()
	fresh := model.Subscription{
		ID: entry.ID, Name: normalized.Name, Source: model.SourceRemote, URL: normalized.URL,
		Raw: download.Body, UserInfo: download.UserInfo, UpdatedAt: now, AttemptedAt: now, Rules: rules,
	}
	if fresh.ID != activeID {
		entries[index] = fresh
		return s.commit(entries, activeID, fresh.ID, fresh.Raw)
	}
	running, found, err := s.store.Get(id)
	if err != nil {
		return err
	}
	if !found {
		return unknownSubscription(id)
	}
	previous, err := s.runtimeConfig(running.Raw, running.Rules, settings)
	if err != nil {
		return s.recordFailure(id, normalized.URL, err)
	}
	entries[index] = fresh
	if err := s.switchCore(ctx, config, previous, entries, activeID, fresh.ID, fresh.Raw); err != nil {
		return s.recordFailure(id, normalized.URL, err)
	}
	return nil
}

// editStored applies an edit that keeps the link: a new name, a new rule
// source, or both. Nothing is downloaded; a rule change of the subscription
// the core runs reloads it on the body that was kept.
func (s *SubscriptionService) editStored(ctx context.Context, entries []model.Subscription, index int, activeID string, input model.SubscriptionInput) error {
	entry := entries[index]
	stored, found, err := s.store.Get(entry.ID)
	if err != nil {
		return err
	}
	if !found {
		return unknownSubscription(entry.ID)
	}
	rules, err := RulesFor(stored.Raw, input.Rules)
	if err != nil {
		return err
	}
	if entry.Name == input.Name && entry.Rules == rules {
		return nil
	}
	previousRules := entry.Rules
	entry.Name, entry.Rules = input.Name, rules
	entries[index] = entry
	if entry.ID != activeID || previousRules == rules {
		return s.store.Commit(entries, activeID)
	}
	settings, err := s.liveSettings(ctx)
	if err != nil {
		return err
	}
	config, err := s.runtimeConfig(stored.Raw, rules, settings)
	if err != nil {
		return err
	}
	if err := s.validate(ctx, config); err != nil {
		return err
	}
	previous, err := s.runtimeConfig(stored.Raw, previousRules, settings)
	if err != nil {
		return err
	}
	return s.switchCore(ctx, config, previous, entries, activeID, "", nil)
}

// editLocal applies an edit that keeps the profile local: a new name, new
// rules, new content, or any of them together. Empty content keeps the stored
// one, so renaming reads and applies nothing; a rule or content change of the
// profile the core runs reloads it.
func (s *SubscriptionService) editLocal(ctx context.Context, entries []model.Subscription, index int, activeID string, input model.SubscriptionInput) error {
	entry := entries[index]
	stored, found, err := s.store.Get(entry.ID)
	if err != nil {
		return err
	}
	if !found {
		return unknownSubscription(entry.ID)
	}
	raw, replaced := stored.Raw, false
	if len(input.Content) > 0 && !bytes.Equal(input.Content, stored.Raw) {
		if err := checkLocalContent(input.Content); err != nil {
			return err
		}
		raw, replaced = input.Content, true
	}
	rules, err := RulesFor(raw, input.Rules)
	if err != nil {
		return err
	}
	if entry.Name == input.Name && entry.Rules == rules && !replaced {
		return nil
	}
	previousRules := entry.Rules
	entry.Name, entry.Rules, entry.Source = input.Name, rules, model.SourceLocal
	if replaced {
		entry.Raw = raw
		entry.UpdatedAt = time.Now()
	}
	entries[index] = entry
	if entry.ID != activeID {
		if replaced {
			return s.commit(entries, activeID, entry.ID, entry.Raw)
		}
		return s.store.Commit(entries, activeID)
	}
	if previousRules == rules && !replaced {
		return s.store.Commit(entries, activeID)
	}
	settings, err := s.liveSettings(ctx)
	if err != nil {
		return err
	}
	config, err := s.runtimeConfig(raw, rules, settings)
	if err != nil {
		return err
	}
	if err := s.validate(ctx, config); err != nil {
		return err
	}
	previous, err := s.runtimeConfig(stored.Raw, previousRules, settings)
	if err != nil {
		return err
	}
	if !replaced {
		return s.switchCore(ctx, config, previous, entries, activeID, "", nil)
	}
	return s.switchCore(ctx, config, previous, entries, activeID, entry.ID, entry.Raw)
}

// Use makes the core run the stored subscription id. Switching is offline: it
// runs exactly the body that was kept.
func (s *SubscriptionService) Use(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, activeID, err := s.store.Entries()
	if err != nil {
		return err
	}
	index := entryIndex(entries, id)
	if index < 0 {
		return unknownSubscription(id)
	}
	if entries[index].ID == activeID {
		return nil
	}
	settings, err := s.liveSettings(ctx)
	if err != nil {
		return err
	}
	target, found, err := s.store.Get(id)
	if err != nil {
		return err
	}
	if !found {
		return unknownSubscription(id)
	}
	config, err := s.runtimeConfig(target.Raw, target.Rules, settings)
	if err != nil {
		return err
	}
	var previous []byte
	if activeID == "" {
		if previous, err = BootstrapConfig(settings); err != nil {
			return err
		}
	} else {
		running, found, err := s.store.Get(activeID)
		if err != nil {
			return err
		}
		if !found {
			return unknownSubscription(activeID)
		}
		if previous, err = s.runtimeConfig(running.Raw, running.Rules, settings); err != nil {
			return err
		}
	}
	return s.switchCore(ctx, config, previous, entries, id, "", nil)
}

// Update downloads the subscription id's link again. When any step fails, the
// row keeps its last successful body and time, with the failure noted on it. A
// local profile has no link: it returns ErrLocalProfile and is left untouched.
func (s *SubscriptionService) Update(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refresh(ctx, id)
}

// UpdateAll refreshes every stored subscription. One failing does not stop the
// others: its row keeps its last success and notes the cause, and every failure
// is returned joined. A local profile has no link and is skipped, untouched.
func (s *SubscriptionService) UpdateAll(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, _, err := s.store.Entries()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return ErrNoSubscription
	}
	var failures []error
	for _, entry := range entries {
		if entry.Source == model.SourceLocal {
			continue
		}
		if err := s.refresh(ctx, entry.ID); err != nil {
			failures = append(failures, fmt.Errorf("refresh %s: %w", entry.Name, err))
		}
	}
	return errors.Join(failures...)
}

// AutoUpdate refreshes every stored subscription that is due, by the times
// the store keeps: the last success and the last attempt. A local profile is
// never due: it has no link, and no attempt is recorded for it. A failed refresh
// moves the attempt time, so it is retried after a whole interval rather than
// at every check, and a Mac waking from sleep catches up at the first check
// after the interval, not a full ticker period later. interval <= 0 turns the
// automatic refresh off. The whole pass holds the service lock, so it never
// runs beside a manual refresh or an apply. Each failure is logged with the
// link's host only and noted on its row, as a manual refresh does. It returns
// whether any row was attempted, and the failures joined.
func (s *SubscriptionService) AutoUpdate(ctx context.Context, interval time.Duration) (bool, error) {
	if interval <= 0 {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, _, err := s.store.Entries()
	if err != nil {
		return false, err
	}
	now := time.Now()
	attempted := false
	var failures []error
	for _, entry := range entries {
		if ctx.Err() != nil {
			break
		}
		if entry.Source == model.SourceLocal {
			// No link, no refresh: a local profile never takes an attempt.
			continue
		}
		last := entry.UpdatedAt
		if entry.AttemptedAt.After(last) {
			last = entry.AttemptedAt
		}
		if !last.IsZero() && now.Sub(last) < interval {
			continue
		}
		attempted = true
		if err := s.refresh(ctx, entry.ID); err != nil {
			// The name is user text and may be anything; the ID and host
			// say which row failed without risking the link.
			slog.Warn("automatic subscription refresh failed",
				"subscription", entry.ID, "host", hostOf(entry.URL), "error", err)
			failures = append(failures, fmt.Errorf("refresh %s: %w", entry.ID, err))
		}
	}
	return attempted, errors.Join(failures...)
}

// Delete removes a stored subscription. Deleting one the core does not run
// changes nothing at runtime; deleting the running one first makes the core run
// replacementID — or the built-in bootstrap profile when it was the last — and
// only then removes the entry.
func (s *SubscriptionService) Delete(ctx context.Context, id string, replacementID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, activeID, err := s.store.Entries()
	if err != nil {
		return err
	}
	index := entryIndex(entries, id)
	if index < 0 {
		return unknownSubscription(id)
	}
	if entries[index].ID != activeID {
		// Not the one running: a store-only delete.
		return s.store.Commit(slices.Delete(entries, index, index+1), activeID)
	}

	settings, err := s.liveSettings(ctx)
	if err != nil {
		return err
	}
	running, found, err := s.store.Get(id)
	if err != nil {
		return err
	}
	if !found {
		return unknownSubscription(id)
	}
	previous, err := s.runtimeConfig(running.Raw, running.Rules, settings)
	if err != nil {
		return err
	}
	remaining := slices.Delete(entries, index, index+1)

	if len(remaining) == 0 {
		if replacementID != "" {
			return errors.New("the last subscription cannot be replaced by another one: delete it without a replacement")
		}
		// Deleting the last one: after the user's confirmation the core falls
		// back to the built-in bootstrap profile.
		bootstrap, err := BootstrapConfig(settings)
		if err != nil {
			return err
		}
		return s.switchCore(ctx, bootstrap, previous, nil, "", "", nil)
	}
	if replacementID == "" {
		return errors.New("deleting the subscription in use needs the one to run next")
	}
	if entryIndex(remaining, replacementID) < 0 {
		return fmt.Errorf("replacement subscription %q is not stored", replacementID)
	}
	target, found, err := s.store.Get(replacementID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("replacement subscription %q is not stored", replacementID)
	}
	config, err := s.runtimeConfig(target.Raw, target.Rules, settings)
	if err != nil {
		return err
	}
	return s.switchCore(ctx, config, previous, remaining, replacementID, "", nil)
}

// StartupConfig returns the config the core should start with: the active
// subscription's stored body, or the bootstrap profile when none is
// stored. Nothing is downloaded: a restart runs what was last kept.
func (s *SubscriptionService) StartupConfig() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startupConfigWith(s.settings)
}

// StartupConfigTun returns the startup config with TUN on or off, on the
// saved startup mode: exactly what the core would run started cold.
func (s *SubscriptionService) StartupConfigTun(tun bool) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	settings := s.settings
	settings.Tun = tun
	return s.startupConfigWith(settings)
}

// LiveConfigTun returns the config for switching TUN while a core runs,
// keeping the outbound mode the running core is in.
func (s *SubscriptionService) LiveConfigTun(ctx context.Context, tun bool) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	settings, err := s.liveSettings(ctx)
	if err != nil {
		return nil, err
	}
	settings.Tun = tun
	return s.startupConfigWith(settings)
}

// startupConfigWith builds the config the core should start with under
// settings: the active subscription's stored body, or the bootstrap profile
// when none is stored. Nothing is downloaded.
func (s *SubscriptionService) startupConfigWith(settings RuntimeSettings) ([]byte, error) {
	entries, activeID, err := s.store.Entries()
	if err != nil {
		return nil, err
	}
	if activeID == "" {
		return BootstrapConfig(settings)
	}
	if entryIndex(entries, activeID) < 0 {
		return nil, unknownSubscription(activeID)
	}
	active, found, err := s.store.Get(activeID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, unknownSubscription(activeID)
	}
	return s.runtimeConfig(active.Raw, active.Rules, settings)
}

// CurrentConfig returns the config the core runs now: the active
// subscription with the live mode and TUN state, or the bootstrap profile.
// Geodata validation checks a new download against exactly this.
func (s *SubscriptionService) CurrentConfig(ctx context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	settings, err := s.liveSettings(ctx)
	if err != nil {
		return nil, err
	}
	return s.startupConfigWith(settings)
}

// ReloadRuntime rebuilds the running config on the current store and applies
// it. It is how a change of the custom rules reaches the core: the list is
// already saved, so no subscription entry changes and nothing is committed.
// With no subscription in use there is nothing to rebuild.
func (s *SubscriptionService) ReloadRuntime(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, activeID, err := s.store.Entries()
	if err != nil {
		return err
	}
	if activeID == "" {
		return nil
	}
	if entryIndex(entries, activeID) < 0 {
		return unknownSubscription(activeID)
	}
	active, found, err := s.store.Get(activeID)
	if err != nil {
		return err
	}
	if !found {
		return unknownSubscription(activeID)
	}
	settings, err := s.liveSettings(ctx)
	if err != nil {
		return err
	}
	config, err := s.runtimeConfig(active.Raw, active.Rules, settings)
	if err != nil {
		return err
	}
	if err := s.validate(ctx, config); err != nil {
		return err
	}
	if err := s.applier.ApplyConfig(ctx, config); err != nil {
		return fmt.Errorf("the core did not accept the config: %w", err)
	}
	return nil
}

// LockRuntime keeps a change of the core's own data, such as replacing the
// geodata it loads at start, from racing a subscription apply: the geodata
// service holds this lock while it replaces files and restarts the core.
func (s *SubscriptionService) LockRuntime() { s.mu.Lock() }

// UnlockRuntime releases what LockRuntime took.
func (s *SubscriptionService) UnlockRuntime() { s.mu.Unlock() }

// refresh downloads the subscription id's link again and records the outcome: a
// success swaps in the new body and time, a failure keeps the last success and
// notes why. Every call reloads the committed state, so uncommitted changes are
// never carried into a later commit; refreshing an entry that is not the one
// running never touches the core.
func (s *SubscriptionService) refresh(ctx context.Context, id string) error {
	entries, activeID, err := s.store.Entries()
	if err != nil {
		return err
	}
	index := entryIndex(entries, id)
	if index < 0 {
		return unknownSubscription(id)
	}
	entry, found, err := s.store.Get(id)
	if err != nil {
		return err
	}
	if !found {
		return unknownSubscription(id)
	}
	if entry.Source == model.SourceLocal {
		return ErrLocalProfile
	}
	download, err := s.downloader.Fetch(ctx, entry.URL)
	if err != nil {
		return s.recordFailure(id, entry.URL, redactedError(err, entry.URL))
	}
	if err := checkUsage(download.UserInfo); err != nil {
		return s.recordFailure(id, entry.URL, err)
	}
	settings, err := s.liveSettings(ctx)
	if err != nil {
		return s.recordFailure(id, entry.URL, err)
	}
	// A provider can change formats: a link list that comes back still
	// needs Hop's rules.
	rules, err := RulesFor(download.Body, entry.Rules)
	if err != nil {
		return s.recordFailure(id, entry.URL, err)
	}
	config, err := s.runtimeConfig(download.Body, rules, settings)
	if err != nil {
		return s.recordFailure(id, entry.URL, err)
	}
	if err := s.validate(ctx, config); err != nil {
		return s.recordFailure(id, entry.URL, err)
	}
	now := time.Now()
	fresh := entry
	fresh.Raw, fresh.UserInfo = download.Body, download.UserInfo
	fresh.UpdatedAt, fresh.AttemptedAt, fresh.LastError, fresh.Rules = now, now, "", rules
	entries[index] = fresh
	if id != activeID {
		// Not the one running: the refresh writes the store only.
		if err := s.store.StageBody(id, fresh.Raw); err != nil {
			return err
		}
		return s.store.Commit(entries, activeID)
	}
	previous, err := s.runtimeConfig(entry.Raw, entry.Rules, settings)
	if err != nil {
		return s.recordFailure(id, entry.URL, err)
	}
	if err := s.switchCore(ctx, config, previous, entries, activeID, id, fresh.Raw); err != nil {
		return s.recordFailure(id, entry.URL, err)
	}
	return nil
}

// recordFailure notes the failure on the row: body and time stay at the last
// success, and no link appears in the note — a link is a credential. The
// committed state is reloaded and only this row changes, so a failing commit
// never publishes uncommitted changes made elsewhere.
func (s *SubscriptionService) recordFailure(id string, attemptURL string, cause error) error {
	entries, activeID, err := s.store.Entries()
	if err != nil {
		return errors.Join(cause, fmt.Errorf("note the failed refresh in the store: %w", err))
	}
	index := entryIndex(entries, id)
	if index < 0 {
		return errors.Join(cause, unknownSubscription(id))
	}
	failed := entries[index]
	failed.LastError = redactNote(cause.Error(), entries, attemptURL)
	failed.AttemptedAt = time.Now()
	entries[index] = failed
	if err := s.store.Commit(entries, activeID); err != nil {
		return errors.Join(cause, fmt.Errorf("note the failed refresh in the store: %w", err))
	}
	return cause
}

// commit stages the new body and publishes the entries atomically; the running
// core is never touched.
func (s *SubscriptionService) commit(entries []model.Subscription, activeID string, stagedID string, stagedRaw []byte) error {
	if err := s.store.StageBody(stagedID, stagedRaw); err != nil {
		return err
	}
	return s.store.Commit(entries, activeID)
}

// switchCore makes the core run config and then commits entries as the store's
// whole state. When the commit fails, the core is put back on previous with its
// own bounded context; the error says whether that worked and never claims the
// runtime is unchanged when it is not.
func (s *SubscriptionService) switchCore(ctx context.Context, config []byte, previous []byte, entries []model.Subscription, activeID string, stagedID string, stagedRaw []byte) error {
	// The body to keep is written before the core is touched: the commit itself
	// is then one small index swap.
	if len(stagedRaw) > 0 {
		if err := s.store.StageBody(stagedID, stagedRaw); err != nil {
			return err
		}
	}
	if err := s.applier.ApplyConfig(ctx, config); err != nil {
		return fmt.Errorf("the core did not accept the config: %w", err)
	}
	commitErr := s.store.Commit(entries, activeID)
	if commitErr == nil {
		return nil
	}
	rollbackCtx, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
	defer cancel()
	if rollbackErr := s.applier.ApplyConfig(rollbackCtx, previous); rollbackErr != nil {
		return errors.Join(
			fmt.Errorf("keep the change in the store: %w", commitErr),
			fmt.Errorf("putting the core back failed, it may not run the stored profile: %w", rollbackErr),
		)
	}
	return fmt.Errorf("keep the change in the store, the core was put back: %w", commitErr)
}

// liveSettings returns the settings with the mode the core runs in now and
// whether TUN is on: applying any config preserves both, so a reload never
// drops the tun section while the helper's core serves it.
func (s *SubscriptionService) liveSettings(ctx context.Context) (RuntimeSettings, error) {
	mode, err := s.core.Mode(ctx)
	if err != nil {
		return RuntimeSettings{}, fmt.Errorf("read the outbound mode the core runs in: %w", err)
	}
	settings := s.settings
	settings.Mode = mode
	settings.Tun = s.tun.On()
	return settings, nil
}

// runtimeConfig builds a config with the stored custom rules ahead of the
// profile's own. The rules are checked every time: a hand-edited file never
// reaches the core unchecked.
func (s *SubscriptionService) runtimeConfig(subscription []byte, rules model.RuleSource, settings RuntimeSettings) ([]byte, error) {
	custom, err := s.customRules.LoadCustomRules()
	if err != nil {
		return nil, fmt.Errorf("read the custom rules: %w", err)
	}
	custom, err = NormalizeCustomRules(custom)
	if err != nil {
		return nil, fmt.Errorf("stored custom rules: %w", err)
	}
	return RuntimeConfig(subscription, rules, custom, settings)
}

// validate has the core check the config itself; validation is never silently
// skipped.
func (s *SubscriptionService) validate(ctx context.Context, config []byte) error {
	if err := s.validator.Validate(ctx, config); err != nil {
		return fmt.Errorf("the core's config test rejected the subscription: %w", err)
	}
	return nil
}

// summarize turns one stored record into its page summary. Only a remote
// profile's host is shown: the rest of the link is a credential. A local one
// has no host, usage or expiry.
func summarize(entry model.Subscription) (model.SubscriptionSummary, error) {
	summary := model.SubscriptionSummary{
		ID: entry.ID, Name: entry.Name, Source: entry.Source,
		UpdatedAt: entry.UpdatedAt, LastError: entry.LastError, Rules: entry.Rules,
	}
	if entry.Source == model.SourceLocal {
		return summary, nil
	}
	u, err := url.Parse(entry.URL)
	if err != nil {
		return model.SubscriptionSummary{}, fmt.Errorf("stored subscription link is not a valid URL: %w", errors.Unwrap(err))
	}
	summary.Host = u.Host
	if entry.UserInfo != "" {
		usage, err := ParseUsage(entry.UserInfo)
		if err != nil {
			return model.SubscriptionSummary{}, err
		}
		summary.Usage, summary.HasUsage = usage, true
	}
	return summary, nil
}

// normalizeInput trims what the user typed and fills an empty name: a remote
// profile takes its link's host, a local one the short local label. A remote
// link is checked as one Hop can download; a local content is held to the
// download size limit and read for its format later, like any other body.
func normalizeInput(input model.SubscriptionInput) (model.SubscriptionInput, error) {
	if !slices.Contains(ProfileSources, input.Source) {
		return model.SubscriptionInput{}, fmt.Errorf("unknown profile source %q", input.Source)
	}
	if !slices.Contains(RuleSources, input.Rules) {
		return model.SubscriptionInput{}, fmt.Errorf("unknown rule source %q", input.Rules)
	}
	name := strings.TrimSpace(input.Name)
	if input.Source == model.SourceLocal {
		if len(input.Content) > MaxProfileSize {
			return model.SubscriptionInput{}, fmt.Errorf("the local config is larger than %d bytes", MaxProfileSize)
		}
		return model.SubscriptionInput{
			Name: cmp.Or(name, LocalProfileName), Source: model.SourceLocal,
			Content: input.Content, Rules: input.Rules,
		}, nil
	}
	rawURL := strings.TrimSpace(input.URL)
	if rawURL == "" {
		return model.SubscriptionInput{}, errors.New("subscription link is empty")
	}
	if _, err := subscription.Redact(rawURL); err != nil {
		return model.SubscriptionInput{}, err
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return model.SubscriptionInput{}, fmt.Errorf("subscription link is not a valid URL: %w", errors.Unwrap(err))
	}
	return model.SubscriptionInput{
		Name: cmp.Or(name, u.Host), Source: model.SourceRemote, URL: rawURL, Rules: input.Rules,
	}, nil
}

// checkUsage validates the provider's usage header: a non-empty one must parse,
// and bad metadata is never stored — stored, it would make the whole list fail
// to open.
func checkUsage(userInfo string) error {
	if userInfo == "" {
		return nil
	}
	if _, err := ParseUsage(userInfo); err != nil {
		return fmt.Errorf("the provider's usage header is not valid: %w", err)
	}
	return nil
}

// entryIndex finds a subscription by its ID, -1 when it is not stored.
func entryIndex(entries []model.Subscription, id string) int {
	return slices.IndexFunc(entries, func(entry model.Subscription) bool { return entry.ID == id })
}

func unknownSubscription(id string) error {
	return fmt.Errorf("subscription %q is not stored", id)
}

// hostOf names a link by its host in errors: the link itself is a credential.
func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "the provider"
	}
	return u.Host
}

// redactLinks replaces every link in text with its host-only form: neither
// errors nor notes carry the link itself.
func redactLinks(text string, links ...string) string {
	for _, link := range links {
		target, err := subscription.Redact(link)
		if err != nil {
			continue
		}
		text = strings.ReplaceAll(text, link, target)
	}
	return text
}

// redactNote fully sanitizes an error note: every stored link and the one just
// attempted (a newly edited link not yet in entries included) is replaced whole
// first, then every key=value pair and bare value those links' queries carry is
// masked — a provider's error body may echo them.
func redactNote(text string, entries []model.Subscription, attemptURL string) string {
	links := make([]string, 0, len(entries)+1)
	for _, entry := range entries {
		links = append(links, entry.URL)
	}
	if attemptURL != "" {
		links = append(links, attemptURL)
	}
	text = redactLinks(text, links...)
	for _, link := range links {
		text = subscription.RedactQuery(text, link)
	}
	return text
}

// redactedError strips the link and its query credentials from an error's text
// before it travels up: the repository redacts what it knows and this catches
// any echo that slips through. The error is returned unchanged when nothing
// needed redacting.
func redactedError(err error, rawURL string) error {
	redacted := redactNote(err.Error(), nil, rawURL)
	if redacted == err.Error() {
		return err
	}
	return errors.New(redacted)
}
