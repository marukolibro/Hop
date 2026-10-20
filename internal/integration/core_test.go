package integration

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"hop/config"
	"hop/internal/model"
	"hop/internal/repository/corerelease"
	"hop/internal/repository/corestore"
	"hop/internal/repository/mihomo"
	"hop/internal/service"
)

// olderVersion stands in for the running core's version, so the real core
// can be "upgraded" to the very release the test serves.
type olderVersion struct{}

func (olderVersion) Version(context.Context) (string, error) { return "v1.0.0", nil }

// releaseServer serves one release the way GitHub does: the newest-release
// document, and the asset it points to.
type releaseServer struct {
	server *httptest.Server
	asset  []byte
	digest string
}

func newReleaseServer(t *testing.T, tag string, assetName string) *releaseServer {
	t.Helper()
	r := &releaseServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, req *http.Request) {
		doc := map[string]any{
			"tag_name": tag,
			"assets": []map[string]any{{
				"name": assetName, "size": len(r.asset), "digest": "sha256:" + r.digest,
				"browser_download_url": r.server.URL + "/asset",
			}},
		}
		if err := json.NewEncoder(w).Encode(doc); err != nil {
			t.Errorf("write release document: %v", err)
		}
	})
	mux.HandleFunc("/asset", func(w http.ResponseWriter, req *http.Request) {
		if _, err := w.Write(r.asset); err != nil {
			t.Errorf("write asset: %v", err)
		}
	})
	r.server = httptest.NewServer(mux)
	t.Cleanup(r.server.Close)
	return r
}

// serve makes the release's asset content, with the digest GitHub would
// publish for it.
func (r *releaseServer) serve(content []byte) {
	r.asset = content
	sum := sha256.Sum256(content)
	r.digest = hex.EncodeToString(sum[:])
}

func gzipBytes(t *testing.T, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(content); err != nil {
		t.Fatalf("gzip: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

func TestCoreUpgrade(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("the release asset of this test is the darwin/arm64 one")
	}
	binary := os.Getenv("HOP_MIHOMO_BINARY")
	if binary == "" {
		t.Skip("HOP_MIHOMO_BINARY is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// The release is the core under test itself, so the upgraded core is
	// real, runs, and reports the tag it was served under.
	tag, err := mihomo.BinaryVersion(ctx, binary)
	if err != nil {
		t.Fatalf("version of the core under test: %v", err)
	}
	coreBytes, err := os.ReadFile(binary)
	if err != nil {
		t.Fatalf("read the core under test: %v", err)
	}
	releases := newReleaseServer(t, tag, "mihomo-darwin-arm64-"+tag+".gz")

	appDir := t.TempDir()
	cfg := config.Config{
		MihomoBinary: binary, BundledBinary: binary, CoreOrigin: model.CoreBundled,
		AppDir: appDir, HomeDir: filepath.Join(appDir, "mihomo"),
		MixedPort: testMixedPort, ControllerAddr: testController, Secret: testSecret,
	}
	client := mihomo.NewClient(testController, testSecret, mihomo.ConfigPath(cfg.HomeDir), &http.Client{}, 5*time.Second)
	settings := service.RuntimeSettings{
		MixedPort: testMixedPort, ControllerAddr: testController, ControllerSecret: testSecret,
		BindAddress: "127.0.0.1", LogLevel: "warning", BootstrapGroupName: "Hop", Mode: model.ModeRule,
	}
	startup, err := service.BootstrapConfig(settings)
	if err != nil {
		t.Fatalf("bootstrap config: %v", err)
	}
	runnerCtx, stop := context.WithCancel(ctx)
	runner := mihomo.NewRunner(runnerCtx, cfg, client)
	if err := runner.Start(startup); err != nil {
		t.Fatalf("start core: %v", err)
	}
	defer func() {
		stop()
		if err := runner.Stop(); err != nil {
			t.Errorf("stop core: %v", err)
		}
	}()

	store := corestore.New(appDir)
	core := service.NewCoreService(
		olderVersion{},
		corerelease.NewSource(&http.Client{}, releases.server.URL+"/latest"),
		store, runner,
		mihomo.NewCoreChecker(mihomo.ConfigPath(cfg.HomeDir), filepath.Join(appDir, "validate"), validateTimeout),
		binary, binary, model.CoreBundled,
	)
	noProgress := func(model.CoreProgress) {}

	assertRunsFrom := func(wantBinary string) {
		t.Helper()
		if got := runner.Binary(); got != wantBinary {
			t.Errorf("core runs from %q, want %q", got, wantBinary)
		}
		if _, err := client.Version(ctx); err != nil {
			t.Errorf("controller does not answer: %v", err)
		}
	}
	assertNothingInstalled := func() {
		t.Helper()
		if _, _, found, err := store.Current(); err != nil || found {
			t.Errorf("a core is recorded as installed (found=%v err=%v)", found, err)
		}
		if entries, _ := os.ReadDir(filepath.Join(appDir, "core")); len(entries) != 0 {
			t.Errorf("downloaded files were left behind: %v", entries)
		}
	}

	t.Run("a download that does not match its digest is refused", func(t *testing.T) {
		releases.serve(gzipBytes(t, coreBytes))
		releases.digest = strings.Repeat("0", 64)
		err := core.Upgrade(ctx, noProgress)
		if err == nil || !strings.Contains(err.Error(), "sha256") {
			t.Fatalf("want a sha256 error, got %v", err)
		}
		assertRunsFrom(binary)
		assertNothingInstalled()
	})

	t.Run("a download that is no mihomo is refused before the core restarts", func(t *testing.T) {
		releases.serve(gzipBytes(t, []byte("#!/bin/sh\necho not mihomo\n")))
		err := core.Upgrade(ctx, noProgress)
		if err == nil || !strings.Contains(err.Error(), "version") {
			t.Fatalf("want a version error, got %v", err)
		}
		assertRunsFrom(binary)
		assertNothingInstalled()
	})

	t.Run("upgrading installs the core and restarts on it", func(t *testing.T) {
		releases.serve(gzipBytes(t, coreBytes))
		var stages []model.CoreStage
		err := core.Upgrade(ctx, func(p model.CoreProgress) {
			if len(stages) == 0 || stages[len(stages)-1] != p.Stage {
				stages = append(stages, p.Stage)
			}
		})
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		want := []model.CoreStage{model.CoreStageDownload, model.CoreStageVerify, model.CoreStageRestart}
		if len(stages) != len(want) || stages[0] != want[0] || stages[1] != want[1] || stages[2] != want[2] {
			t.Errorf("stages = %v, want %v", stages, want)
		}
		installed, err := store.BinaryPath(tag)
		if err != nil {
			t.Fatalf("installed path: %v", err)
		}
		assertRunsFrom(installed)
		if gotTag, _, found, err := store.Current(); err != nil || !found || gotTag != tag {
			t.Errorf("Current() = %q found=%v err=%v, want %q", gotTag, found, err, tag)
		}
		info, err := core.Info(ctx)
		if err != nil || info.Origin != model.CoreInstalled {
			t.Errorf("Info() = %+v err=%v, want origin installed", info, err)
		}
		if err := core.Upgrade(ctx, noProgress); err != nil {
			// The core service still sees the stand-in's older version, so
			// asking again installs again; the point is it must not break.
			t.Fatalf("a second upgrade failed: %v", err)
		}
	})

	t.Run("going back runs the bundled core and forgets the installed one", func(t *testing.T) {
		if err := core.RestoreBundled(ctx); err != nil {
			t.Fatalf("restore: %v", err)
		}
		assertRunsFrom(binary)
		assertNothingInstalled()
		if err := core.RestoreBundled(ctx); err == nil {
			t.Error("restoring when the bundled core already runs should say so")
		}
	})

	t.Run("a developer's core cannot be upgraded", func(t *testing.T) {
		override := service.NewCoreService(olderVersion{}, nil, nil, nil, nil, binary, binary, model.CoreOverride)
		if err := override.Upgrade(ctx, noProgress); !errors.Is(err, service.ErrCoreNotUpgradable) {
			t.Errorf("err = %v, want ErrCoreNotUpgradable", err)
		}
	})
}
