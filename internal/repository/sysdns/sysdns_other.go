//go:build !darwin

package sysdns

import (
	"context"
	"errors"
)

// ErrUnsupported says the system DNS cannot be managed on this platform.
var ErrUnsupported = errors.New("managing the system DNS is not supported on this platform yet")

// Read is a stub everywhere but darwin.
func Read(context.Context, string) ([]string, error) { return nil, ErrUnsupported }

// Write is a stub everywhere but darwin.
func Write(context.Context, string, []string) error { return ErrUnsupported }

// FlushCache is a stub everywhere but darwin.
func FlushCache(context.Context) error { return ErrUnsupported }

// Backup mirrors the darwin type so the helper compiles anywhere.
type Backup struct {
	Services map[string][]string `json:"services"`
}

// Encode is a stub everywhere but darwin.
func (b Backup) Encode() ([]byte, error) { return nil, ErrUnsupported }

// DecodeBackup is a stub everywhere but darwin.
func DecodeBackup([]byte) (Backup, error) { return Backup{}, ErrUnsupported }
