//go:build linux

package isoconfig

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"
)

// linuxLocator finds the config-drive via its udev /dev/disk/by-label
// symlink and mounts it with the `mount` binary if it isn't already
// mounted. Works whether the platform presents the config-drive as a real
// optical device or a small virtio-blk disk carrying an ISO9660/UDF
// filesystem — unlike guessing a fixed device name (/dev/sr0).
type linuxLocator struct {
	runner CommandRunner
	logger *zap.Logger
}

// NewLocator returns a Locator backed by /dev/disk/by-label + mount/umount.
func NewLocator(runner CommandRunner, logger *zap.Logger) Locator {
	return &linuxLocator{runner: runner, logger: logger}
}

func (l *linuxLocator) Locate(ctx context.Context, mountPath, label string) (string, func(), error) {
	devicePath := "/dev/disk/by-label/" + label
	if _, err := os.Stat(devicePath); err != nil {
		return "", noop, nil // no config-drive attached — expected outcome
	}

	if isMounted(mountPath) {
		l.logger.Debug("config-drive already mounted", zap.String("mount_path", mountPath))
		return mountPath, noop, nil
	}

	if err := os.MkdirAll(mountPath, 0755); err != nil {
		return "", noop, fmt.Errorf("creating mount point %q: %w", mountPath, err)
	}

	if _, stderr, err := l.runner.Execute(ctx, "mount", devicePath, mountPath); err != nil {
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

// isMounted reports whether mountPath is currently a mount point, per
// /proc/self/mountinfo.
func isMounted(mountPath string) bool {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		// Format: <id> <parent-id> <major:minor> <root> <mount-point> ...
		if len(fields) > 4 && fields[4] == mountPath {
			return true
		}
	}
	return false
}
