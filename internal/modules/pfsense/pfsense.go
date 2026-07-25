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
	// SetPassword changes the password of an existing local pfSense user,
	// matched by exact name.
	SetPassword(ctx context.Context, username, password string) (*SetPasswordResult, error)

	// SetDefaultUserPassword changes the password of pfSense's default
	// superuser account, matched by uid 0 rather than by name — pfSense
	// ships this account named "admin", but the box owner can rename it,
	// so name-matching a generic "root"/"admin" guess from cross-platform
	// VM metadata isn't reliable. Used only by boot-time provisioning,
	// never by the NATS pfsense.set_password command (which must not
	// silently fall back to the superuser account on a name mismatch).
	SetDefaultUserPassword(ctx context.Context, password string) (*SetPasswordResult, error)

	// ApplyBootNetworkConfig sets static IP/subnet/gateway/DNS/MTU on
	// pfSense interfaces matching the given NICs' MAC addresses. A NIC with
	// no pfSense interface role yet is auto-assigned as lan, but only if
	// lan doesn't already exist (wan and an existing lan are never
	// reassigned or touched); broader interface assignment is still out of
	// scope. The returned result's Snapshot can be passed to Revert to
	// undo the change. Returns (nil, nil) on platforms other than
	// pfSense/FreeBSD.
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
	// Assigned is true if this NIC had no pfSense interface role yet and
	// was auto-assigned as lan (only ever lan, only when lan didn't
	// already exist — see scripts/netconfig/apply.php).
	Assigned bool   `json:"assigned,omitempty"`
	Message  string `json:"message,omitempty"`
}
