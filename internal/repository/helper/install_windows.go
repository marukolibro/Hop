//go:build windows

package helper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Installer installs and removes the helper's Windows service. Installing
// is the user's action: it runs the bundled helper elevated through UAC, and
// that process copies itself and the core in, registers the service and
// starts it. Nothing here runs elevated itself.
type Installer struct{}

// InstalledVersion returns the version of the helper answering on the
// production socket. A socket file alone, left by a helper that was killed,
// is not an install: the error says no helper answered.
func (Installer) InstalledVersion(ctx context.Context) (string, error) {
	return NewClient(SocketPath, versionProbeTimeout).Version(ctx)
}

// Install runs the bundled helper elevated: it copies itself and the core
// into InstallDir, guards the socket directory with an ACL, registers the
// service and starts it. uid is macOS's installing user; Windows passes the
// user's SID instead, which the elevated process never derives from its own
// token (an administrator's credentials entered at the UAC prompt would
// otherwise let the wrong account talk to the helper).
func (Installer) Install(ctx context.Context, toolSource string, coreSource string, geodataDir string, uid int) error {
	if toolSource == "" {
		return errors.New("the app ships no hop-helper; TUN cannot be installed (build it with scripts/build-helper.sh)")
	}
	sid, err := CurrentUserSID()
	if err != nil {
		return err
	}
	args := []string{"install", "-core", coreSource, "-geodata", geodataDir, "-sid", sid}
	return runElevated(ctx, "install the helper", toolSource, args, InstallLogPath())
}

// Uninstall runs a copy of the installed helper elevated from the temp
// directory, so that process can stop the service and remove the install
// directory (a running executable cannot delete itself). The copy exits when
// the service is gone and the files are removed.
func (Installer) Uninstall(ctx context.Context) error {
	if _, err := os.Stat(ToolPath); err != nil {
		return fmt.Errorf("the installed helper is not there (path=%q): %w", ToolPath, err)
	}
	copied, err := copyToTemp(ToolPath)
	if err != nil {
		return err
	}
	defer os.Remove(copied)
	sid, err := CurrentUserSID()
	if err != nil {
		return err
	}
	if err := runElevated(ctx, "uninstall the helper", copied, []string{"uninstall", "-sid", sid}, InstallLogPath()); err != nil {
		return err
	}
	// The elevated process removes the files after the service stopped; wait
	// for its work to land so the dashboard never shows a removed helper as
	// still installed.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ToolPath); errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the helper's files are still there after the elevated uninstall (path=%q)", ToolPath)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// copyToTemp copies src into a fresh executable file in the temp directory.
func copyToTemp(src string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", fmt.Errorf("open the installed helper (path=%q): %w", src, err)
	}
	defer in.Close()
	tmp, err := os.CreateTemp("", "hop-helper-uninstall-*.exe")
	if err != nil {
		return "", fmt.Errorf("stage the uninstaller: %w", err)
	}
	name := tmp.Name()
	_, copyErr := io.Copy(tmp, in)
	closeErr := tmp.Close()
	if copyErr != nil || closeErr != nil {
		os.Remove(name)
		return "", fmt.Errorf("stage the uninstaller (path=%q): %w", name, errors.Join(copyErr, closeErr))
	}
	return name, nil
}

// CurrentUserSID is the SID of the user running Hop, the one the helper's
// socket admits.
func CurrentUserSID() (string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("read the current user's SID: %w", err)
	}
	sid := user.User.Sid.String()
	if !ValidSIDText(sid) {
		return "", fmt.Errorf("the current user's SID is not a SID (%q)", sid)
	}
	return sid, nil
}

// Windows flags for ShellExecuteEx.
const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskNoAsync        = 0x00000100
	seeMaskFlagNoUI       = 0x00000400
	coinitApartmentThread = 0x2
)

// shellExecuteInfo is SHELLEXECUTEINFOW. Its layout is the one the SDK
// defines for 64-bit Windows, where Hop builds.
type shellExecuteInfo struct {
	cbSize         uint32
	fMask          uint32
	hwnd           windows.Handle
	lpVerb         *uint16
	lpFile         *uint16
	lpParameters   *uint16
	lpDirectory    *uint16
	nShow          int32
	hInstApp       windows.Handle
	lpIDList       unsafe.Pointer
	lpClass        *uint16
	hkeyClass      windows.Handle
	dwHotKey       uint32
	hIconOrMonitor windows.Handle
	hProcess       windows.Handle
}

var procShellExecuteExW = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

// runElevated starts exe with the arguments through the UAC "runas" verb,
// waits for it to exit, and returns an error with the elevated process's
// exit code and the tail of its log when it failed. The user approving the
// UAC dialog is the only elevation this ever asks for; nothing is started
// silently.
func runElevated(ctx context.Context, action string, exe string, args []string, logPath string) error {
	// COM is per-thread, and Go may move a goroutine between calls: pin this
	// one so the initialization and the ShellExecuteEx call share a thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// ShellExecuteEx may use shell extensions, which want COM initialized.
	// S_FALSE (already initialized on this thread) is not a failure here.
	_ = windows.CoInitializeEx(0, coinitApartmentThread)
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return fmt.Errorf("prepare the elevation verb: %w", err)
	}
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return fmt.Errorf("prepare the elevated program (path=%q): %w", exe, err)
	}
	params, err := windows.UTF16PtrFromString(QuoteCommandLine(args))
	if err != nil {
		return fmt.Errorf("prepare the elevated arguments: %w", err)
	}
	info := shellExecuteInfo{
		fMask:        seeMaskNoCloseProcess | seeMaskNoAsync | seeMaskFlagNoUI,
		lpVerb:       verb,
		lpFile:       file,
		lpParameters: params,
		nShow:        windows.SW_HIDE,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))
	ok, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		if errors.Is(callErr, windows.ERROR_CANCELLED) {
			return fmt.Errorf("%s: the elevation prompt was cancelled", action)
		}
		return fmt.Errorf("%s: start %q elevated: %w", action, exe, callErr)
	}
	if info.hProcess == 0 {
		return fmt.Errorf("%s: the elevated helper returned no process handle", action)
	}
	defer windows.CloseHandle(info.hProcess)

	for {
		event, err := windows.WaitForSingleObject(info.hProcess, 200)
		if err != nil {
			return fmt.Errorf("%s: wait for the elevated helper: %w", action, err)
		}
		if event == windows.WAIT_OBJECT_0 {
			break
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%s: wait for the elevated helper: %w", action, err)
		}
	}
	var code uint32
	if err := windows.GetExitCodeProcess(info.hProcess, &code); err != nil {
		return fmt.Errorf("%s: read the elevated helper's exit code: %w", action, err)
	}
	if code != 0 {
		return fmt.Errorf("%s: the elevated helper exited with code %d%s", action, code, logTail(logPath))
	}
	return nil
}

// logTail reads the end of the elevated helper's log for an error message.
// The log records paths and errors only, never the subscription.
func logTail(path string) string {
	const maxTail = 4096
	if path == "" {
		return ""
	}
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ""
	}
	offset := int64(0)
	if info.Size() > maxTail {
		offset = info.Size() - maxTail
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxTail))
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return ""
	}
	return ":\n" + text
}
