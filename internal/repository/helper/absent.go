package helper

import (
	"context"
	"errors"

	"hop/internal/model"
)

// ErrAbsent says this build keeps away from the privileged helper: the helper
// is one per machine, so a build that is not the installed Hop would stop or
// replace the core the installed Hop runs under it.
var ErrAbsent = errors.New("this build does not use the privileged helper, which belongs to the installed Hop")

// Absent stands in for the helper's client and installer in a build that must
// not touch the helper. Every call fails with ErrAbsent; nothing reaches the
// helper's socket or installs anything.
type Absent struct{}

func (Absent) Version(context.Context) (string, error) { return "", ErrAbsent }

func (Absent) Status(context.Context) (Status, error) { return Status{}, ErrAbsent }

func (Absent) InstallCore(context.Context, model.CoreArchive, string) error { return ErrAbsent }

func (Absent) InstallGeodata(context.Context, string) error { return ErrAbsent }

func (Absent) StartCore(context.Context, []byte) error { return ErrAbsent }

func (Absent) ApplyConfig(context.Context, []byte) error { return ErrAbsent }

func (Absent) StopCore(context.Context) error { return ErrAbsent }

func (Absent) InstalledVersion(context.Context) (string, error) { return "", ErrAbsent }

func (Absent) Install(context.Context, string, string, string, int) error { return ErrAbsent }

func (Absent) Uninstall(context.Context) error { return ErrAbsent }
