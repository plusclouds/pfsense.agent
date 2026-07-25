// Package pfsense provides pfSense-specific configuration operations that
// go through pfSense's own PHP config/auth subsystem rather than
// reimplementing config.xml or credential handling in Go.
package pfsense

import (
	"context"
	"encoding/json"

	"github.com/plusclouds/ubuntu-agent/pkg/isoconfig"
)

// Manager is the entry point used by the dispatcher and boot orchestration
// for pfSense operations. Platform-specific implementations are selected at
// compile time via build tags.
type Manager interface {
	// SetPassword changes the password of an existing local pfSense user.
	SetPassword(ctx context.Context, username, password string) (*SetPasswordResult, error)

	// ApplyBootNetworkConfig sets static IP/subnet/gateway/DNS/MTU on the
	// already-assigned pfSense interfaces matching the given NICs' MAC
	// addresses. It does not perform interface assignment. The returned
	// result's Snapshot can be passed to Revert to undo the change.
	// Returns (nil, nil) on platforms other than pfSense/FreeBSD.
	ApplyBootNetworkConfig(ctx context.Context, cards []isoconfig.VirtualNetworkCard) (*BootNetworkResult, error)

	// Revert restores interface/gateway/DNS config from a snapshot
	// previously returned by ApplyBootNetworkConfig.
	Revert(ctx context.Context, snapshot json.RawMessage) error
}

// SetPasswordResult is the outcome of a SetPassword call.
type SetPasswordResult struct {
	Username string `json:"username"`
	Success  bool   `json:"success"`
	Message  string `json:"message,omitempty"`
}

// BootNetworkResult is the outcome of an ApplyBootNetworkConfig call.
type BootNetworkResult struct {
	// Snapshot is the pre-change interface/gateway/DNS config, opaque to Go —
	// only pfSense's own scripts read or write it. Pass to Revert to undo.
	Snapshot json.RawMessage `json:"-"`
	// Interfaces reports the outcome for each NIC in the metadata.
	Interfaces []InterfaceApplyResult `json:"interfaces"`
}

// InterfaceApplyResult is the outcome of applying config for one NIC.
type InterfaceApplyResult struct {
	MACAddr string `json:"mac_addr"`
	IfName  string `json:"ifname,omitempty"`
	Logical string `json:"logical,omitempty"`
	Applied bool   `json:"applied"`
	Message string `json:"message,omitempty"`
}
