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

	// ListFirewallRules returns every rule currently in pfSense's filter
	// config, in config.xml order.
	ListFirewallRules(ctx context.Context) ([]FirewallRule, error)

	// CreateFirewallRule appends a new filter rule and applies it live.
	// The returned rule's Tracker is the id to pass to DeleteFirewallRule.
	CreateFirewallRule(ctx context.Context, rule FirewallRuleInput) (*FirewallRule, error)

	// DeleteFirewallRule removes the filter rule matching tracker (as
	// returned by CreateFirewallRule/ListFirewallRules) and applies the
	// change live. Returns an error if no rule matches.
	DeleteFirewallRule(ctx context.Context, tracker string) error

	// ListPortForwards returns every NAT port-forward rule currently in
	// pfSense's config, in config.xml order.
	ListPortForwards(ctx context.Context) ([]PortForward, error)

	// CreatePortForward appends a new NAT port-forward rule and applies it
	// live. The returned rule's Tracker is the id to pass to
	// DeletePortForward.
	CreatePortForward(ctx context.Context, pf PortForwardInput) (*PortForward, error)

	// DeletePortForward removes the NAT rule matching tracker (as returned
	// by CreatePortForward/ListPortForwards) and applies the change live.
	// Returns an error if no rule matches.
	DeletePortForward(ctx context.Context, tracker string) error
}

// FirewallRule is one pfSense filter (firewall) rule.
type FirewallRule struct {
	Tracker     string `json:"tracker"`
	Interface   string `json:"interface"`
	Action      string `json:"action"`
	Protocol    string `json:"protocol,omitempty"`
	Source      string `json:"source"`
	SourcePort  string `json:"source_port,omitempty"`
	Destination string `json:"destination"`
	DestPort    string `json:"destination_port,omitempty"`
	Description string `json:"description,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
}

// FirewallRuleInput is the caller-supplied shape for CreateFirewallRule.
// Interface is pfSense's logical role name (lan/wan/optN, matching
// InterfaceApplyResult.Logical), not the OS interface name. Source and
// Destination accept a bare IP/CIDR, "any", or a pfSense network alias
// (e.g. "lan" for that interface's subnet).
type FirewallRuleInput struct {
	Interface   string `json:"interface"`
	Action      string `json:"action"` // pass, block, or reject
	Protocol    string `json:"protocol,omitempty"`
	Source      string `json:"source"`
	SourcePort  string `json:"source_port,omitempty"`
	Destination string `json:"destination"`
	DestPort    string `json:"destination_port,omitempty"`
	Description string `json:"description,omitempty"`
}

// PortForward is one pfSense NAT port-forward rule.
type PortForward struct {
	Tracker     string `json:"tracker"`
	Interface   string `json:"interface"`
	Protocol    string `json:"protocol"`
	Source      string `json:"source,omitempty"`
	SourcePort  string `json:"source_port,omitempty"`
	Destination string `json:"destination,omitempty"`
	DestPort    string `json:"destination_port"`
	TargetIP    string `json:"target_ip"`
	TargetPort  string `json:"target_port"`
	Description string `json:"description,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
}

// PortForwardInput is the caller-supplied shape for CreatePortForward.
// Interface is pfSense's logical role name (lan/wan/optN). Destination
// defaults to that interface's own address (pfSense's "<if>ip" shorthand,
// the common port-forward case) when left empty.
type PortForwardInput struct {
	Interface   string `json:"interface"`
	Protocol    string `json:"protocol"` // tcp, udp, or tcp/udp
	Source      string `json:"source,omitempty"`
	SourcePort  string `json:"source_port,omitempty"`
	Destination string `json:"destination,omitempty"`
	DestPort    string `json:"destination_port"`
	TargetIP    string `json:"target_ip"`
	TargetPort  string `json:"target_port"`
	Description string `json:"description,omitempty"`
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
