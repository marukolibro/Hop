//go:build darwin

package helper

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

// commandPath matches the absolute command a script line starts with.
var commandPath = regexp.MustCompile(`(?m)^\s*(/(?:usr/)?s?bin/[a-z]+)\s`)

// TestInstallScriptRunsOnThisMac checks what an install hands launchd and
// /bin/sh without running either: the plist must pass plutil, both scripts
// must parse, and every command they call must exist. Each of these broke a
// real install once.
func TestInstallScriptRunsOnThisMac(t *testing.T) {
	dir := t.TempDir()

	plist := filepath.Join(dir, "helper.plist")
	if err := os.WriteFile(plist, []byte(plistXML(501)), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("plutil", "-lint", plist).CombinedOutput(); err != nil {
		t.Fatalf("the LaunchDaemon plist does not lint: %v\n%s", err, out)
	}

	// The helper itself runs these by absolute path, because launchd's PATH
	// is minimal. A missing tool would fail a takeover or a restore.
	for _, command := range []string{"/usr/sbin/networksetup", "/usr/bin/dscacheutil"} {
		if _, err := os.Stat(command); err != nil {
			t.Errorf("the helper runs %s, which this Mac does not have", command)
		}
	}

	scripts := map[string]string{
		"install":   BuildInstallScript("/x/Hop Dev.app/Contents/Resources/bin/hop-helper", "/x/Hop Dev.app/Contents/Resources/bin/mihomo", "/x/Hop Dev.app/Contents/Resources/geodata", 501),
		"uninstall": BuildUninstallScript(),
	}
	for name, script := range scripts {
		path := filepath.Join(dir, name+".sh")
		if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("/bin/sh", "-n", path).CombinedOutput(); err != nil {
			t.Fatalf("the %s script does not parse: %v\n%s", name, err, out)
		}
		for _, match := range commandPath.FindAllStringSubmatch(script, -1) {
			command := match[1]
			if _, err := os.Stat(command); err != nil {
				t.Errorf("the %s script calls %s, which this Mac does not have", name, command)
			}
		}
	}
}
