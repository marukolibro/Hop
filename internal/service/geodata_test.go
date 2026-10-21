package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	geodatarepo "hop/internal/repository/geodata"
)

// geodataTestValidator stands in for the core's config test and records what
// it was asked to check.
type geodataTestValidator struct {
	err   error
	calls int
	dir   string
}

func (v *geodataTestValidator) ValidateWithGeodata(_ context.Context, _ []byte, dir string) error {
	v.calls++
	v.dir = dir
	return v.err
}

// geodataTestConfigs hands the service a config to check.
type geodataTestConfigs struct{ config []byte }

func (c geodataTestConfigs) CurrentConfig(context.Context) ([]byte, error) {
	return c.config, nil
}

// geodataTestStore keeps the recorded success in memory.
type geodataTestStore struct {
	at    time.Time
	found bool
}

func (s *geodataTestStore) LoadGeodataUpdatedAt() (time.Time, bool, error) {
	return s.at, s.found, nil
}

func (s *geodataTestStore) SaveGeodataUpdatedAt(at time.Time) error {
	s.at, s.found = at, true
	return nil
}

// geodataTestHelper records what the helper was asked to install.
type geodataTestHelper struct {
	dirs []string
	err  error
}

func (h *geodataTestHelper) InstallGeodata(_ context.Context, dir string) error {
	h.dirs = append(h.dirs, dir)
	return h.err
}

// geodataTestCore records restarts of Hop's own core.
type geodataTestCore struct {
	restarts int
	err      error
}

func (c *geodataTestCore) RestartCurrent(context.Context) error {
	c.restarts++
	return c.err
}

// geodataTestLock records that the replacement took the runtime lock.
type geodataTestLock struct{ locks int }

func (l *geodataTestLock) LockRuntime() { l.locks++ }
func (l *geodataTestLock) UnlockRuntime() {
}

// TestGeodataReplacement is the replace path: a download whose checksums
// match but whose files the core rejects must leave every old database in
// place and record no success. A good download replaces all three copies and
// restarts the core that reads them; under TUN the helper does the copying
// and restarting instead.
func TestGeodataReplacement(t *testing.T) {
	good := map[string][]byte{
		"geosite.dat":  []byte("new-site"),
		"geoip.metadb": []byte("new-ip"),
	}
	for _, tc := range []struct {
		name        string
		validator   *geodataTestValidator
		checksumBad bool
		wantErr     bool
		wantRestart int
		wantHelper  bool
		wantSaved   bool
	}{
		{
			name:        "good files replace every copy",
			validator:   &geodataTestValidator{},
			wantRestart: 1,
			wantSaved:   true,
		},
		{
			name:      "core rejects the files",
			validator: &geodataTestValidator{err: errors.New("bad geosite")},
			wantErr:   true,
		},
		{
			name:        "checksum mismatch",
			validator:   &geodataTestValidator{},
			checksumBad: true,
			wantErr:     true,
		},
		{
			name:       "tun on leaves the copy and restart to the helper",
			validator:  &geodataTestValidator{},
			wantHelper: true,
			wantSaved:  true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			appDir := filepath.Join(root, "geodata")
			homeDir := filepath.Join(root, "mihomo")
			validateDir := filepath.Join(root, "validate")
			for _, dir := range []string{appDir, homeDir, validateDir} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			oldSite, oldIP := []byte("old-site"), []byte("old-ip")
			for _, dir := range []string{appDir, homeDir, validateDir} {
				if err := os.WriteFile(filepath.Join(dir, "GeoSite.dat"), oldSite, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "geoip.metadb"), oldIP, 0o600); err != nil {
					t.Fatal(err)
				}
			}

			server := newGeodataServer(t, good, tc.checksumBad)
			t.Cleanup(server.Close)
			store := &geodataTestStore{}
			helper := &geodataTestHelper{}
			core := &geodataTestCore{}
			lock := &geodataTestLock{}
			tun := NewTunFlag()
			tun.Set(tc.wantHelper)
			svc := NewGeodataService(
				geodatarepo.NewSource(server.Client(), server.URL+"/"),
				tc.validator,
				geodataTestConfigs{config: []byte("mixed-port: 17890\n")},
				store, helper, core, tun, lock,
				appDir, homeDir, validateDir,
			)

			err := svc.Update(context.Background())
			if tc.wantErr {
				if err == nil {
					t.Fatal("Update accepted files it should have refused")
				}
				for _, dir := range []string{appDir, homeDir, validateDir} {
					if got := readTestFile(t, filepath.Join(dir, "GeoSite.dat")); string(got) != string(oldSite) {
						t.Fatalf("%s was replaced by a refused download: %q", dir, got)
					}
					if got := readTestFile(t, filepath.Join(dir, "geoip.metadb")); string(got) != string(oldIP) {
						t.Fatalf("%s was replaced by a refused download: %q", dir, got)
					}
				}
				if store.found {
					t.Fatal("a refused download recorded a successful update")
				}
				if core.restarts != 0 || len(helper.dirs) != 0 {
					t.Fatalf("a refused download restarted or copied: core=%d helper=%v", core.restarts, helper.dirs)
				}
				return
			}
			if err != nil {
				t.Fatalf("Update: %v", err)
			}
			if tc.validator.calls != 1 {
				t.Fatalf("validator calls = %d, want 1", tc.validator.calls)
			}
			if !strings.HasPrefix(tc.validator.dir, appDir) {
				t.Fatalf("validator checked in %q, want a staging dir under %q", tc.validator.dir, appDir)
			}
			for _, dir := range []string{appDir, homeDir, validateDir} {
				if got := readTestFile(t, filepath.Join(dir, "GeoSite.dat")); string(got) != string(good["geosite.dat"]) {
					t.Fatalf("%s was not replaced: %q", dir, got)
				}
				if got := readTestFile(t, filepath.Join(dir, "geoip.metadb")); string(got) != string(good["geoip.metadb"]) {
					t.Fatalf("%s was not replaced: %q", dir, got)
				}
			}
			if core.restarts != tc.wantRestart {
				t.Fatalf("core restarts = %d, want %d", core.restarts, tc.wantRestart)
			}
			if lock.locks != 1 {
				t.Fatalf("runtime locks = %d, want 1", lock.locks)
			}
			if tc.wantHelper {
				if len(helper.dirs) != 1 || helper.dirs[0] != appDir {
					t.Fatalf("helper got %v, want [%s]", helper.dirs, appDir)
				}
			} else if len(helper.dirs) != 0 {
				t.Fatalf("helper got %v with TUN off", helper.dirs)
			}
			if store.found != tc.wantSaved {
				t.Fatalf("store found = %v, want %v", store.found, tc.wantSaved)
			}
		})
	}

	t.Run("old success is not due for a week", func(t *testing.T) {
		root := t.TempDir()
		server := newGeodataServer(t, good, false)
		t.Cleanup(server.Close)
		store := &geodataTestStore{at: time.Now().Add(-8 * 24 * time.Hour), found: true}
		svc := NewGeodataService(
			geodatarepo.NewSource(server.Client(), server.URL+"/"),
			&geodataTestValidator{},
			geodataTestConfigs{config: []byte("mixed-port: 17890\n")},
			store, &geodataTestHelper{}, &geodataTestCore{}, NewTunFlag(), &geodataTestLock{},
			filepath.Join(root, "geodata"), filepath.Join(root, "mihomo"), filepath.Join(root, "validate"),
		)
		if changed, err := svc.AutoUpdate(context.Background()); err != nil || !changed {
			t.Fatalf("AutoUpdate with an eight-day-old success = %v, %v; want an update", changed, err)
		}
		if changed, err := svc.AutoUpdate(context.Background()); err != nil || changed {
			t.Fatalf("AutoUpdate right after a success = %v, %v; want no update", changed, err)
		}
	})
}

// newGeodataServer serves the two databases and their checksums. When
// checksumBad is set the checksum describes other bytes, which the downloader
// must refuse.
func newGeodataServer(t *testing.T, files map[string][]byte, checksumBad bool) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if fileName, isSum := strings.CutSuffix(name, ".sha256sum"); isSum {
			data, known := files[fileName]
			if !known {
				http.NotFound(w, r)
				return
			}
			if checksumBad {
				data = []byte("different bytes")
			}
			digest := sha256.Sum256(data)
			_, _ = w.Write([]byte(hex.EncodeToString(digest[:]) + "  " + fileName + "\n"))
			return
		}
		data, known := files[name]
		if !known {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	return server
}

func readTestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
