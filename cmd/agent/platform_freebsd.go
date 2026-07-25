//go:build freebsd

package main

import (
	"context"

	"go.uber.org/zap"

	"github.com/plusclouds/ubuntu-agent/internal/modules/services"
)

// newServiceManager returns the pfSense/FreeBSD service manager stub.
// Real rc.d/service(8) integration is not yet implemented.
func newServiceManager(_ context.Context, logger *zap.Logger) (services.Manager, func()) {
	return services.New(logger), func() {}
}
