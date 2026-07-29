//go:build !freebsd

package pfsense

import (
	"context"
	"encoding/json"
	"fmt"

	"go.uber.org/zap"

	"github.com/plusclouds/ubuntu-agent/internal/executor"
	"github.com/plusclouds/ubuntu-agent/pkg/isoconfig"
)

// stubManager reports pfSense operations as unsupported on non-FreeBSD platforms.
type stubManager struct {
	logger *zap.Logger
}

// New returns a stub Manager for platforms other than pfSense/FreeBSD.
// exec is accepted (but unused) so main.go can call New with the same
// signature regardless of build target.
func New(_ *executor.Executor, logger *zap.Logger) Manager {
	return &stubManager{logger: logger}
}

var errNotSupported = fmt.Errorf("pfSense operations are only supported on pfSense/FreeBSD")

func (m *stubManager) SetPassword(_ context.Context, username, _ string) (*SetPasswordResult, error) {
	return &SetPasswordResult{Username: username, Success: false, Message: errNotSupported.Error()}, nil
}

func (m *stubManager) SetDefaultUserPassword(_ context.Context, _ string) (*SetPasswordResult, error) {
	return &SetPasswordResult{Success: false, Message: errNotSupported.Error()}, nil
}

// ApplyBootNetworkConfig is a no-op outside pfSense/FreeBSD — boot
// orchestration treats a nil result as "nothing to do" and skips silently.
func (m *stubManager) ApplyBootNetworkConfig(_ context.Context, _ []isoconfig.VirtualNetworkCard) (*BootNetworkResult, error) {
	return nil, nil
}

func (m *stubManager) Revert(_ context.Context, _ json.RawMessage) error {
	return nil
}

func (m *stubManager) ListFirewallRules(_ context.Context) ([]FirewallRule, error) {
	return nil, errNotSupported
}

func (m *stubManager) CreateFirewallRule(_ context.Context, _ FirewallRuleInput) (*FirewallRule, error) {
	return nil, errNotSupported
}

func (m *stubManager) DeleteFirewallRule(_ context.Context, _ string) error {
	return errNotSupported
}

func (m *stubManager) ListPortForwards(_ context.Context) ([]PortForward, error) {
	return nil, errNotSupported
}

func (m *stubManager) CreatePortForward(_ context.Context, _ PortForwardInput) (*PortForward, error) {
	return nil, errNotSupported
}

func (m *stubManager) DeletePortForward(_ context.Context, _ string) error {
	return errNotSupported
}

func (m *stubManager) ListDHCPLeases(_ context.Context) ([]DHCPLease, error) {
	return nil, errNotSupported
}
