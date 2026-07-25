package isoconfig_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"

	"github.com/plusclouds/ubuntu-agent/pkg/isoconfig"
)

const sampleJSON = `{"hostname":"test-host","virtual_machine_id":"abc-123","agent_api_key":"secret-key"}`

type fakeLocator struct {
	dir          string
	err          error
	unmountCalls int
}

func (f *fakeLocator) Locate(_ context.Context, _, _ string) (string, func(), error) {
	return f.dir, func() { f.unmountCalls++ }, f.err
}

func TestLoad_FoundAndMounted_ReadsAndCaches(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "pc-meta-data.json"), []byte(sampleJSON), 0600); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(t.TempDir(), "cache", "pc-meta-data.json")
	loc := &fakeLocator{dir: srcDir}

	meta, err := isoconfig.Load(context.Background(), loc, "/media/plusclouds-config", cachePath, "plusclouds-config", zap.NewNop())
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if meta.VMID() != "abc-123" {
		t.Errorf("VMID: got %q", meta.VMID())
	}
	if loc.unmountCalls != 1 {
		t.Errorf("expected unmount to be called once, got %d", loc.unmountCalls)
	}

	// Cache should now contain a copy, written atomically (no leftover .tmp), 0600.
	info, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("cache file not written: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("cache file perms: got %v, want 0600", info.Mode().Perm())
	}
	if _, err := os.Stat(cachePath + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("expected no leftover .tmp file, stat err: %v", err)
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != sampleJSON {
		t.Errorf("cached content mismatch: got %s", data)
	}
}

func TestLoad_NoConfigDriveNoCache_ReturnsEmpty(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "cache", "pc-meta-data.json")
	loc := &fakeLocator{dir: ""}

	meta, err := isoconfig.Load(context.Background(), loc, "/media/plusclouds-config", cachePath, "plusclouds-config", zap.NewNop())
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if meta.Raw() != nil {
		t.Error("expected nil Raw() when no config-drive and no cache")
	}
	if meta.VMID() != "" {
		t.Errorf("VMID: got %q, want empty", meta.VMID())
	}
}

func TestLoad_NoConfigDrive_FallsBackToCache(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "cache")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(cacheDir, "pc-meta-data.json")
	if err := os.WriteFile(cachePath, []byte(sampleJSON), 0600); err != nil {
		t.Fatal(err)
	}
	loc := &fakeLocator{dir: ""} // no config-drive this boot

	meta, err := isoconfig.Load(context.Background(), loc, "/media/plusclouds-config", cachePath, "plusclouds-config", zap.NewNop())
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if meta.VMID() != "abc-123" {
		t.Errorf("expected cached VMID, got %q", meta.VMID())
	}
}

func TestLoad_LocateError_FallsBackToCache(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "cache")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(cacheDir, "pc-meta-data.json")
	if err := os.WriteFile(cachePath, []byte(sampleJSON), 0600); err != nil {
		t.Fatal(err)
	}
	loc := &fakeLocator{err: os.ErrPermission} // mount failed

	meta, err := isoconfig.Load(context.Background(), loc, "/media/plusclouds-config", cachePath, "plusclouds-config", zap.NewNop())
	if err != nil {
		t.Fatalf("Load() should not surface locator errors, got: %v", err)
	}
	if meta.VMID() != "abc-123" {
		t.Errorf("expected cached VMID after locate error, got %q", meta.VMID())
	}
}
