package helper

import (
	"errors"
	"strconv"
	"strings"
)

// The bits of the Windows helper's setup that are data rather than system
// calls live here, free of build tags, so they run and are tested on any
// platform: the service's argument list, its display strings, the SDDL that
// guards the socket and the install log, and the quoting ShellExecuteEx
// expects for its parameter string.

// ServiceDisplayName and ServiceDescription describe the Windows service in
// the services panel and in Task Manager.
const (
	ServiceDisplayName = "Hop Helper"
	ServiceDescription = "Runs Hop's mihomo core with system privileges in the background while TUN is on."
)

// ServiceArgs is the command line the service runs the helper with. It is
// the same set of paths the macOS LaunchDaemon passes as flags; Windows has
// no manage-dns flag because sing-tun gives the Wintun adapter its own DNS
// server and metric, so the helper never touches the system DNS there.
func ServiceArgs(socket string, home string, core string, allowSID string) []string {
	args := []string{"-socket", socket, "-home", home, "-core", core}
	if allowSID != "" {
		args = append(args, "-allow-sid", allowSID)
	}
	return args
}

// HomeSDDL is the security descriptor of the helper's home: SYSTEM and
// Administrators only. The core's config carries the subscription's nodes
// and the controller secret.
func HomeSDDL() string {
	return "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"
}

// SocketDirSDDL is the security descriptor of the directory holding the
// helper's AF_UNIX socket: SYSTEM, Administrators, and the installing user,
// nobody else. Windows enforces this DACL when a process connects to the
// socket (Microsoft's AF_UNIX documentation: a connecting process needs
// write permission on the socket), which is what replaces the peer
// credential check macOS makes. An empty userSID leaves the directory to
// SYSTEM and Administrators.
func SocketDirSDDL(userSID string) (string, error) {
	if userSID == "" {
		return "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)", nil
	}
	if !ValidSIDText(userSID) {
		return "", errors.New("the allowed user's SID is not a SID: " + strconv.Quote(userSID))
	}
	return "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FA;;;" + userSID + ")", nil
}

// LogDirSDDL is the security descriptor of the directory holding the
// elevated installer's log: SYSTEM and Administrators may write it, the
// installing user may read it, so Hop can report why an install failed.
func LogDirSDDL(userSID string) (string, error) {
	if userSID == "" {
		return "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)", nil
	}
	if !ValidSIDText(userSID) {
		return "", errors.New("the allowed user's SID is not a SID: " + strconv.Quote(userSID))
	}
	return "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OI;FR;;;" + userSID + ")(A;;FRFX;;;" + userSID + ")", nil
}

// ValidSIDText says whether s is a SID in the decimal form SDDL accepts:
// S-1-<authority>-<sub authority>..., digits only. It exists so a value
// passed to the elevated installer can never break out of its ACE string.
func ValidSIDText(s string) bool {
	parts := strings.Split(s, "-")
	if len(parts) < 4 || parts[0] != "S" || parts[1] != "1" {
		return false
	}
	for _, part := range parts[2:] {
		if part == "" {
			return false
		}
		if _, err := strconv.ParseUint(part, 10, 64); err != nil {
			return false
		}
	}
	return true
}

// QuoteCommandLine joins args the way CommandLineToArgvW will split them
// again, for ShellExecuteEx's single parameter string: an argument with
// whitespace or a quote is quoted, and backslashes before a quote (or before
// the closing quote) are doubled.
func QuoteCommandLine(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = quoteArg(arg)
	}
	return strings.Join(quoted, " ")
}

// quoteArg quotes one argument of a Windows command line.
func quoteArg(arg string) string {
	if arg != "" && !strings.ContainsAny(arg, " \t\n\v\"") {
		return arg
	}
	var b strings.Builder
	b.WriteByte('"')
	backslashes := 0
	for _, r := range arg {
		switch r {
		case '\\':
			backslashes++
			continue
		case '"':
			b.WriteString(strings.Repeat(`\`, backslashes*2+1))
			b.WriteByte('"')
			backslashes = 0
		default:
			b.WriteString(strings.Repeat(`\`, backslashes))
			backslashes = 0
			b.WriteRune(r)
		}
	}
	b.WriteString(strings.Repeat(`\`, backslashes*2))
	b.WriteByte('"')
	return b.String()
}
