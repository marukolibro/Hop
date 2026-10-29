package integration

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hop/config"
	"hop/internal/model"
	"hop/internal/repository/mihomo"
	"hop/internal/service"
)

// holdingNode is a node of the "http" type that opens every tunnel asked of
// it and keeps it open until the other end closes it.
func holdingNode(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for a node: %v", err)
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
				if _, err := http.ReadRequest(reader); err != nil {
					return
				}
				io.WriteString(conn, "HTTP/1.1 200 Connection established\r\n\r\n")
				_, _ = io.Copy(io.Discard, reader)
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

// TestSwitchClosesConnectionsLeftBehind checks that switching a group's node
// closes the connections still on the old node, and that choosing the node
// a group already uses closes nothing.
func TestSwitchClosesConnectionsLeftBehind(t *testing.T) {
	binary := os.Getenv("HOP_MIHOMO_BINARY")
	if binary == "" {
		t.Skip("HOP_MIHOMO_BINARY is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	profile := fmt.Sprintf(`
proxies:
  - {name: "node-a", type: http, server: 127.0.0.1, port: %d}
  - {name: "node-b", type: http, server: 127.0.0.1, port: %d}
proxy-groups:
  - {name: "Proxy", type: select, proxies: ["node-a", "node-b"]}
rules:
  - MATCH,Proxy
`, holdingNode(t), holdingNode(t))

	appDir := t.TempDir()
	cfg := config.Config{
		MihomoBinary: binary, AppDir: appDir, HomeDir: filepath.Join(appDir, "mihomo"),
		MixedPort: testMixedPort, ControllerAddr: testController, Secret: testSecret,
	}
	settings := service.RuntimeSettings{
		MixedPort: testMixedPort, ControllerAddr: testController, ControllerSecret: testSecret,
		BindAddress: "127.0.0.1", LogLevel: "warning", BootstrapGroupName: "Hop", Mode: model.ModeRule,
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
	proxies := service.NewProxyService(client)
	if err := proxies.Select(ctx, "Proxy", "node-a"); err != nil {
		t.Fatalf("select node-a: %v", err)
	}

	// A tunnel through the mixed port, which the rules send through Proxy.
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", testMixedPort))
	if err != nil {
		t.Fatalf("dial the mixed port: %v", err)
	}
	defer conn.Close()
	fmt.Fprint(conn, "CONNECT site.invalid:443 HTTP/1.1\r\nHost: site.invalid:443\r\n\r\n")
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatalf("read the CONNECT reply: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT answered %s, want 200", resp.Status)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		conns, err := client.Connections(ctx)
		if err != nil {
			t.Fatalf("list connections: %v", err)
		}
		if len(service.LeftBehind(conns, "Proxy", "node-b")) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the tunnel never showed up on node-a: %+v", conns.Active)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// open reports whether the tunnel is still open after a moment.
	open := func() bool {
		t.Helper()
		if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatalf("set read deadline: %v", err)
		}
		_, err := reader.ReadByte()
		var netErr net.Error
		return errors.As(err, &netErr) && netErr.Timeout()
	}
	if err := proxies.Select(ctx, "Proxy", "node-a"); err != nil {
		t.Fatalf("select node-a again: %v", err)
	}
	if !open() {
		t.Fatal("choosing the node the group already uses closed its connection")
	}
	if err := proxies.Select(ctx, "Proxy", "node-b"); err != nil {
		t.Fatalf("select node-b: %v", err)
	}
	if open() {
		t.Fatal("switching to node-b left the connection open on node-a")
	}
}
