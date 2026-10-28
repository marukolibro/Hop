// Package logfile keeps Hop's log on disk, so a user can send it after
// something went wrong: a window's log page is gone once Hop quits.
package logfile

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const (
	dirPerm  = 0o700
	filePerm = 0o600
)

// Writer appends to name in dir. When the file would pass maxBytes it moves
// to name.1, the older ones up one place, and the oldest of keep files goes.
type Writer struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	keep     int
	file     *os.File
	size     int64
}

// Open makes dir if it is missing and opens name for appending. keep is how
// many files there are in all, the current one included.
func Open(dir string, name string, maxBytes int64, keep int) (*Writer, error) {
	if keep < 1 || maxBytes < 1 {
		return nil, fmt.Errorf("log file limits must be positive (keep=%d max_bytes=%d)", keep, maxBytes)
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return nil, fmt.Errorf("create the log dir (dir=%q): %w", dir, err)
	}
	w := &Writer{path: filepath.Join(dir, name), maxBytes: maxBytes, keep: keep}
	if err := w.openCurrent(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Writer) openCurrent() error {
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, filePerm)
	if err != nil {
		return fmt.Errorf("open the log file (path=%q): %w", w.path, err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("stat the log file (path=%q): %w", w.path, err)
	}
	w.file, w.size = file, info.Size()
	return nil
}

// Write appends p, rotating first when it would not fit.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.size > 0 && w.size+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	if err != nil {
		return n, fmt.Errorf("write the log file (path=%q): %w", w.path, err)
	}
	return n, nil
}

// rotate shifts name to name.1, name.1 to name.2 and so on, dropping the
// last, and opens a new empty file.
func (w *Writer) rotate() error {
	if err := w.file.Close(); err != nil {
		return fmt.Errorf("close the log file (path=%q): %w", w.path, err)
	}
	for i := w.keep - 1; i >= 1; i-- {
		from := w.path
		if i > 1 {
			from = fmt.Sprintf("%s.%d", w.path, i-1)
		}
		to := fmt.Sprintf("%s.%d", w.path, i)
		if err := os.Rename(from, to); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("rotate the log file (from=%q to=%q): %w", from, to, err)
		}
	}
	if w.keep == 1 {
		if err := os.Remove(w.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("restart the log file (path=%q): %w", w.path, err)
		}
	}
	return w.openCurrent()
}

// Close closes the current file.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}

// WarningsOnly is a writer that passes on whole lines of mihomo's log, but
// not the info and debug ones: at info the core logs every connection with
// the host it went to, and a browsing history does not belong in a file.
// Lines without a level, such as a Go panic, are kept.
type WarningsOnly struct {
	mu      sync.Mutex
	dst     io.Writer
	partial []byte
}

// NewWarningsOnly passes the lines that are kept on to dst.
func NewWarningsOnly(dst io.Writer) *WarningsOnly {
	return &WarningsOnly{dst: dst}
}

var (
	infoMark  = []byte(" level=info ")
	debugMark = []byte(" level=debug ")
)

// Write buffers p until its lines are whole, then writes the ones to keep.
func (f *WarningsOnly) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.partial = append(f.partial, p...)
	for {
		end := bytes.IndexByte(f.partial, '\n')
		if end < 0 {
			return len(p), nil
		}
		line := f.partial[:end+1]
		if !bytes.Contains(line, infoMark) && !bytes.Contains(line, debugMark) {
			if _, err := f.dst.Write(line); err != nil {
				return 0, err
			}
		}
		f.partial = f.partial[end+1:]
	}
}
