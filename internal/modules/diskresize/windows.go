//go:build windows

package diskresize

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go.uber.org/zap"
)

// windowsResizer grows an NTFS volume via the Storage module's
// Resize-Partition/Get-PartitionSupportedSize PowerShell cmdlets.
type windowsResizer struct {
	runner CommandRunner
	logger *zap.Logger
}

// New returns a Resizer backed by PowerShell's Storage module.
func New(runner CommandRunner, logger *zap.Logger) Resizer {
	return &windowsResizer{runner: runner, logger: logger}
}

type psResizeOutput struct {
	Before uint64 `json:"before"`
	After  uint64 `json:"after"`
}

func (r *windowsResizer) Resize(ctx context.Context, target string) (*Result, error) {
	letter := strings.ToUpper(strings.TrimSuffix(strings.TrimSpace(target), ":"))
	if letter == "" {
		letter = "C"
	}

	script := fmt.Sprintf(`
$ErrorActionPreference = "Stop"
$before = (Get-Partition -DriveLetter %[1]s).Size
$max = (Get-PartitionSupportedSize -DriveLetter %[1]s).SizeMax
Resize-Partition -DriveLetter %[1]s -Size $max
$after = (Get-Partition -DriveLetter %[1]s).Size
@{ before = $before; after = $after } | ConvertTo-Json -Compress
`, letter)

	stdout, stderr, err := r.runner.Execute(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	if err != nil {
		return nil, fmt.Errorf("resizing partition %s: %w (stderr: %s)", letter, err, stderr)
	}

	var out psResizeOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &out); err != nil {
		return nil, fmt.Errorf("parsing Resize-Partition output: %w (stdout: %s)", err, stdout)
	}

	return &Result{
		Target:      letter,
		BeforeBytes: out.Before,
		AfterBytes:  out.After,
		GrownBytes:  int64(out.After) - int64(out.Before),
		Message:     fmt.Sprintf("resized drive %s:", letter),
	}, nil
}
