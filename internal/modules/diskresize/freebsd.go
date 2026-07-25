//go:build freebsd

package diskresize

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// freebsdResizer is a stub that reports disk resize as unsupported on
// pfSense. Growing a UFS/ZFS filesystem uses a different mechanism
// (growfs, zpool online -e) than Linux's growpart/resize2fs and is not yet
// implemented.
type freebsdResizer struct {
	logger *zap.Logger
}

// New returns a stub Resizer for pfSense/FreeBSD.
func New(_ CommandRunner, logger *zap.Logger) Resizer {
	logger.Info("disk resize: pfSense UFS/ZFS growth not yet implemented — disk.resize will return unsupported")
	return &freebsdResizer{logger: logger}
}

var errNotSupported = fmt.Errorf("disk resize is not yet supported on pfSense")

func (r *freebsdResizer) Resize(_ context.Context, target string) (*Result, error) {
	return &Result{Target: target, Message: errNotSupported.Error()}, nil
}
