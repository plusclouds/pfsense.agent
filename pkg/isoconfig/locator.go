package isoconfig

import "context"

// Locator finds the config-drive by volume label, mounting it if necessary,
// and returns the directory containing pc-meta-data.json. dir is "" (with a
// nil error) when no config-drive is present at all — that's an expected,
// not exceptional, outcome.
//
// unmount is always non-nil. It's a no-op closure when the drive was
// already mounted by something else (or when nothing was found), and
// actually unmounts when this call performed the mount itself. Callers
// should always defer it unconditionally.
type Locator interface {
	Locate(ctx context.Context, mountPath, label string) (dir string, unmount func(), err error)
}

// CommandRunner matches *executor.Executor.Execute's signature, letting
// tests inject a fake instead of shelling out for real. Duplicated locally
// rather than importing internal/modules/diskresize, to keep this pkg/
// package independent of that internal/ one.
type CommandRunner interface {
	Execute(ctx context.Context, command string, args ...string) (stdout, stderr string, err error)
}

func noop() {}
