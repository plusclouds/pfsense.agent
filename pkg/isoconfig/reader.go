// Package isoconfig reads VM metadata from the PlusClouds config-drive ISO.
// The ISO is mounted (typically at /media/plusclouds-config) and contains a
// JSON file named pc-meta-data.json with VM identity, network, disk, and
// service role information. metadata.yaml/metadata.json are also supported
// as legacy fallback names.
package isoconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Raw metadata types — mirror the exact YAML/JSON structure on the ISO.
// ---------------------------------------------------------------------------

// VirtualMachineMetadata is the top-level structure of the ISO config-drive
// metadata file (pc-meta-data.json, with metadata.yaml/metadata.json as
// legacy fallback names).
type VirtualMachineMetadata struct {
	Hostname            string               `yaml:"hostname"              json:"hostname"`
	Username            string               `yaml:"username"              json:"username"`
	Password            string               `yaml:"password"              json:"password"`
	AgentAPIKey         string               `yaml:"agent_api_key"         json:"agent_api_key"`
	VirtualMachineID    string               `yaml:"virtual_machine_id"    json:"virtual_machine_id"`
	VirtualDisks        []VirtualDisk        `yaml:"virtual_disks"         json:"virtual_disks"`
	VirtualNetworkCards []VirtualNetworkCard `yaml:"virtual_network_cards" json:"virtual_network_cards"`
	ServiceRoles        []ServiceRole        `yaml:"service_roles"         json:"service_roles"`
	SSHPublicKeys       []string             `yaml:"ssh_keys"              json:"ssh_keys"`
}

// DataList is the generic wrapper used throughout the metadata for lists.
// The API consistently uses { "data": [...] } as an envelope.
type DataList[T any] struct {
	Data []T `yaml:"data" json:"data"`
}

// VirtualDisk describes one virtual disk attached to the VM.
type VirtualDisk struct {
	DiskType     string `yaml:"disk_type"     json:"disk_type"`
	DeviceNumber int    `yaml:"device_number" json:"device_number"`
	TotalDisk    int64  `yaml:"total_disk"    json:"total_disk"`
}

// VirtualNetworkCard describes one virtual NIC attached to the VM.
type VirtualNetworkCard struct {
	DeviceNumber int               `yaml:"device_number" json:"device_number"`
	MACAddr      string            `yaml:"mac_addr"      json:"mac_addr"`
	Network      Network           `yaml:"network"       json:"network"`
	IPList       DataList[IPEntry] `yaml:"ip_list"        json:"ip_list"`
}

// Network holds the network configuration for a NIC's subnet.
type Network struct {
	IPAddr         string   `yaml:"ip_addr"         json:"ip_addr"`
	IPRangeStart   string   `yaml:"ip_range_start"  json:"ip_range_start"`
	IPRangeEnd     string   `yaml:"ip_range_end"    json:"ip_range_end"`
	Gateway        *string  `yaml:"gateway"         json:"gateway"`
	Subnet         string   `yaml:"subnet"          json:"subnet"`
	Netmask        string   `yaml:"netmask"         json:"netmask"`
	NetworkAddress string   `yaml:"network"         json:"network"`
	DHCPServer     string   `yaml:"dhcp_server"     json:"dhcp_server"`
	DNSNameservers []string `yaml:"dns_nameservers" json:"dns_nameservers"`
	MTU            int      `yaml:"mtu"             json:"mtu"`
}

// IPEntry is an IP address assignment on a NIC.
type IPEntry struct {
	ID         int    `yaml:"id"          json:"id"`
	IPAddr     string `yaml:"ip_addr"     json:"ip_addr"`
	IsReserved bool   `yaml:"is_reserved" json:"is_reserved"`
}

// ServiceRole describes a service role assigned to the VM by the orchestrator.
type ServiceRole struct {
	Name   string `yaml:"name"   json:"name"`
	Config string `yaml:"config" json:"config,omitempty"`
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

// NetworkCards returns all virtual NICs from the metadata, or nil if the
// ISO wasn't mounted or carried no NICs.
func (m *ISOMetadata) NetworkCards() []VirtualNetworkCard {
	if m == nil || m.raw == nil {
		return nil
	}
	return m.raw.VirtualNetworkCards
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

// Read parses the metadata file from the ISO mount point and returns an
// ISOMetadata. It tries metadata.yaml, then metadata.json, then
// pc-meta-data.json (the real filename used by the platform) in order.
// If none are present the returned ISOMetadata is non-nil but empty (all
// accessors will return zero values).
func (r *Reader) Read() (*ISOMetadata, error) {
	// Try YAML first.
	yamlPath := filepath.Join(r.mountPath, "metadata.yaml")
	if data, err := os.ReadFile(yamlPath); err == nil {
		var vm VirtualMachineMetadata
		if err := yaml.Unmarshal(data, &vm); err != nil {
			return nil, fmt.Errorf("parsing metadata.yaml: %w", err)
		}
		return &ISOMetadata{raw: &vm}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("reading metadata.yaml: %w", err)
	}

	// Try JSON fallback names.
	for _, name := range []string{"metadata.json", "pc-meta-data.json"} {
		jsonPath := filepath.Join(r.mountPath, name)
		data, err := os.ReadFile(jsonPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("reading %s: %w", name, err)
		}
		var vm VirtualMachineMetadata
		if err := json.Unmarshal(data, &vm); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", name, err)
		}
		return &ISOMetadata{raw: &vm}, nil
	}

	// No metadata file present — return empty metadata (local-only / dev mode).
	return &ISOMetadata{}, nil
}
