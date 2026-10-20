// Package profile keeps the added subscriptions on disk.
package profile

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"hop/internal/model"
)

const (
	dirPerm  = 0o700
	filePerm = 0o600

	// Every subscription lives in its own directory; the index file is the only
	// one that decides what is stored, the other files only count once an entry
	// references them.
	subsDirName   = "subscriptions"
	indexFileName = "index.json"
	indexVersion  = 1

	// The three files earlier versions kept for their one subscription.
	legacyURLFileName  = "subscription.url"
	legacyRawFileName  = "subscription.yaml"
	legacyMetaFileName = "subscription.meta"

	modeFileName = "mode"
)

// bodyFilePattern is the shape of a body file's name: a 24-hex-digit ID and a
// 12-hex-digit body digest. The sweep only touches files of this shape; the
// index, its temporary twin and anything else are never removed.
var bodyFilePattern = regexp.MustCompile(`^[0-9a-f]{24}-[0-9a-f]{12}\.yaml$`)

// indexFile is the store's committed state: every entry and the one active. It
// is replaced as a whole, so the disk always holds one complete state.
type indexFile struct {
	Version  int         `json:"version"`
	ActiveID string      `json:"activeID"`
	Entries  []entryFile `json:"entries"`
}

// entryFile is one record of the index; Body names the file the raw Clash YAML
// is kept in.
type entryFile struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	UserInfo  string    `json:"userInfo"`
	UpdatedAt time.Time `json:"updatedAt"`
	LastError string    `json:"lastError"`
	Body      string    `json:"body"`
}

// legacyMeta is what the old store kept about a download besides its body and
// link.
type legacyMeta struct {
	UserInfo  string    `json:"userInfo"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Store keeps the added subscriptions under dir. Its files are readable by the
// owner only: the links carry the accounts' tokens.
type Store struct {
	dir string
}

// NewStore returns a store that keeps its files under dir, carrying over the
// subscription saved by earlier versions of Hop.
func NewStore(dir string) (*Store, error) {
	s := &Store{dir: dir}
	if err := s.prepare(); err != nil {
		return nil, err
	}
	return s, nil
}

// NewID returns a fresh identity for a stored subscription. Names may repeat;
// the ID is what Hop addresses a row by. Go 1.27 has no standard library uuid
// yet, so this is 12 crypto/rand bytes in hex.
func NewID() (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate subscription id: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// prepare moves a legacy single subscription into the new format and clears what
// that left behind, orphan bodies included. Running it again is safe: once the
// new format is committed, the legacy files are only leftovers to remove.
func (s *Store) prepare() error {
	index, found, err := s.readIndex()
	if err != nil {
		return err
	}
	if found {
		if err := s.removeLegacy(); err != nil {
			return err
		}
		s.sweep(index)
		return nil
	}
	legacyRaw := filepath.Join(s.dir, legacyRawFileName)
	if _, err := os.Stat(legacyRaw); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("read legacy subscription (path=%q): %w", legacyRaw, err)
	}
	return s.migrateLegacy()
}

// migrateLegacy carries the legacy subscription over as the one active entry:
// the new format is committed before any legacy file is removed, so a crash
// leaves one of the two states complete. Leftovers of an interrupted migration
// are cleared first: without an index, nothing in the subscriptions directory is
// referenced.
func (s *Store) migrateLegacy() error {
	urlPath := filepath.Join(s.dir, legacyURLFileName)
	rawPath := filepath.Join(s.dir, legacyRawFileName)
	link, err := os.ReadFile(urlPath)
	if err != nil {
		return fmt.Errorf("read legacy subscription link (path=%q): %w", urlPath, err)
	}
	raw, err := os.ReadFile(rawPath)
	if err != nil {
		return fmt.Errorf("read legacy subscription (path=%q): %w", rawPath, err)
	}
	meta, err := s.readLegacyMeta()
	if err != nil {
		return err
	}
	storedURL := strings.TrimSpace(string(link))
	u, err := url.Parse(storedURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("legacy subscription link is not a valid URL (path=%q)", urlPath)
	}
	id, err := NewID()
	if err != nil {
		return err
	}
	if err := os.RemoveAll(s.subsDir()); err != nil {
		return fmt.Errorf("clear interrupted migration (path=%q): %w", s.subsDir(), err)
	}
	body := bodyName(id, raw)
	if err := writeAtomic(filepath.Join(s.subsDir(), body), raw); err != nil {
		return err
	}
	index := indexFile{
		Version:  indexVersion,
		ActiveID: id,
		Entries: []entryFile{{
			ID: id, Name: u.Host, URL: storedURL,
			UserInfo: meta.UserInfo, UpdatedAt: meta.UpdatedAt, Body: body,
		}},
	}
	if err := s.writeIndex(index); err != nil {
		return err
	}
	return s.removeLegacy()
}

// readLegacyMeta reads what the old store kept about the download. A
// subscription saved before the store kept it has none: its usage and time are
// unknown.
func (s *Store) readLegacyMeta() (legacyMeta, error) {
	path := filepath.Join(s.dir, legacyMetaFileName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return legacyMeta{}, nil
	}
	if err != nil {
		return legacyMeta{}, fmt.Errorf("read legacy subscription meta (path=%q): %w", path, err)
	}
	var meta legacyMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return legacyMeta{}, fmt.Errorf("decode legacy subscription meta (path=%q): %w", path, err)
	}
	return meta, nil
}

// removeLegacy deletes the files of the old single-subscription store. Hop reads
// the new format only; a file that stays behind is retried on the next start.
func (s *Store) removeLegacy() error {
	for _, name := range []string{legacyURLFileName, legacyRawFileName, legacyMetaFileName} {
		path := filepath.Join(s.dir, name)
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove legacy subscription file (path=%q): %w", path, err)
		}
	}
	return nil
}

// Entries returns the stored subscriptions without their raw bodies, and the
// active subscription's ID ("" when none is stored).
func (s *Store) Entries() ([]model.Subscription, string, error) {
	index, found, err := s.readIndex()
	if err != nil || !found {
		return nil, "", err
	}
	entries := make([]model.Subscription, 0, len(index.Entries))
	for _, file := range index.Entries {
		entries = append(entries, model.Subscription{
			ID: file.ID, Name: file.Name, URL: file.URL,
			UserInfo: file.UserInfo, UpdatedAt: file.UpdatedAt, LastError: file.LastError,
		})
	}
	return entries, index.ActiveID, nil
}

// Get returns the subscription with that ID, its raw body included, and false
// when no subscription has it.
func (s *Store) Get(id string) (model.Subscription, bool, error) {
	index, found, err := s.readIndex()
	if err != nil || !found {
		return model.Subscription{}, false, err
	}
	for _, file := range index.Entries {
		if file.ID != id {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.subsDir(), file.Body))
		if err != nil {
			return model.Subscription{}, false, fmt.Errorf("read subscription %q body (path=%q): %w", id, file.Body, err)
		}
		return model.Subscription{
			ID: file.ID, Name: file.Name, URL: file.URL, Raw: raw,
			UserInfo: file.UserInfo, UpdatedAt: file.UpdatedAt, LastError: file.LastError,
		}, true, nil
	}
	return model.Subscription{}, false, nil
}

// StageBody writes raw as the body the subscription will reference once
// committed. Body files are named by their content, so staging changes nothing
// the store reports until Commit publishes it.
func (s *Store) StageBody(id string, raw []byte) error {
	if err := os.MkdirAll(s.subsDir(), dirPerm); err != nil {
		return fmt.Errorf("create subscriptions dir (path=%q): %w", s.subsDir(), err)
	}
	return writeRaw(s.subsDir(), bodyName(id, raw), raw)
}

// Commit atomically publishes the store's whole state: entries is every entry
// (those with a new body carry it in Raw, unchanged ones leave Raw empty to keep
// their committed body) and activeID the one in use. Bodies nothing references
// any more are swept afterwards.
func (s *Store) Commit(entries []model.Subscription, activeID string) error {
	if err := os.MkdirAll(s.subsDir(), dirPerm); err != nil {
		return fmt.Errorf("create subscriptions dir (path=%q): %w", s.subsDir(), err)
	}
	current, found, err := s.readIndex()
	if err != nil {
		return err
	}
	if !found {
		current = indexFile{}
	}
	committed := make(map[string]string, len(current.Entries))
	for _, file := range current.Entries {
		committed[file.ID] = file.Body
	}

	active := activeID
	if len(entries) == 0 {
		active = ""
	} else if !hasID(entries, activeID) {
		return fmt.Errorf("active subscription %q is not among the %d entries to store", activeID, len(entries))
	}

	index := indexFile{Version: indexVersion, ActiveID: active}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if entry.ID == "" {
			return errors.New("stored subscription needs an id")
		}
		if seen[entry.ID] {
			return fmt.Errorf("subscription %q appears twice in the entries to store", entry.ID)
		}
		seen[entry.ID] = true
		body, kept := committed[entry.ID]
		if len(entry.Raw) > 0 {
			body = bodyName(entry.ID, entry.Raw)
			if err := writeRaw(s.subsDir(), body, entry.Raw); err != nil {
				return err
			}
		} else if !kept {
			return fmt.Errorf("subscription %q has no body to keep", entry.ID)
		}
		index.Entries = append(index.Entries, entryFile{
			ID: entry.ID, Name: entry.Name, URL: entry.URL,
			UserInfo: entry.UserInfo, UpdatedAt: entry.UpdatedAt, LastError: entry.LastError, Body: body,
		})
	}
	if err := s.writeIndex(index); err != nil {
		return err
	}
	// The commit is done; a body the sweep cannot remove only wastes space and
	// is retried when the store is next opened.
	s.sweep(index)
	return nil
}

// SaveMode keeps the outbound mode for the next start.
func (s *Store) SaveMode(mode string) error {
	if err := os.MkdirAll(s.dir, dirPerm); err != nil {
		return fmt.Errorf("create profile dir (path=%q): %w", s.dir, err)
	}
	return writeAtomic(filepath.Join(s.dir, modeFileName), []byte(mode))
}

// LoadMode returns the saved outbound mode, and false when none was saved.
func (s *Store) LoadMode() (string, bool, error) {
	path := filepath.Join(s.dir, modeFileName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read saved mode (path=%q): %w", path, err)
	}
	return string(raw), true, nil
}

// readIndex reads and checks the index. Nothing is papered over: a corrupt
// index, an unknown version, an active ID pointing at a missing entry, or an
// entry whose body is gone are all errors.
func (s *Store) readIndex() (indexFile, bool, error) {
	path := s.indexPath()
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return indexFile{}, false, nil
	}
	if err != nil {
		return indexFile{}, false, fmt.Errorf("read subscription index (path=%q): %w", path, err)
	}
	var index indexFile
	if err := json.Unmarshal(raw, &index); err != nil {
		return indexFile{}, false, fmt.Errorf("decode subscription index (path=%q): %w", path, err)
	}
	if index.Version != indexVersion {
		return indexFile{}, false, fmt.Errorf("subscription index version is %d, hop writes %d (path=%q)", index.Version, indexVersion, path)
	}
	ids := make(map[string]bool, len(index.Entries))
	for _, entry := range index.Entries {
		if entry.ID == "" {
			return indexFile{}, false, fmt.Errorf("subscription index (path=%q) has an entry without an id", path)
		}
		if ids[entry.ID] {
			return indexFile{}, false, fmt.Errorf("subscription index (path=%q) lists %q twice", path, entry.ID)
		}
		ids[entry.ID] = true
		if entry.URL == "" {
			return indexFile{}, false, fmt.Errorf("subscription %q in the index (path=%q) has no link", entry.ID, path)
		}
		body := filepath.Join(s.subsDir(), entry.Body)
		if _, err := os.Stat(body); err != nil {
			return indexFile{}, false, fmt.Errorf("subscription %q body is not readable (path=%q): %w", entry.ID, body, err)
		}
	}
	switch {
	case index.ActiveID == "":
		if len(index.Entries) != 0 {
			return indexFile{}, false, fmt.Errorf("subscription index (path=%q) has %d entries but none in use", path, len(index.Entries))
		}
	case !ids[index.ActiveID]:
		return indexFile{}, false, fmt.Errorf("subscription index (path=%q) points at active subscription %q, which is not stored", path, index.ActiveID)
	}
	return index, true, nil
}

// writeIndex replaces the index atomically: the one step that commits state.
func (s *Store) writeIndex(index indexFile) error {
	raw, err := json.Marshal(index)
	if err != nil {
		return fmt.Errorf("encode subscription index: %w", err)
	}
	return writeAtomic(s.indexPath(), raw)
}

// sweep removes bodies nothing references any more, such as one staged for a
// change that failed. Only files of the body shape are touched; a removal that
// fails is warned about and retried when the store is next opened.
func (s *Store) sweep(index indexFile) {
	referenced := make(map[string]bool, len(index.Entries))
	for _, entry := range index.Entries {
		referenced[entry.Body] = true
	}
	files, err := os.ReadDir(s.subsDir())
	if err != nil {
		slog.Warn("list subscription bodies to sweep", "dir", s.subsDir(), "error", err)
		return
	}
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		name := file.Name()
		if !bodyFilePattern.MatchString(name) && !bodyFilePattern.MatchString(strings.TrimSuffix(name, ".tmp")) {
			continue
		}
		if referenced[name] {
			continue
		}
		if err := os.Remove(filepath.Join(s.subsDir(), name)); err != nil {
			slog.Warn("remove unreferenced subscription body", "file", name, "error", err)
		}
	}
}

func (s *Store) subsDir() string {
	return filepath.Join(s.dir, subsDirName)
}

func (s *Store) indexPath() string {
	return filepath.Join(s.subsDir(), indexFileName)
}

// bodyName is the file a body is kept under: the subscription's ID and the
// body's own digest, so a new body never overwrites a committed one and the same
// body is kept once.
func bodyName(id string, raw []byte) string {
	digest := sha256.Sum256(raw)
	return id + "-" + hex.EncodeToString(digest[:6]) + ".yaml"
}

// writeRaw writes a body, skipping when one with the same name exists: the name
// is the content's digest.
func writeRaw(dir string, name string, raw []byte) error {
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("check subscription body (path=%q): %w", path, err)
	}
	return writeAtomic(path, raw)
}

func hasID(entries []model.Subscription, id string) bool {
	for _, entry := range entries {
		if entry.ID == id {
			return true
		}
	}
	return false
}

// writeAtomic writes through a temporary file, so a crash never leaves half
// a file behind.
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("create dir (path=%q): %w", filepath.Dir(path), err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, filePerm); err != nil {
		return fmt.Errorf("write file (path=%q): %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("move file into place (from=%q to=%q): %w", tmp, path, err)
	}
	return nil
}
