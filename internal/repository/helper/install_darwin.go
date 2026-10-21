//go:build darwin

package helper

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
)

// Installer installs and removes the helper's LaunchDaemon. Creating it is a
// function call so the service layer can depend on an interface it
// implements.
type Installer struct{}

// InstalledVersion returns the version of the helper answering on the
// production socket. A socket file alone, left by a helper that was killed,
// is not an install: the error says no helper answered.
func (Installer) InstalledVersion(ctx context.Context) (string, error) {
	return NewClient(SocketPath, versionProbeTimeout).Version(ctx)
}

// Install puts the helper in place: the binary from toolSource and the core
// and geodata it runs, root-owned, and the LaunchDaemon that runs it for
// uid. The user approves it with an administrator password, once.
func (Installer) Install(ctx context.Context, toolSource string, coreSource string, geodataDir string, uid int) error {
	script := BuildInstallScript(toolSource, coreSource, geodataDir, uid)
	return runPrivileged(ctx, "install the helper", script)
}

// Uninstall stops the helper and removes everything Install put in place:
// the daemon, the binary, the socket, the home directory and the core in it.
func (Installer) Uninstall(ctx context.Context) error {
	return runPrivileged(ctx, "uninstall the helper", BuildUninstallScript())
}

// runPrivileged runs script as the administrator the user approves being.
// The script travels base64-encoded inside the osascript line: AppleScript
// string literals cannot hold the script's newlines, and encoding them keeps
// the script itself exactly the string BuildInstallScript built, with every
// path in it quoted there. The script is logged before it runs, so what ran
// is on record.
func runPrivileged(ctx context.Context, what string, script string) error {
	slog.Info("running a privileged helper script", "action", what, "script", script)
	encoded := base64.StdEncoding.EncodeToString([]byte(script))
	cmd := exec.CommandContext(ctx, "osascript", "-e",
		fmt.Sprintf("do shell script \"printf '%%s' '%s' | base64 -d | /bin/sh\" with administrator privileges", encoded))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s with administrator privileges (output=%q): %w", what, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// BuildInstallScript returns the /bin/sh script that installs the helper.
// It is a pure function of its inputs so it can be read and reviewed; the
// caller runs it as root. Every path is single-quoted with shQuote, so
// spaces (a "Hop Dev.app" in the way) are safe. It:
//
//  1. stops and forgets any previous helper,
//  2. copies the helper binary to ToolPath, root:wheel 0755,
//  3. writes the LaunchDaemon plist (RunAtLoad, KeepAlive, the production
//     flags and the installing user's uid),
//  4. copies the core in beside the helper's own files and seeds the
//     geodata the core's rules can need,
//  5. loads the daemon.
func BuildInstallScript(toolSource string, coreSource string, geodataDir string, uid int) string {
	var b strings.Builder
	b.WriteString("set -eu\n")
	// A previous helper gets no chance to race the new one.
	fmt.Fprintf(&b, "/bin/launchctl bootout system/%s 2>/dev/null || true\n", Label)
	fmt.Fprintf(&b, "/bin/rm -f %s %s\n", shQuote(PlistPath), shQuote(SocketPath))
	fmt.Fprintf(&b, "/bin/mkdir -p %s %s\n", shQuote(filepath.Dir(ToolPath)), shQuote(HomeDir))
	fmt.Fprintf(&b, "/bin/cp %s %s\n", shQuote(toolSource), shQuote(ToolPath))
	b.WriteString("/usr/sbin/chown root:wheel " + shQuote(ToolPath) + "\n")
	b.WriteString("/bin/chmod 0755 " + shQuote(ToolPath) + "\n")
	// The plist is written as one single-quoted argument of printf, so
	// nothing in it is expanded and no heredoc is needed.
	fmt.Fprintf(&b, "/usr/bin/printf '%%s' %s > %s\n", shQuote(plistXML(uid)), shQuote(PlistPath))
	b.WriteString("/usr/sbin/chown root:wheel " + shQuote(PlistPath) + "\n")
	b.WriteString("/bin/chmod 0644 " + shQuote(PlistPath) + "\n")
	// The core lands beside the helper's own files through a temporary name,
	// so an interrupted copy never leaves a half binary as the core.
	coreTmp := HomeDir + "/mihomo.incoming"
	fmt.Fprintf(&b, "/bin/cp %s %s\n", shQuote(coreSource), shQuote(coreTmp))
	b.WriteString("/usr/sbin/chown root:wheel " + shQuote(coreTmp) + "\n")
	b.WriteString("/bin/chmod 0755 " + shQuote(coreTmp) + "\n")
	fmt.Fprintf(&b, "/bin/mv -f %s %s\n", shQuote(coreTmp), shQuote(CorePath))
	b.WriteString(seedGeodataScript(geodataDir))
	fmt.Fprintf(&b, "/bin/launchctl bootstrap system %s\n", shQuote(PlistPath))
	return b.String()
}

// BuildUninstallScript returns the /bin/sh script that removes the helper:
// its daemon, binary, socket and home, the core included.
func BuildUninstallScript() string {
	var b strings.Builder
	b.WriteString("set -eu\n")
	fmt.Fprintf(&b, "/bin/launchctl bootout system/%s 2>/dev/null || true\n", Label)
	fmt.Fprintf(&b, "/bin/rm -f %s %s %s\n", shQuote(PlistPath), shQuote(ToolPath), shQuote(SocketPath))
	fmt.Fprintf(&b, "/bin/rm -rf %s\n", shQuote(HomeDir))
	return b.String()
}

// seedGeodataScript copies the geodata databases the core's rules can need
// into the helper's home, each through a temporary name. A directory that is
// not there seeds nothing: the core downloads what a config needs itself.
func seedGeodataScript(geodataDir string) string {
	if geodataDir == "" {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "if [ -d %s ]; then\n", shQuote(geodataDir))
	for _, name := range []string{"GeoSite.dat", "geoip.metadb"} {
		src := geodataDir + "/" + name
		dst := HomeDir + "/" + name
		tmp := dst + ".incoming"
		fmt.Fprintf(&b, "  if [ -f %s ]; then\n", shQuote(src))
		fmt.Fprintf(&b, "    /bin/cp %s %s\n", shQuote(src), shQuote(tmp))
		b.WriteString("    /usr/sbin/chown root:wheel " + shQuote(tmp) + "\n")
		b.WriteString("    /bin/chmod 0644 " + shQuote(tmp) + "\n")
		fmt.Fprintf(&b, "    /bin/mv -f %s %s\n", shQuote(tmp), shQuote(dst))
		b.WriteString("  fi\n")
	}
	b.WriteString("fi\n")
	return b.String()
}

// plistXML is the LaunchDaemon's definition: the helper binary under its
// production flags, started at load and kept alive, serving the installing
// user's uid.
func plistXML(uid int) string {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	writePlistKey(&b, "Label", "<string>"+plistEscape(Label)+"</string>")
	b.WriteString("\t<key>ProgramArguments</key>\n\t<array>\n")
	for _, arg := range []string{ToolPath, "-socket", SocketPath, "-home", HomeDir, "-core", CorePath, "-allow-uid", fmt.Sprint(uid), "-manage-dns"} {
		fmt.Fprintf(&b, "\t\t<string>%s</string>\n", plistEscape(arg))
	}
	b.WriteString("\t</array>\n")
	writePlistKey(&b, "RunAtLoad", "<true/>")
	writePlistKey(&b, "KeepAlive", "<true/>")
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

func writePlistKey(b *strings.Builder, key string, value string) {
	fmt.Fprintf(b, "\t<key>%s</key>\n\t%s\n", plistEscape(key), value)
}

// plistEscape escapes the one character XML asks for in text; the paths and
// labels here carry no markup.
func plistEscape(text string) string {
	return strings.ReplaceAll(text, "&", "&amp;")
}

// shQuote wraps text in single quotes for /bin/sh: the only character a
// single-quoted string cannot hold is the quote itself, which is written
// outside the quotes with a backslash.
func shQuote(text string) string {
	return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'"
}
