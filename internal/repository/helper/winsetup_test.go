package helper

import (
	"slices"
	"testing"
)

// TestServiceArgs pins the command line the Windows service is registered
// with: the same paths the macOS daemon gets as flags, and the allowed user's
// SID only when one was given.
func TestServiceArgs(t *testing.T) {
	got := ServiceArgs(`C:\Hop\helper\run\hop-helper.sock`, `C:\Hop\helper\home`, `C:\Hop\helper\bin\mihomo.exe`, "S-1-5-21-1-2-3-1001")
	want := []string{
		"-socket", `C:\Hop\helper\run\hop-helper.sock`,
		"-home", `C:\Hop\helper\home`,
		"-core", `C:\Hop\helper\bin\mihomo.exe`,
		"-allow-sid", "S-1-5-21-1-2-3-1001",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("ServiceArgs = %q, want %q", got, want)
	}
	without := ServiceArgs("sock", "home", "core", "")
	if slices.Contains(without, "-allow-sid") {
		t.Fatalf("ServiceArgs without a SID = %q, want no -allow-sid", without)
	}
}

// TestSocketDirSDDL guards the DACL that replaces macOS's peer check: only
// SYSTEM, Administrators and the installing user may reach the socket, and
// a malformed SID never reaches the ACE string.
func TestSocketDirSDDL(t *testing.T) {
	got, err := SocketDirSDDL("S-1-5-21-1-2-3-1001")
	if err != nil {
		t.Fatal(err)
	}
	want := "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FA;;;S-1-5-21-1-2-3-1001)"
	if got != want {
		t.Fatalf("SocketDirSDDL = %q, want %q", got, want)
	}
	if got, err := SocketDirSDDL(""); err != nil || got != HomeSDDL() {
		t.Fatalf("SocketDirSDDL(\"\") = %q, %v; want the home DACL", got, err)
	}
	if _, err := SocketDirSDDL("S-1-5-21-1);(A;;GA;;;WD"); err == nil {
		t.Fatal("SocketDirSDDL accepted a SID that would inject an ACE")
	}
}

// TestLogDirSDDL keeps the install log readable by the user who approved the
// install, and writable only by SYSTEM and Administrators.
func TestLogDirSDDL(t *testing.T) {
	got, err := LogDirSDDL("S-1-5-21-1-2-3-1001")
	if err != nil {
		t.Fatal(err)
	}
	want := "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OI;FR;;;S-1-5-21-1-2-3-1001)(A;;FRFX;;;S-1-5-21-1-2-3-1001)"
	if got != want {
		t.Fatalf("LogDirSDDL = %q, want %q", got, want)
	}
	if _, err := LogDirSDDL("1001"); err == nil {
		t.Fatal("LogDirSDDL accepted a non-SID")
	}
}

// TestValidSIDText pins the decimal SID form SDDL accepts.
func TestValidSIDText(t *testing.T) {
	cases := []struct {
		sid  string
		want bool
	}{
		{"S-1-5-18", true},
		{"S-1-5-21-1-2-3-1001", true},
		{"S-1-0-0", true},
		{"", false},
		{"S-1-5", false},
		{"S-2-5-18", false},
		{"S-1-5-21-1-2-3-1001)", false},
		{"S-1-5-21-1-2-3-10x1", false},
		{"S-1-5-21--1001", false},
	}
	for _, tc := range cases {
		if got := ValidSIDText(tc.sid); got != tc.want {
			t.Errorf("ValidSIDText(%q) = %v, want %v", tc.sid, got, tc.want)
		}
	}
}

// TestQuoteCommandLine pins the Windows quoting ShellExecuteEx needs: an
// argument with spaces or a quote is quoted, and backslashes before a quote
// or the closing quote are doubled, so the elevated helper reads the paths
// exactly as Hop wrote them.
func TestQuoteCommandLine(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"plain", []string{"install", "-core", `C:\Hop\mihomo.exe`}, `install -core C:\Hop\mihomo.exe`},
		{"space", []string{`C:\Program Files\Hop\hop-helper.exe`}, `"C:\Program Files\Hop\hop-helper.exe"`},
		{"empty", []string{""}, `""`},
		{"trailing backslash", []string{`a b\`}, `"a b\\"`},
		{"quote", []string{`a"b`}, `"a\"b"`},
		{"spaces and quotes", []string{"a b", `c\d`, ""}, `"a b" c\d ""`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := QuoteCommandLine(tc.args); got != tc.want {
				t.Fatalf("QuoteCommandLine(%q) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}
