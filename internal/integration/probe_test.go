package integration

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"hop/config"
	"hop/internal/model"
	"hop/internal/repository/mihomo"
	"hop/internal/service"
)

const testProbePort = 17920

// tunnelNode is a node of the "http" type that answers every request with its
// own name, so a test sees which node a request went through.
func tunnelNode(t *testing.T, name string) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for node %s: %v", name, err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				reader := bufio.NewReader(conn)
				// The core's http outbound opens a CONNECT tunnel, then the
				// client's own request travels inside it.
				if _, err := http.ReadRequest(reader); err != nil {
					return
				}
				io.WriteString(conn, "HTTP/1.1 200 Connection established\r\n\r\n")
				if _, err := http.ReadRequest(reader); err != nil {
					return
				}
				fmt.Fprintf(conn, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(name), name)
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

// TestProbeLanesFollowTheChosenGroup checks the path the unlock checks take:
// each probe lane sends a request through whichever group it has picked,
// whatever the rules say, two lanes at once without crossing, and neither the
// lane groups nor the listeners show up in what the dashboard reads.
func TestProbeLanesFollowTheChosenGroup(t *testing.T) {
	binary := os.Getenv("HOP_MIHOMO_BINARY")
	if binary == "" {
		t.Skip("HOP_MIHOMO_BINARY is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	portA, portB := tunnelNode(t, "node-a"), tunnelNode(t, "node-b")
	profile := fmt.Sprintf(`
proxies:
  - {name: "node-a", type: http, server: 127.0.0.1, port: %d}
  - {name: "node-b", type: http, server: 127.0.0.1, port: %d}
proxy-groups:
  - {name: "Proxy", type: select, proxies: ["node-a", "node-b"]}
  - {name: "Line A", type: select, proxies: ["node-a"]}
  - {name: "Line B", type: select, proxies: ["node-b"]}
rules:
  - MATCH,DIRECT
`, portA, portB)

	appDir := t.TempDir()
	cfg := config.Config{
		MihomoBinary: binary, AppDir: appDir, HomeDir: filepath.Join(appDir, "mihomo"),
		MixedPort: testMixedPort, ControllerAddr: testController, Secret: testSecret, ProbePort: testProbePort,
	}
	settings := service.RuntimeSettings{
		MixedPort: testMixedPort, ControllerAddr: testController, ControllerSecret: testSecret,
		BindAddress: "127.0.0.1", LogLevel: "warning", BootstrapGroupName: "Hop", Mode: model.ModeRule,
		ProbePort: testProbePort,
	}
	runtime, err := service.RuntimeConfig([]byte(profile), model.RulesProvider, nil, settings)
	if err != nil {
		t.Fatalf("build config: %v", err)
	}
	client := mihomo.NewClient(testController, testSecret, mihomo.ConfigPath(cfg.HomeDir), &http.Client{}, 5*time.Second)
	runnerCtx, stop := context.WithCancel(ctx)
	runner := mihomo.NewRunner(runnerCtx, cfg, client)
	if err := runner.Start(runtime); err != nil {
		t.Fatalf("start core: %v", err)
	}
	defer func() {
		stop()
		if err := runner.Stop(); err != nil {
			t.Errorf("stop core: %v", err)
		}
	}()

	through := func(lane int, group string) string {
		t.Helper()
		if err := client.SelectProxy(ctx, model.ProbeGroup(lane), group); err != nil {
			t.Fatalf("select %s in probe lane %d: %v", group, lane, err)
		}
		probe := &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				DisableKeepAlives: true,
				Proxy:             http.ProxyURL(&url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", fmt.Sprint(testProbePort+lane))}),
			},
		}
		resp, err := probe.Get("http://probe.invalid/")
		if err != nil {
			t.Fatalf("probe through %s in lane %d: %v", group, lane, err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read the probe reply through %s: %v", group, err)
		}
		return strings.TrimSpace(string(body))
	}
	for _, c := range []struct {
		lane  int
		group string
		want  string
	}{{0, "Line A", "node-a"}, {1, "Line B", "node-b"}, {0, "Line B", "node-b"}, {model.ProbeLanes - 1, "Line A", "node-a"}} {
		if got := through(c.lane, c.group); got != c.want {
			t.Errorf("a probe sent through %s in lane %d was answered by %q, want %q", c.group, c.lane, got, c.want)
		}
	}

	snapshot, err := client.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	names := make([]string, 0, len(snapshot.Groups))
	for _, group := range snapshot.Groups {
		names = append(names, group.Name)
	}
	if !slices.Equal(names, []string{"Proxy", "Line A", "Line B"}) {
		t.Errorf("groups = %v, want only the profile's own", names)
	}
	for _, option := range snapshot.Global.Options {
		if strings.HasPrefix(option, model.ProbeGroupPrefix) {
			t.Errorf("the global group offers the probe group %s", option)
		}
	}
}
