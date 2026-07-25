// Package diskresize grows a filesystem to fill its underlying disk after a
// cloud volume expand. Platform-specific implementations are selected at
// compile time via build tags, mirroring the internal/modules/services
// package's Manager pattern.
package diskresize

import "context"

// Resizer is the OS-agnostic interface for growing a filesystem.
// target is a mountpoint on Linux (e.g. "/") or a drive letter on Windows
// (e.g. "C").
type Resizer interface {
	Resize(ctx context.Context, target string) (*Result, error)
}

// CommandRunner matches *executor.Executor.Execute's signature, letting
// tests inject a fake instead of shelling out for real.
type CommandRunner interface {
	Execute(ctx context.Context, command string, args ...string) (stdout, stderr string, err error)
}

// Result reports the outcome of a resize operation.
type Result struct {
	Target      string `json:"target"`
	Device      string `json:"device,omitempty"`
	Fstype      string `json:"fstype,omitempty"`
	BeforeBytes uint64 `json:"before_bytes"`
	AfterBytes  uint64 `json:"after_bytes"`
	GrownBytes  int64  `json:"grown_bytes"`
	Message     string `json:"message"`
}
