//go:build linux

package isoconfig

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMountInfoHasConfigDrive_SelfBindMount_NotFooled is a regression test:
// under systemd's ProtectSystem=strict, every ReadWritePaths= entry
// (including the config-drive mount point) is implemented as a bind mount
// of that directory onto itself, which shows up in /proc/self/mountinfo
// exactly like a real mount — with the host's root filesystem (e.g. ext4)
// underneath. This must not be mistaken for the config-drive actually
// being mounted, or the locator skips mounting it and silently reads an
// empty directory. Fixture line captured verbatim from a real VM.
func TestMountInfoHasConfigDrive_SelfBindMount_NotFooled(t *testing.T) {
	mountInfo := "359 337 202:2 /media/plusclouds-config /media/plusclouds-config rw,nosuid,relatime shared:300 master:1 - ext4 /dev/xvda2 rw\n"
	path := writeMountInfo(t, mountInfo)

	if mountInfoHasConfigDrive(path, "/media/plusclouds-config") {
		t.Error("self-bind-mount of the host root fs must not be reported as the config-drive being mounted")
	}
}

func TestMountInfoHasConfigDrive_RealISO9660Mount_Detected(t *testing.T) {
	mountInfo := "412 337 11:0 / /media/plusclouds-config ro,relatime - iso9660 /dev/sr0 ro\n"
	path := writeMountInfo(t, mountInfo)

	if !mountInfoHasConfigDrive(path, "/media/plusclouds-config") {
		t.Error("a real iso9660 mount at the target path should be detected")
	}
}

func TestMountInfoHasConfigDrive_RealUDFMount_Detected(t *testing.T) {
	mountInfo := "412 337 11:0 / /media/plusclouds-config ro,relatime - udf /dev/sr0 ro\n"
	path := writeMountInfo(t, mountInfo)

	if !mountInfoHasConfigDrive(path, "/media/plusclouds-config") {
		t.Error("a real udf mount at the target path should be detected")
	}
}

func TestMountInfoHasConfigDrive_DifferentPath_NotDetected(t *testing.T) {
	mountInfo := "412 337 11:0 / /media/something-else ro,relatime - iso9660 /dev/sr0 ro\n"
	path := writeMountInfo(t, mountInfo)

	if mountInfoHasConfigDrive(path, "/media/plusclouds-config") {
		t.Error("a mount at an unrelated path must not match")
	}
}

func TestMountInfoHasConfigDrive_NoFile_ReturnsFalse(t *testing.T) {
	if mountInfoHasConfigDrive("/nonexistent/mountinfo", "/media/plusclouds-config") {
		t.Error("missing mountinfo file should report false, not panic or error")
	}
}

func writeMountInfo(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "mountinfo")
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}
