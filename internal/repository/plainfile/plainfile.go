// Package plainfile reads a file named by a less privileged caller without
// letting the name reach anything else: the privileged helper reads only what
// the user could have written there, never a file the user cannot read that a
// link points it at.
package plainfile

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// ErrNotPlain is returned for a path that is a link, a file with more than
// one name, or no regular file at all.
var ErrNotPlain = errors.New("not a plain file")

// ErrTooLarge is returned for a file larger than the limit the caller set.
var ErrTooLarge = errors.New("file is larger than allowed")

// Read returns the content of the regular file at path, at most limit bytes.
// A symbolic link is refused, and so is a file with another hard link: either
// could be a name the caller made for a file only root may read. A file
// swapped for another between the check and the open is refused too.
func Read(path string, limit int64) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("look at the file (path=%q): %w", path, err)
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("%w (path=%q mode=%s)", ErrNotPlain, path, before.Mode())
	}
	if before.Size() > limit {
		return nil, fmt.Errorf("%w (path=%q size=%d limit=%d)", ErrTooLarge, path, before.Size(), limit)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open the file (path=%q): %w", path, err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("look at the opened file (path=%q): %w", path, err)
	}
	if !os.SameFile(before, opened) {
		return nil, fmt.Errorf("%w: it changed while being opened (path=%q)", ErrNotPlain, path)
	}
	links, err := linkCount(file)
	if err != nil {
		return nil, err
	}
	if links > 1 {
		return nil, fmt.Errorf("%w: it has %d hard links (path=%q)", ErrNotPlain, links, path)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read the file (path=%q): %w", path, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w (path=%q limit=%d)", ErrTooLarge, path, limit)
	}
	return data, nil
}
