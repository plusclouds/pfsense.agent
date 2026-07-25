//go:build linux

package diskresize

import (
	"context"
	"os/exec"
	"testing"

	"go.uber.org/zap"
)

// ---------------------------------------------------------------------------
// parseDevice
// ---------------------------------------------------------------------------

func TestParseDevice(t *testing.T) {
	cases := []struct {
		name     string
		wantBase string
		wantNum  string
		wantOK   bool
	}{
		{"sda1", "sda", "1", true},
		{"xvda1", "xvda", "1", true},
		{"vda2", "vda", "2", true},
		{"nvme0n1p1", "nvme0n1", "1", true},
		{"sda", "", "", false},
		{"nvme0n1", "", "", false},
	}
	for _, c := range cases {
		base, num, ok := parseDevice(c.name)
		if base != c.wantBase || num != c.wantNum || ok != c.wantOK {
			t.Errorf("parseDevice(%q) = (%q, %q, %v), want (%q, %q, %v)",
				c.name, base, num, ok, c.wantBase, c.wantNum, c.wantOK)
		}
	}
}

// ---------------------------------------------------------------------------
// fakeRunner — records calls and returns scripted responses.
// ---------------------------------------------------------------------------

type call struct {
	command string
	args    []string
}

type response struct {
	stdout string
	stderr string
	err    error
}

type fakeRunner struct {
	calls     []call
	responses map[string][]response // keyed by command; consumed in order
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{responses: map[string][]response{}}
}

func (f *fakeRunner) on(command string, resp response) *fakeRunner {
	f.responses[command] = append(f.responses[command], resp)
	return f
}

func (f *fakeRunner) Execute(_ context.Context, command string, args ...string) (string, string, error) {
	f.calls = append(f.calls, call{command: command, args: args})
	queue := f.responses[command]
	if len(queue) == 0 {
		return "", "", nil
	}
	resp := queue[0]
	f.responses[command] = queue[1:]
	return resp.stdout, resp.stderr, resp.err
}

func newTestResizer(runner CommandRunner, isPart bool) *linuxResizer {
	return &linuxResizer{
		runner:      runner,
		logger:      zap.NewNop(),
		isPartition: func(string) bool { return isPart },
	}
}

// ---------------------------------------------------------------------------
// grow()
// ---------------------------------------------------------------------------

func TestGrow_Partition_Ext4_RunsGrowpartThenResize2fs(t *testing.T) {
	fr := newFakeRunner()
	r := newTestResizer(fr, true)

	if err := r.grow(context.Background(), "/dev/sda1", "ext4", "/"); err != nil {
		t.Fatalf("grow() error: %v", err)
	}
	if len(fr.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d: %+v", len(fr.calls), fr.calls)
	}
	if fr.calls[0].command != "growpart" {
		t.Errorf("call 0: expected growpart, got %s", fr.calls[0].command)
	}
	if got := fr.calls[0].args; len(got) != 2 || got[0] != "/dev/sda" || got[1] != "1" {
		t.Errorf("growpart args: got %v, want [/dev/sda 1]", got)
	}
	if fr.calls[1].command != "resize2fs" {
		t.Errorf("call 1: expected resize2fs, got %s", fr.calls[1].command)
	}
	if got := fr.calls[1].args; len(got) != 1 || got[0] != "/dev/sda1" {
		t.Errorf("resize2fs args: got %v, want [/dev/sda1]", got)
	}
}

func TestGrow_Partition_XFS_UsesMountpointNotDevice(t *testing.T) {
	fr := newFakeRunner()
	r := newTestResizer(fr, true)

	if err := r.grow(context.Background(), "/dev/sda1", "xfs", "/data"); err != nil {
		t.Fatalf("grow() error: %v", err)
	}
	if fr.calls[1].command != "xfs_growfs" {
		t.Errorf("expected xfs_growfs, got %s", fr.calls[1].command)
	}
	if got := fr.calls[1].args; len(got) != 1 || got[0] != "/data" {
		t.Errorf("xfs_growfs args: got %v, want [/data] (mountpoint, not device)", got)
	}
}

func TestGrow_WholeDisk_SkipsGrowpart(t *testing.T) {
	fr := newFakeRunner()
	r := newTestResizer(fr, false) // not a partition

	if err := r.grow(context.Background(), "/dev/vdb", "ext4", "/data"); err != nil {
		t.Fatalf("grow() error: %v", err)
	}
	if len(fr.calls) != 1 {
		t.Fatalf("expected 1 call (no growpart), got %d: %+v", len(fr.calls), fr.calls)
	}
	if fr.calls[0].command != "resize2fs" {
		t.Errorf("expected resize2fs, got %s", fr.calls[0].command)
	}
}

func TestGrow_UnsupportedFilesystem_ReturnsError(t *testing.T) {
	fr := newFakeRunner()
	r := newTestResizer(fr, false)

	err := r.grow(context.Background(), "/dev/vdb", "btrfs", "/data")
	if err == nil {
		t.Fatal("expected error for unsupported filesystem")
	}
}

func TestGrow_GrowpartNochange_TreatedAsSuccess(t *testing.T) {
	fr := newFakeRunner().on("growpart", response{
		stdout: "NOCHANGE: partition 1 is size 1234. it cannot be grown",
		err:    errExit(1),
	})
	r := newTestResizer(fr, true)

	if err := r.grow(context.Background(), "/dev/sda1", "ext4", "/"); err != nil {
		t.Fatalf("grow() should treat NOCHANGE as success, got error: %v", err)
	}
}

func TestGrow_MissingTool_InstallsThenRetries(t *testing.T) {
	fr := newFakeRunner().
		on("resize2fs", response{err: exec.ErrNotFound}). // first attempt: not found
		on("apt-get", response{err: nil}).                // install succeeds
		on("resize2fs", response{err: nil})               // retry succeeds
	r := newTestResizer(fr, false)

	if err := r.grow(context.Background(), "/dev/vdb", "ext4", "/data"); err != nil {
		t.Fatalf("grow() error: %v", err)
	}

	var resize2fsCalls, aptCalls int
	for _, c := range fr.calls {
		switch c.command {
		case "resize2fs":
			resize2fsCalls++
		case "apt-get":
			aptCalls++
		}
	}
	if resize2fsCalls != 2 {
		t.Errorf("expected resize2fs to be called twice (fail, then retry), got %d", resize2fsCalls)
	}
	if aptCalls != 1 {
		t.Errorf("expected apt-get install to be called once, got %d", aptCalls)
	}
}

// errExit returns a plausible *exec.ExitError-shaped error for tests that
// only need "some non-nil, non-ErrNotFound error".
func errExit(code int) error {
	return &exitError{code: code}
}

type exitError struct{ code int }

func (e *exitError) Error() string { return "exit status" }
