//go:build freebsd

package isoconfig

import "testing"

func TestMountOutputHasConfigDrive_RealCD9660Mount_Detected(t *testing.T) {
	out := "/dev/iso9660/cidata on /media/plusclouds-config (cd9660, local, read-only)\n"
	if !mountOutputHasConfigDrive(out, "/media/plusclouds-config") {
		t.Error("a real cd9660 mount at the target path should be detected")
	}
}

func TestMountOutputHasConfigDrive_RealUDFMount_Detected(t *testing.T) {
	out := "/dev/iso9660/cidata on /media/plusclouds-config (udf, local, read-only)\n"
	if !mountOutputHasConfigDrive(out, "/media/plusclouds-config") {
		t.Error("a real udf mount at the target path should be detected")
	}
}

func TestMountOutputHasConfigDrive_UnrelatedFilesystem_NotDetected(t *testing.T) {
	out := "/dev/ada0p2 on /media/plusclouds-config (ufs, local, journaled soft-updates)\n"
	if mountOutputHasConfigDrive(out, "/media/plusclouds-config") {
		t.Error("a non-cd9660/udf filesystem at the target path must not be reported as the config-drive")
	}
}

func TestMountOutputHasConfigDrive_DifferentPath_NotDetected(t *testing.T) {
	out := "/dev/iso9660/cidata on /media/something-else (cd9660, local, read-only)\n"
	if mountOutputHasConfigDrive(out, "/media/plusclouds-config") {
		t.Error("a mount at an unrelated path must not match")
	}
}

func TestMountOutputHasConfigDrive_MultilineOutput_FindsMatch(t *testing.T) {
	out := "" +
		"/dev/gpt/rootfs on / (ufs, local, soft-updates)\n" +
		"devfs on /dev (devfs)\n" +
		"/dev/iso9660/cidata on /media/plusclouds-config (cd9660, local, read-only)\n"
	if !mountOutputHasConfigDrive(out, "/media/plusclouds-config") {
		t.Error("expected to find the config-drive mount among other unrelated mount lines")
	}
}

func TestMountOutputHasConfigDrive_Empty_ReturnsFalse(t *testing.T) {
	if mountOutputHasConfigDrive("", "/media/plusclouds-config") {
		t.Error("empty mount output should report false")
	}
}
