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

	if isConfigDriveMounted(mountPath) {
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

// isConfigDriveMounted reports whether an iso9660/udf filesystem — i.e. the
// config-drive itself, not just anything — is mounted at mountPath.
//
// It is not enough to check that mountPath merely appears as a mount point:
// under systemd's ProtectSystem=strict, every ReadWritePaths= entry (which
// includes the config-drive mount point, so the agent can mount onto it) is
// implemented as a bind mount of that directory onto itself inside the
// service's private mount namespace. That self-bind-mount shows up in this
// process's own /proc/self/mountinfo exactly like a real mount would, with
// the *host's root filesystem* underneath (e.g. ext4) — so a naive "is
// something mounted here" check reports true and the locator skips actually
// mounting the config-drive, silently reading an empty directory instead.
// Filtering on filesystem type tells the two apart.
func isConfigDriveMounted(mountPath string) bool {
	return mountInfoHasConfigDrive("/proc/self/mountinfo", mountPath)
}

func mountInfoHasConfigDrive(mountInfoPath, mountPath string) bool {
	f, err := os.Open(mountInfoPath)
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		// Format: <id> <parent-id> <major:minor> <root> <mount-point>
		// <options> <optional-fields...> - <fstype> <source> <super-options>
		pre, post, ok := strings.Cut(scanner.Text(), " - ")
		if !ok {
			continue
		}
		preFields := strings.Fields(pre)
		postFields := strings.Fields(post)
		if len(preFields) < 5 || len(postFields) < 1 {
			continue
		}
		if preFields[4] != mountPath {
			continue
		}
		switch postFields[0] {
		case "iso9660", "udf":
			return true
		}
	}
	return false
}
