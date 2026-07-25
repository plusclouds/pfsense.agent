// Package isoconfig reads VM metadata from the PlusClouds config-drive ISO.
// The ISO is mounted (typically at /media/plusclouds-config) and contains a
// single JSON file named pc-meta-data.json with VM identity, network, disk,
// and service role information.
package isoconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ---------------------------------------------------------------------------
// Raw metadata types — mirror the exact JSON structure on the ISO.
// ---------------------------------------------------------------------------

// VirtualMachineMetadata is the top-level structure of the ISO config-drive
// metadata file (pc-meta-data.json).
type VirtualMachineMetadata struct {
	Hostname            string               `json:"hostname"`
	Username            string               `json:"username"`
	Password            string               `json:"password"`
	VirtualMachineID    string               `json:"virtual_machine_id"`
	AgentAPIKey         string               `json:"agent_api_key"`
	VirtualDisks        []VirtualDisk        `json:"virtual_disks"`
	VirtualNetworkCards []VirtualNetworkCard `json:"virtual_network_cards"`
	ServiceRoles        []ServiceRole        `json:"service_roles"`
	ComputePool         ComputePool          `json:"compute_pool"`
	CloudNode           CloudNode            `json:"cloud_node"`
	SSHKeys             []string             `json:"ssh_keys"`
	EnvVars             []json.RawMessage    `json:"env_vars"`
	Tokens              []json.RawMessage    `json:"tokens"`
}

// DataList is the generic wrapper used by nested list fields that are still
// enveloped as { "data": [...] } (e.g. a NIC's ip_list).
type DataList[T any] struct {
	Data []T `json:"data"`
}

// VirtualDisk describes one virtual disk attached to the VM.
type VirtualDisk struct {
	DiskType     string `json:"disk_type"`
	DeviceNumber int    `json:"device_number"`
	TotalDisk    int64  `json:"total_disk"`
}

// VirtualNetworkCard describes one virtual NIC attached to the VM.
type VirtualNetworkCard struct {
	DeviceNumber int               `json:"device_number"`
	MACAddr      string            `json:"mac_addr"`
	NetworkName  string            `json:"network_name"`
	Network      Network           `json:"network"`
	IPList       DataList[IPEntry] `json:"ip_list"`
}

// Network holds the network configuration for a NIC's subnet.
type Network struct {
	Name           string   `json:"name"`
	IPAddr         string   `json:"ip_addr"`
	IPRangeStart   string   `json:"ip_range_start"`
	IPRangeEnd     string   `json:"ip_range_end"`
	Gateway        *string  `json:"gateway"`
	Subnet         string   `json:"subnet"`
	Netmask        string   `json:"netmask"`
	NetworkAddress string   `json:"network"`
	DHCPServer     string   `json:"dhcp_server"`
	DNSNameservers []string `json:"dns_nameservers"`
	MTU            int      `json:"mtu"`
}

// IPEntry is an IP address assignment on a NIC.
type IPEntry struct {
	ID         int    `json:"id"`
	IPAddr     string `json:"ip_addr"`
	IsReserved bool   `json:"is_reserved"`
}

// ServiceRole describes a service role assigned to the VM by the orchestrator.
type ServiceRole struct {
	Name   string `json:"name"`
	Config string `json:"config,omitempty"`
}

// ComputePool describes the hypervisor pool the VM runs on.
type ComputePool struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	PoolType          string  `json:"pool_type"`
	HypervisorType    *string `json:"hypervisor_type"`
	HypervisorVersion *string `json:"hypervisor_version"`
}

// CloudNode describes the physical/cloud node the VM runs on.
type CloudNode struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Location *string `json:"location"`
	Provider *string `json:"provider"`
	Region   *string `json:"region"`
}

// ---------------------------------------------------------------------------
// ISOMetadata — public façade over VirtualMachineMetadata
// ---------------------------------------------------------------------------

// ISOMetadata is the parsed config-drive metadata. All methods are nil-safe.
type ISOMetadata struct {
	raw *VirtualMachineMetadata
}

// New wraps a VirtualMachineMetadata in an ISOMetadata. Useful in tests and
// for constructing metadata from sources other than the config-drive file.
func New(vm *VirtualMachineMetadata) *ISOMetadata {
	return &ISOMetadata{raw: vm}
}

// Raw returns the underlying metadata struct. May be nil if the ISO was not
// mounted or the metadata file was not found.
func (m *ISOMetadata) Raw() *VirtualMachineMetadata {
	if m == nil {
		return nil
	}
	return m.raw
}

// VMID returns the virtual machine identifier (virtual_machine_id).
func (m *ISOMetadata) VMID() string {
	if m == nil || m.raw == nil {
		return ""
	}
	return m.raw.VirtualMachineID
}

// Hostname returns the VM hostname from the metadata.
func (m *ISOMetadata) Hostname() string {
	if m == nil || m.raw == nil {
		return ""
	}
	return m.raw.Hostname
}

// Username returns the default OS username provisioned on the VM.
func (m *ISOMetadata) Username() string {
	if m == nil || m.raw == nil {
		return ""
	}
	return m.raw.Username
}

// Password returns the provisioned OS password.
// This is sensitive — never log or expose it in API responses.
func (m *ISOMetadata) Password() string {
	if m == nil || m.raw == nil {
		return ""
	}
	return m.raw.Password
}

// TenantID returns an empty string. Tenant information is not present in the
// current metadata schema; it is resolved server-side from the agent token.
func (m *ISOMetadata) TenantID() string { return "" }

// APIKey returns the password field as the shared agent API key.
func (m *ISOMetadata) APIKey() string { return m.Password() }

// AgentAPIKey returns the NATS authentication token for this agent.
// It is stored in the agent_api_key field of the ISO metadata and used
// as the NATS password during connection (user = VMID, password = AgentAPIKey).
func (m *ISOMetadata) AgentAPIKey() string {
	if m == nil || m.raw == nil {
		return ""
	}
	return m.raw.AgentAPIKey
}

// AgentToken returns the NATS agent API key (alias for AgentAPIKey).
func (m *ISOMetadata) AgentToken() string { return m.AgentAPIKey() }

// ControlPlaneURL returns an empty string. The control-plane URL is not
// carried in the metadata file; configure it via the agent config file.
func (m *ISOMetadata) ControlPlaneURL() string { return "" }

// PrimaryIP returns the first assigned IP address from the first NIC, or "".
// The address is in CIDR notation (e.g. "185.255.172.129/32").
func (m *ISOMetadata) PrimaryIP() string {
	if m == nil || m.raw == nil {
		return ""
	}
	for _, nic := range m.raw.VirtualNetworkCards {
		if len(nic.IPList.Data) > 0 {
			return nic.IPList.Data[0].IPAddr
		}
	}
	return ""
}

// Gateway returns the gateway address from the first NIC's network config,
// or "" if not set (gateway may be null in the metadata).
func (m *ISOMetadata) Gateway() string {
	if m == nil || m.raw == nil {
		return ""
	}
	for _, nic := range m.raw.VirtualNetworkCards {
		if nic.Network.Gateway != nil {
			return *nic.Network.Gateway
		}
	}
	return ""
}

// DNSNameservers returns the DNS nameserver list from the first NIC, or nil.
func (m *ISOMetadata) DNSNameservers() []string {
	if m == nil || m.raw == nil {
		return nil
	}
	for _, nic := range m.raw.VirtualNetworkCards {
		if len(nic.Network.DNSNameservers) > 0 {
			return nic.Network.DNSNameservers
		}
	}
	return nil
}

// Tags returns an empty map. Tags are not present in the current metadata
// schema and will be populated by the orchestrator in future versions.
func (m *ISOMetadata) Tags() map[string]string { return nil }

// ---------------------------------------------------------------------------
// Reader
// ---------------------------------------------------------------------------

// Reader reads metadata from a mounted config-drive ISO directory.
type Reader struct {
	mountPath string
}

// NewReader creates a Reader that reads from the given mount path.
func NewReader(mountPath string) *Reader {
	return &Reader{mountPath: mountPath}
}

// Read parses pc-meta-data.json from the ISO mount point and returns an
// ISOMetadata. If the file is not present the returned ISOMetadata is
// non-nil but empty (all accessors will return zero values).
func (r *Reader) Read() (*ISOMetadata, error) {
	path := filepath.Join(r.mountPath, "pc-meta-data.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &ISOMetadata{}, nil
		}
		return nil, fmt.Errorf("reading pc-meta-data.json: %w", err)
	}

	var vm VirtualMachineMetadata
	if err := json.Unmarshal(data, &vm); err != nil {
		return nil, fmt.Errorf("parsing pc-meta-data.json: %w", err)
	}
	return &ISOMetadata{raw: &vm}, nil
}
