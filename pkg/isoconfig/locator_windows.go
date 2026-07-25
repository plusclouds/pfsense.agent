//go:build windows

package isoconfig

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"
)

// windowsLocator finds the config-drive by volume label. Windows
// auto-assigns a drive letter to attached optical/removable media, so
// there's no explicit "mount" step (and correspondingly, nothing to
// unmount) — mountPath is unused here, kept only to satisfy the Locator
// interface shared with the Linux implementation.
type windowsLocator struct {
	runner CommandRunner
	logger *zap.Logger
}

// NewLocator returns a Locator backed by Get-Volume/FileSystemLabel lookup.
func NewLocator(runner CommandRunner, logger *zap.Logger) Locator {
	return &windowsLocator{runner: runner, logger: logger}
}

func (l *windowsLocator) Locate(ctx context.Context, _ string, label string) (string, func(), error) {
	script := fmt.Sprintf(
		`(Get-Volume | Where-Object FileSystemLabel -eq '%s' | Select-Object -First 1).DriveLetter`,
		label,
	)
	stdout, stderr, err := l.runner.Execute(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	if err != nil {
		return "", noop, fmt.Errorf("querying volumes for label %q: %w (stderr: %s)", label, err, stderr)
	}

	letter := strings.TrimSpace(stdout)
	if letter == "" {
		return "", noop, nil // no config-drive attached — expected outcome
	}

	l.logger.Info("found config-drive volume", zap.String("drive_letter", letter))
	return letter + `:\`, noop, nil
}
