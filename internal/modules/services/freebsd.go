//go:build freebsd

package services

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// freebsdManager is a stub that reports all service operations as unsupported.
// pfSense manages services via rc.d, not systemd; real service(8)/rc.d
// integration can replace this stub when pfSense service management is required.
type freebsdManager struct {
	logger *zap.Logger
}

// New returns a stub Manager for FreeBSD/pfSense.
func New(logger *zap.Logger) Manager {
	logger.Info("service management: pfSense rc.d integration not yet implemented — service operations will return unsupported")
	return &freebsdManager{logger: logger}
}

var errNotSupported = fmt.Errorf("service management is not yet supported on pfSense")

func (m *freebsdManager) List(_ context.Context) ([]ServiceInfo, error) {
	return nil, errNotSupported
}
func (m *freebsdManager) Get(_ context.Context, _ string) (*ServiceInfo, error) {
	return nil, errNotSupported
}
func (m *freebsdManager) Start(_ context.Context, name string) (*ActionResult, error) {
	return &ActionResult{Service: name, Action: ActionStart, Success: false, Message: errNotSupported.Error()}, nil
}
func (m *freebsdManager) Stop(_ context.Context, name string) (*ActionResult, error) {
	return &ActionResult{Service: name, Action: ActionStop, Success: false, Message: errNotSupported.Error()}, nil
}
func (m *freebsdManager) Restart(_ context.Context, name string) (*ActionResult, error) {
	return &ActionResult{Service: name, Action: ActionRestart, Success: false, Message: errNotSupported.Error()}, nil
}
func (m *freebsdManager) Reload(_ context.Context, name string) (*ActionResult, error) {
	return &ActionResult{Service: name, Action: ActionReload, Success: false, Message: errNotSupported.Error()}, nil
}
func (m *freebsdManager) Enable(_ context.Context, name string) (*ActionResult, error) {
	return &ActionResult{Service: name, Action: ActionEnable, Success: false, Message: errNotSupported.Error()}, nil
}
func (m *freebsdManager) Disable(_ context.Context, name string) (*ActionResult, error) {
	return &ActionResult{Service: name, Action: ActionDisable, Success: false, Message: errNotSupported.Error()}, nil
}
