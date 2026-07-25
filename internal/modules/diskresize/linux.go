//go:build linux

package diskresize

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/shirou/gopsutil/v3/disk"
	"go.uber.org/zap"
)

// linuxResizer grows a filesystem via growpart (partition table) and
// resize2fs/xfs_growfs (filesystem), installing either tool on demand if
// it isn't already present.
type linuxResizer struct {
	runner CommandRunner
	logger *zap.Logger
	// isPartition reports whether a kernel device name (no /dev/ prefix) is
	// a partition rather than a whole disk. Overridable in tests; defaults
	// to a sysfs check.
	isPartition func(devName string) bool
}

// New returns a Resizer backed by growpart/resize2fs/xfs_growfs.
func New(runner CommandRunner, logger *zap.Logger) Resizer {
	return &linuxResizer{runner: runner, logger: logger, isPartition: isPartitionSysfs}
}

var (
	reNVMEPartition        = regexp.MustCompile(`^(nvme\d+n\d+)p(\d+)$`)
	reTraditionalPartition = regexp.MustCompile(`^([a-z]+)(\d+)$`)
)

// parseDevice splits a partition device name (e.g. "sda1", "xvda1",
// "nvme0n1p1") into its base device and partition number. ok is false if
// name doesn't look like a partition (e.g. "sda", "nvme0n1" — whole disks).
func parseDevice(name string) (base, num string, ok bool) {
	if m := reNVMEPartition.FindStringSubmatch(name); m != nil {
		return m[1], m[2], true
	}
	if m := reTraditionalPartition.FindStringSubmatch(name); m != nil {
		return m[1], m[2], true
	}
	return "", "", false
}

// isPartitionSysfs reports whether the given kernel device name (no /dev/
// prefix) is a partition rather than a whole disk, per sysfs.
func isPartitionSysfs(devName string) bool {
	_, err := os.Stat("/sys/class/block/" + devName + "/partition")
	return err == nil
}

// Resize resolves target to its underlying device/filesystem via gopsutil,
// then delegates the actual grow to grow().
func (r *linuxResizer) Resize(ctx context.Context, target string) (*Result, error) {
	partitions, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("disk.Partitions: %w", err)
	}

	var device, fstype string
	found := false
	for _, p := range partitions {
		if p.Mountpoint == target {
			device, fstype = p.Device, p.Fstype
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("no mounted filesystem at %q", target)
	}

	before, err := disk.UsageWithContext(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("reading usage for %q: %w", target, err)
	}

	if err := r.grow(ctx, device, fstype, target); err != nil {
		return nil, err
	}

	after, err := disk.UsageWithContext(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("reading usage for %q after resize: %w", target, err)
	}

	return &Result{
		Target:      target,
		Device:      device,
		Fstype:      fstype,
		BeforeBytes: before.Total,
		AfterBytes:  after.Total,
		GrownBytes:  int64(after.Total) - int64(before.Total),
		Message:     fmt.Sprintf("resized %s (%s) at %s", device, fstype, target),
	}, nil
}

// grow runs growpart (if device is a partition, not a whole disk) followed
// by the filesystem-specific grow tool. Pure orchestration — no gopsutil
// calls — so it can be exercised in tests with a fake CommandRunner.
func (r *linuxResizer) grow(ctx context.Context, device, fstype, target string) error {
	devName := strings.TrimPrefix(device, "/dev/")
	if r.isPartition(devName) {
		base, num, ok := parseDevice(devName)
		if !ok {
			return fmt.Errorf("could not parse partition device %q", device)
		}
		if err := r.runOrInstall(ctx, "growpart", "cloud-guest-utils", "cloud-utils-growpart", "/dev/"+base, num); err != nil {
			return fmt.Errorf("growing partition: %w", err)
		}
	}

	switch fstype {
	case "ext2", "ext3", "ext4":
		if err := r.runOrInstall(ctx, "resize2fs", "e2fsprogs", "e2fsprogs", device); err != nil {
			return fmt.Errorf("growing filesystem: %w", err)
		}
	case "xfs":
		if err := r.runOrInstall(ctx, "xfs_growfs", "xfsprogs", "xfsprogs", target); err != nil {
			return fmt.Errorf("growing filesystem: %w", err)
		}
	default:
		return fmt.Errorf("unsupported filesystem %q for resize (supported: ext2/ext3/ext4, xfs)", fstype)
	}
	return nil
}

// runOrInstall runs tool with args, and if the binary isn't found, attempts
// to install it via whichever package manager is present (apt, then dnf,
// then yum), then retries once.
func (r *linuxResizer) runOrInstall(ctx context.Context, tool, aptPkg, rhelPkg string, args ...string) error {
	stdout, stderr, err := r.runner.Execute(ctx, tool, args...)
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToUpper(stdout+stderr), "NOCHANGE") {
		r.logger.Info("resize: no change needed", zap.String("tool", tool))
		return nil
	}
	if !errors.Is(err, exec.ErrNotFound) {
		return err
	}

	r.logger.Info("resize: tool not found, attempting install", zap.String("tool", tool))
	if instErr := r.installPackage(ctx, aptPkg, rhelPkg); instErr != nil {
		return fmt.Errorf("%s not found and install failed: %w", tool, instErr)
	}

	stdout, stderr, err = r.runner.Execute(ctx, tool, args...)
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToUpper(stdout+stderr), "NOCHANGE") {
		return nil
	}
	return fmt.Errorf("%s still failing after install: %w", tool, err)
}

// installPackage tries apt-get, then dnf, then yum — whichever package
// manager is actually present on this VM.
func (r *linuxResizer) installPackage(ctx context.Context, aptPkg, rhelPkg string) error {
	managers := []struct {
		bin  string
		args []string
	}{
		{"apt-get", []string{"install", "-y", aptPkg}},
		{"dnf", []string{"install", "-y", rhelPkg}},
		{"yum", []string{"install", "-y", rhelPkg}},
	}

	var lastErr error
	for _, m := range managers {
		_, _, err := r.runner.Execute(ctx, m.bin, m.args...)
		if err == nil {
			return nil
		}
		if errors.Is(err, exec.ErrNotFound) {
			continue
		}
		lastErr = err
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("no supported package manager found (tried apt-get, dnf, yum)")
}
