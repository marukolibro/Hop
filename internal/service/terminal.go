package service

// TerminalProxyCommand is what makes a terminal session send its traffic
// through Hop at address (host:port): the proxy variables command line
// tools read, in the shell of goos — PowerShell on Windows, a POSIX shell
// elsewhere.
func TerminalProxyCommand(goos string, address string) string {
	httpProxy, socksProxy := "http://"+address, "socks5://"+address
	if goos == "windows" {
		return `$env:HTTPS_PROXY="` + httpProxy + `"; $env:HTTP_PROXY="` + httpProxy + `"; $env:ALL_PROXY="` + socksProxy + `"`
	}
	return "export https_proxy=" + httpProxy + " http_proxy=" + httpProxy + " all_proxy=" + socksProxy
}
