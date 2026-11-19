package integration

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"hop/config"
	"hop/internal/model"
	"hop/internal/repository/mihomo"
	"hop/internal/service"
)

// TestCoreCrashRestart kills the core Hop itself started, by the pid Hop
// recorded for it, and expects the runner to bring a core back by itself.
func TestCoreCrashRestart(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the core of this test is killed the unix way")
	}
	binary := os.Getenv("HOP_MIHOMO_BINARY")
	if binary == "" {
		t.Skip("HOP_MIHOMO_BINARY is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

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
	restarts := make(chan model.CoreRestart, 8)
	runner.OnRestart(func(r model.CoreRestart) { restarts <- r })
	if err := runner.Start(startup); err != nil {
		t.Fatalf("start core: %v", err)
	}
	defer func() {
		stop()
		if err := runner.Stop(); err != nil {
			t.Errorf("stop core: %v", err)
		}
	}()

	// The pid Hop recorded, and none other, is the child this test kills.
	raw, err := os.ReadFile(filepath.Join(appDir, "mihomo.pid"))
	if err != nil {
		t.Fatalf("read the core's pid file: %v", err)
	}
	pid, err := strconv.Atoi(string(raw))
	if err != nil {
		t.Fatalf("the core's pid file does not hold a pid (content=%q): %v", raw, err)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		t.Fatalf("find the core's process: %v", err)
	}
	if err := proc.Kill(); err != nil {
		t.Fatalf("kill the core's process (pid=%d): %v", pid, err)
	}

	// The controller answers again once a core came back.
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := client.Version(ctx); err == nil {
			break
		} else if time.Now().After(deadline) {
			t.Fatalf("the core did not come back within 30s: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}

	select {
	case r := <-restarts:
		if r.GaveUp {
			t.Errorf("restart event = gave up, want a core that came back: %+v", r)
		}
		if r.InARow != 1 {
			t.Errorf("restart event counts %d crashes in a row, want 1", r.InARow)
		}
		if r.Exit == "" {
			t.Error("restart event says nothing about why the core left")
		}
	case <-time.After(10 * time.Second):
		t.Error("the core came back without reporting the restart")
	}

	// What answers is a new core, not the one that was killed.
	raw, err = os.ReadFile(filepath.Join(appDir, "mihomo.pid"))
	if err != nil {
		t.Fatalf("read the core's pid file after the restart: %v", err)
	}
	restarted, err := strconv.Atoi(string(raw))
	if err != nil {
		t.Fatalf("the core's pid file does not hold a pid after the restart (content=%q): %v", raw, err)
	}
	if restarted == pid {
		t.Errorf("the core runs as pid %d, the very one that was killed", pid)
	}
}
