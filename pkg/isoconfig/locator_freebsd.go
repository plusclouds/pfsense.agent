//go:build freebsd

package isoconfig

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"
)

// freebsdLocator finds the config-drive via its GEOM label device
// (/dev/iso9660/<label> — the FreeBSD analog of Linux's
// /dev/disk/by-label/<label>) and mounts it with `mount -t cd9660` if it
// isn't already mounted. Works whether the platform presents the
// config-drive as a real optical device or a small virtio-blk disk
// carrying an ISO9660 filesystem.
type freebsdLocator struct {
	runner CommandRunner
	logger *zap.Logger
}

// NewLocator returns a Locator backed by /dev/iso9660/<label> + mount/umount.
func NewLocator(runner CommandRunner, logger *zap.Logger) Locator {
	return &freebsdLocator{runner: runner, logger: logger}
}

func (l *freebsdLocator) Locate(ctx context.Context, mountPath, label string) (string, func(), error) {
	devicePath := "/dev/iso9660/" + label
	if _, err := os.Stat(devicePath); err != nil {
		return "", noop, nil // no config-drive attached — expected outcome
	}

	mounted, err := l.isMounted(ctx, mountPath)
	if err != nil {
		return "", noop, fmt.Errorf("checking mount status of %q: %w", mountPath, err)
	}
	if mounted {
		l.logger.Debug("config-drive already mounted", zap.String("mount_path", mountPath))
		return mountPath, noop, nil
	}

	if err := os.MkdirAll(mountPath, 0755); err != nil {
		return "", noop, fmt.Errorf("creating mount point %q: %w", mountPath, err)
	}

	if _, stderr, err := l.runner.Execute(ctx, "mount", "-t", "cd9660", devicePath, mountPath); err != nil {
		return "", noop, fmt.Errorf("mounting %q at %q: %w (stderr: %s)", devicePath, mountPath, err, stderr)
	}

	l.logger.Info("mounted config-drive", zap.String("device", devicePath), zap.String("mount_path", mountPath))

	unmount := func() {
		if _, stderr, err := l.runner.Execute(ctx, "umount", mountPath); err != nil {
			l.logger.Warn("could not unmount config-drive",
				zap.String("mount_path", mountPath), zap.Error(err), zap.String("stderr", stderr))
			return
		}
		l.logger.Info("unmounted config-drive", zap.String("mount_path", mountPath))
	}
	return mountPath, unmount, nil
}

// isMounted reports whether mountPath is currently a mount point. FreeBSD
// has no /proc/self/mountinfo by default, so this shells out to `mount` and
// scans its output for a line mentioning mountPath as the mount point
// (format: "<device> on <mountPath> (<options>)").
func (l *freebsdLocator) isMounted(ctx context.Context, mountPath string) (bool, error) {
	stdout, _, err := l.runner.Execute(ctx, "mount")
	if err != nil {
		return false, err
	}
	needle := " on " + mountPath + " ("
	for _, line := range strings.Split(stdout, "\n") {
		if strings.Contains(line, needle) {
			return true, nil
		}
	}
	return false, nil
}
