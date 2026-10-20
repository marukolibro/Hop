//go:build windows

package mihomo

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// commandLine returns the command line of process pid, or "" when no such
// process runs. It reads the process's own memory the way a debugger does:
// the basic information gives the PEB's address, the PEB its parameters,
// the parameters the command line. The struct offsets are taken from the
// windows package's own definitions, so they hold wherever Hop builds; only
// a process of another bitness than Hop could not be read this way, and the
// core Hop starts is never that.
func commandLine(pid int) (string, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			// No process runs with this pid.
			return "", nil
		}
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			// Hop's core runs as the same user and can always be opened;
			// a process that cannot is someone else's that reused the pid.
			return "", nil
		}
		return "", fmt.Errorf("open pid %d to read its command line: %w", pid, err)
	}
	defer windows.CloseHandle(handle)

	var info windows.PROCESS_BASIC_INFORMATION
	var returned uint32
	if err := windows.NtQueryInformationProcess(handle, windows.ProcessBasicInformation, unsafe.Pointer(&info), uint32(unsafe.Sizeof(info)), &returned); err != nil {
		return "", fmt.Errorf("query pid %d for its process information: %w", pid, err)
	}
	if info.PebBaseAddress == nil {
		// The process is on its way out.
		return "", nil
	}

	// The pointers are into the other process's memory: only the bytes at
	// them can be taken, one field at a time.
	params, err := readProcessPointer(handle, uintptr(unsafe.Pointer(info.PebBaseAddress))+unsafe.Offsetof(windows.PEB{}.ProcessParameters))
	if err != nil {
		return "", fmt.Errorf("read the parameter block address of pid %d: %w", pid, err)
	}
	if params == 0 {
		return "", nil
	}
	var line windows.NTUnicodeString
	if err := readProcessBytes(handle, params+unsafe.Offsetof(windows.RTL_USER_PROCESS_PARAMETERS{}.CommandLine), unsafe.Pointer(&line), unsafe.Sizeof(line)); err != nil {
		return "", fmt.Errorf("read the command line length of pid %d: %w", pid, err)
	}
	if line.Length == 0 || line.Buffer == nil {
		return "", nil
	}
	// Length counts bytes of UTF-16 text, and is always even.
	buf := make([]uint16, line.Length/2)
	if err := readProcessBytes(handle, uintptr(unsafe.Pointer(line.Buffer)), unsafe.Pointer(&buf[0]), uintptr(line.Length)); err != nil {
		return "", fmt.Errorf("read the command line of pid %d: %w", pid, err)
	}
	return strings.TrimSpace(windows.UTF16ToString(buf)), nil
}

// readProcessPointer reads one pointer-sized value at base of the process.
func readProcessPointer(handle windows.Handle, base uintptr) (uintptr, error) {
	var value uintptr
	err := readProcessBytes(handle, base, unsafe.Pointer(&value), unsafe.Sizeof(value))
	return value, err
}

// readProcessBytes reads size bytes at base of the process into dst.
func readProcessBytes(handle windows.Handle, base uintptr, dst unsafe.Pointer, size uintptr) error {
	var read uintptr
	if err := windows.ReadProcessMemory(handle, base, (*byte)(dst), size, &read); err != nil {
		return err
	}
	if read != size {
		return fmt.Errorf("read %d of %d bytes at %#x", read, size, base)
	}
	return nil
}
