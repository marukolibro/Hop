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

const testProbePort = 17895

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

// TestProbeListenerFollowsTheChosenNode checks the path the unlock checks
// take: the probe listener sends a request through whichever node the probe
// group has picked, whatever the rules say, and neither the probe group nor
// the listener shows up in what the dashboard reads.
func TestProbeListenerFollowsTheChosenNode(t *testing.T) {
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

	probe := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DisableKeepAlives: true,
			Proxy:             http.ProxyURL(&url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", fmt.Sprint(testProbePort))}),
		},
	}
	through := func(node string) string {
		t.Helper()
		if err := client.SelectProxy(ctx, model.ProbeGroup, node); err != nil {
			t.Fatalf("select %s in the probe group: %v", node, err)
		}
		resp, err := probe.Get("http://probe.invalid/")
		if err != nil {
			t.Fatalf("probe through %s: %v", node, err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read the probe reply through %s: %v", node, err)
		}
		return strings.TrimSpace(string(body))
	}
	for _, node := range []string{"node-a", "node-b", "node-a"} {
		if got := through(node); got != node {
			t.Errorf("a probe sent through %s was answered by %q", node, got)
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
	if !slices.Equal(names, []string{"Proxy"}) {
		t.Errorf("groups = %v, want only the profile's own", names)
	}
	if slices.Contains(snapshot.Global.Options, model.ProbeGroup) {
		t.Errorf("the global group offers the probe group: %v", snapshot.Global.Options)
	}
}
