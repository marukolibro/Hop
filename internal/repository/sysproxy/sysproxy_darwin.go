//go:build darwin

package sysproxy

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"hop/internal/model"
	"hop/internal/repository/netservice"
)

// Current reads the proxy macOS applies now, that of the primary network
// service, as `scutil --proxy` reports it.
func (c *Controller) Current(ctx context.Context) (model.SystemProxyState, error) {
	out, err := exec.CommandContext(ctx, "scutil", "--proxy").Output()
	if err != nil {
		return model.SystemProxyState{}, fmt.Errorf("read the system proxy (scutil --proxy): %w", err)
	}
	return parseScutil(string(out)), nil
}

// parseScutil reads the proxy dictionary `scutil --proxy` prints, lines of
// "Key : Value".
func parseScutil(out string) model.SystemProxyState {
	values := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), " : ")
		if ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	endpoint := func(prefix string) model.ProxyEndpoint {
		// A proxy without a port reads as port 0, which is no one's.
		port, _ := strconv.Atoi(values[prefix+"Port"])
		return model.ProxyEndpoint{Enabled: values[prefix+"Enable"] == "1", Host: values[prefix+"Proxy"], Port: port}
	}
	return model.SystemProxyState{
		HTTP:       endpoint("HTTP"),
		HTTPS:      endpoint("HTTPS"),
		SOCKS:      endpoint("SOCKS"),
		AutoConfig: values["ProxyAutoConfigEnable"] == "1",
	}
}

// Set points the web, secure web and SOCKS proxies of every enabled network
// service at host:port, so apps use it whichever network is in use.
func (c *Controller) Set(ctx context.Context, host string, port int) error {
	services, err := netservice.List(ctx)
	if err != nil {
		return err
	}
	p := strconv.Itoa(port)
	for _, service := range services {
		for _, args := range [][]string{
			{"-setwebproxy", service, host, p},
			{"-setsecurewebproxy", service, host, p},
			{"-setsocksfirewallproxy", service, host, p},
		} {
			if err := netservice.Run(ctx, args); err != nil {
				return err
			}
		}
	}
	return nil
}

// Clear turns the web, secure web and SOCKS proxies of every enabled network
// service off.
func (c *Controller) Clear(ctx context.Context) error {
	services, err := netservice.List(ctx)
	if err != nil {
		return err
	}
	for _, service := range services {
		for _, args := range [][]string{
			{"-setwebproxystate", service, "off"},
			{"-setsecurewebproxystate", service, "off"},
			{"-setsocksfirewallproxystate", service, "off"},
		} {
			if err := netservice.Run(ctx, args); err != nil {
				return err
			}
		}
	}
	return nil
}
