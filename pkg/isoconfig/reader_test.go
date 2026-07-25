package isoconfig_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/plusclouds/ubuntu-agent/pkg/isoconfig"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func sampleMetadata() isoconfig.VirtualMachineMetadata {
	gw := "185.255.175.254"
	return isoconfig.VirtualMachineMetadata{
		Hostname:         "enelsa-s-r-v",
		Username:         "root",
		Password:         "s3cr3t",
		VirtualMachineID: "016c6a79-dbbe-4284-9ea0-75645ede8ca3",
		VirtualDisks: []isoconfig.VirtualDisk{
			{DiskType: "user", DeviceNumber: 0, TotalDisk: 85899345920},
			{DiskType: "cdrom", DeviceNumber: 3, TotalDisk: 0},
		},
		VirtualNetworkCards: []isoconfig.VirtualNetworkCard{
			{
				DeviceNumber: 0,
				MACAddr:      "7a:9c:c0:d0:ff:bc",
				Network: isoconfig.Network{
					IPAddr:         "185.255.172.0/22",
					Gateway:        &gw,
					Subnet:         "22",
					Netmask:        "255.255.252.0",
					NetworkAddress: "185.255.172.0",
					DNSNameservers: []string{"8.8.4.4/32", "8.8.8.8/32"},
					MTU:            1500,
				},
				IPList: isoconfig.DataList[isoconfig.IPEntry]{
					Data: []isoconfig.IPEntry{
						{
							ID:     1010,
							IPAddr: "185.255.172.129/32",
						},
					},
				},
			},
		},
		ServiceRoles:  []isoconfig.ServiceRole{},
		SSHPublicKeys: []string{},
	}
}

func writeYAML(t *testing.T, dir, filename string, v interface{}) {
	t.Helper()
	data, err := yaml.Marshal(v)
	if err != nil {
		t.Fatalf("yaml.Marshal %s: %v", filename, err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), data, 0600); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}
}

func writeJSONFile(t *testing.T, dir, filename string, v interface{}) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal %s: %v", filename, err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), data, 0600); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}
}

// ---------------------------------------------------------------------------
// Reader.Read — YAML
// ---------------------------------------------------------------------------

func TestRead_YAML_ParsesAllFields(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "metadata.yaml", sampleMetadata())

	meta, err := isoconfig.NewReader(dir).Read()
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}

	raw := meta.Raw()
	if raw == nil {
		t.Fatal("Raw() should not be nil after reading a valid metadata.yaml")
	}
	if raw.Hostname != "enelsa-s-r-v" {
		t.Errorf("Hostname: got %q", raw.Hostname)
	}
	if raw.VirtualMachineID != "016c6a79-dbbe-4284-9ea0-75645ede8ca3" {
		t.Errorf("VirtualMachineID: got %q", raw.VirtualMachineID)
	}
	if len(raw.VirtualDisks) != 2 {
		t.Errorf("VirtualDisks: expected 2, got %d", len(raw.VirtualDisks))
	}
	if len(raw.VirtualNetworkCards) != 1 {
		t.Errorf("VirtualNetworkCards: expected 1, got %d", len(raw.VirtualNetworkCards))
	}
	nic := raw.VirtualNetworkCards[0]
	if nic.MACAddr != "7a:9c:c0:d0:ff:bc" {
		t.Errorf("MACAddr: got %q", nic.MACAddr)
	}
	if len(nic.IPList.Data) != 1 {
		t.Errorf("IPList: expected 1, got %d", len(nic.IPList.Data))
	}
	if nic.IPList.Data[0].IPAddr != "185.255.172.129/32" {
		t.Errorf("IPAddr: got %q", nic.IPList.Data[0].IPAddr)
	}
}

func TestRead_YAML_InvalidYAML_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "metadata.yaml"), []byte(":\tinvalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := isoconfig.NewReader(dir).Read()
	if err == nil {
		t.Error("expected parse error for invalid YAML")
	}
}

// ---------------------------------------------------------------------------
// Reader.Read — JSON fallback
// ---------------------------------------------------------------------------

func TestRead_JSONFallback(t *testing.T) {
	dir := t.TempDir()
	writeJSONFile(t, dir, "metadata.json", sampleMetadata())

	meta, err := isoconfig.NewReader(dir).Read()
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if meta.VMID() != "016c6a79-dbbe-4284-9ea0-75645ede8ca3" {
		t.Errorf("VMID: got %q", meta.VMID())
	}
}

func TestRead_YAMLTakesPrecedenceOverJSON(t *testing.T) {
	dir := t.TempDir()
	yaml := sampleMetadata()
	json := sampleMetadata()
	json.Hostname = "from-json"
	writeYAML(t, dir, "metadata.yaml", yaml)
	writeJSONFile(t, dir, "metadata.json", json)

	meta, err := isoconfig.NewReader(dir).Read()
	if err != nil {
		t.Fatal(err)
	}
	if meta.Hostname() == "from-json" {
		t.Error("expected YAML to take precedence over JSON fallback")
	}
	if meta.Hostname() != "enelsa-s-r-v" {
		t.Errorf("Hostname: got %q", meta.Hostname())
	}
}

func TestRead_NoFiles_ReturnsEmptyMeta(t *testing.T) {
	dir := t.TempDir()
	meta, err := isoconfig.NewReader(dir).Read()
	if err != nil {
		t.Fatalf("Read() on empty dir should not error: %v", err)
	}
	if meta.Raw() != nil {
		t.Error("Raw() should be nil when no metadata file is present")
	}
}

func TestRead_NonExistentMountPath_ReturnsEmptyMeta(t *testing.T) {
	meta, err := isoconfig.NewReader("/nonexistent/path/xyz").Read()
	if err != nil {
		t.Fatalf("Read() on missing mount path should not error: %v", err)
	}
	if meta.Raw() != nil {
		t.Error("expected nil Raw() for missing mount path")
	}
}

// ---------------------------------------------------------------------------
// ISOMetadata accessors
// ---------------------------------------------------------------------------

func readSample(t *testing.T) *isoconfig.ISOMetadata {
	t.Helper()
	dir := t.TempDir()
	writeYAML(t, dir, "metadata.yaml", sampleMetadata())
	meta, err := isoconfig.NewReader(dir).Read()
	if err != nil {
		t.Fatal(err)
	}
	return meta
}

func TestAccessors_VMID(t *testing.T) {
	if got := readSample(t).VMID(); got != "016c6a79-dbbe-4284-9ea0-75645ede8ca3" {
		t.Errorf("VMID: got %q", got)
	}
}

func TestAccessors_Hostname(t *testing.T) {
	if got := readSample(t).Hostname(); got != "enelsa-s-r-v" {
		t.Errorf("Hostname: got %q", got)
	}
}

func TestAccessors_Password(t *testing.T) {
	if got := readSample(t).Password(); got != "s3cr3t" {
		t.Errorf("Password: got %q", got)
	}
}

func TestAccessors_APIKey_EqualsPassword(t *testing.T) {
	m := readSample(t)
	if m.APIKey() != m.Password() {
		t.Errorf("APIKey should equal Password: %q != %q", m.APIKey(), m.Password())
	}
}

func TestAccessors_PrimaryIP(t *testing.T) {
	if got := readSample(t).PrimaryIP(); got != "185.255.172.129/32" {
		t.Errorf("PrimaryIP: got %q", got)
	}
}

func TestAccessors_Gateway(t *testing.T) {
	if got := readSample(t).Gateway(); got != "185.255.175.254" {
		t.Errorf("Gateway: got %q", got)
	}
}

func TestAccessors_DNSNameservers(t *testing.T) {
	dns := readSample(t).DNSNameservers()
	if len(dns) != 2 {
		t.Fatalf("DNSNameservers: expected 2, got %d", len(dns))
	}
	if dns[0] != "8.8.4.4/32" || dns[1] != "8.8.8.8/32" {
		t.Errorf("DNSNameservers: got %v", dns)
	}
}

func TestAccessors_GatewayNull_ReturnsEmpty(t *testing.T) {
	m := sampleMetadata()
	m.VirtualNetworkCards[0].Network.Gateway = nil
	dir := t.TempDir()
	writeYAML(t, dir, "metadata.yaml", m)
	meta, _ := isoconfig.NewReader(dir).Read()
	if got := meta.Gateway(); got != "" {
		t.Errorf("Gateway with null: expected empty, got %q", got)
	}
}

func TestAccessors_NilRaw_AllReturnZero(t *testing.T) {
	meta := &isoconfig.ISOMetadata{}
	if got := meta.VMID(); got != "" {
		t.Errorf("VMID: got %q", got)
	}
	if got := meta.Hostname(); got != "" {
		t.Errorf("Hostname: got %q", got)
	}
	if got := meta.Password(); got != "" {
		t.Errorf("Password: got %q", got)
	}
	if got := meta.APIKey(); got != "" {
		t.Errorf("APIKey: got %q", got)
	}
	if got := meta.PrimaryIP(); got != "" {
		t.Errorf("PrimaryIP: got %q", got)
	}
	if got := meta.Gateway(); got != "" {
		t.Errorf("Gateway: got %q", got)
	}
	if got := meta.TenantID(); got != "" {
		t.Errorf("TenantID: got %q", got)
	}
	if got := meta.AgentToken(); got != "" {
		t.Errorf("AgentToken: got %q", got)
	}
	if got := meta.ControlPlaneURL(); got != "" {
		t.Errorf("ControlPlaneURL: got %q", got)
	}
}

func TestAccessors_NoPrimaryIP_WhenNoNICs(t *testing.T) {
	m := sampleMetadata()
	m.VirtualNetworkCards = nil
	dir := t.TempDir()
	writeYAML(t, dir, "metadata.yaml", m)
	meta, _ := isoconfig.NewReader(dir).Read()
	if got := meta.PrimaryIP(); got != "" {
		t.Errorf("PrimaryIP with no NICs: got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Real-world regression — the literal pc-meta-data.json shape the platform
// actually produces. Round-tripping sampleMetadata() through the struct's
// own tags can never catch a tag mismatch; this fixture is copy-pasted from
// a real config-drive file instead.
// ---------------------------------------------------------------------------

const realPCMetaDataJSON = `{
    "hostname": "tester-gw-3cac73c72a-v-d-c-firewall",
    "username": "root",
    "password": "pep0movt!",
    "virtual_machine_id": "34a35003-a9c1-4935-86c4-77b420b04816",
    "agent_api_key": "SncBECa7ezSovHKCgsgRqN4vomcQuTgRRpAPCtyhAN8DvhfA5xh1SRW1oi4C6ffw",
    "virtual_disks": [
        {"disk_type": "user", "device_number": 0, "total_disk": 21474836480},
        {"disk_type": "cdrom", "device_number": 3, "total_disk": 12048384}
    ],
    "virtual_network_cards": [
        {
            "device_number": 1,
            "mac_addr": "12:ca:8c:2b:6d:b5",
            "network_name": "tester-gw-3cac73c72a",
            "network": {
                "name": "tester-gw-3cac73c72a",
                "ip_addr": "10.128.0.0/16",
                "ip_range_start": "10.128.0.10/32",
                "ip_range_end": "10.128.255.254/32",
                "gateway": null,
                "subnet": "16",
                "netmask": "255.255.0.0",
                "network": "10.128.0.0",
                "dhcp_server": null,
                "dns_nameservers": ["8.8.4.4/32"],
                "mtu": 1500
            },
            "ip_list": {
                "data": [
                    {"id": 1010, "ip_addr": "10.128.0.1/32", "is_reserved": false}
                ]
            }
        },
        {
            "device_number": 0,
            "mac_addr": "6a:e0:4f:84:09:86",
            "network_name": "Public Internet",
            "network": {
                "name": "Public Internet",
                "ip_addr": "185.255.172.0/22",
                "ip_range_start": "185.255.172.2/32",
                "ip_range_end": "185.255.175.254/32",
                "gateway": null,
                "subnet": "22",
                "netmask": "255.255.252.0",
                "network": "185.255.172.0",
                "dhcp_server": "185.255.175.0/32",
                "dns_nameservers": ["8.8.4.4/32", "8.8.8.8/32"],
                "mtu": 1500
            },
            "ip_list": {
                "data": [
                    {"id": 1011, "ip_addr": "185.255.172.16/32", "is_reserved": false}
                ]
            }
        }
    ],
    "service_roles": [],
    "ssh_keys": [],
    "env_vars": [],
    "tokens": []
}`

func TestRead_RealPCMetaDataJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pc-meta-data.json"), []byte(realPCMetaDataJSON), 0600); err != nil {
		t.Fatal(err)
	}

	meta, err := isoconfig.NewReader(dir).Read()
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}

	if got := meta.Hostname(); got != "tester-gw-3cac73c72a-v-d-c-firewall" {
		t.Errorf("Hostname: got %q", got)
	}
	if got := meta.Username(); got != "root" {
		t.Errorf("Username: got %q", got)
	}
	if got := meta.Password(); got != "pep0movt!" {
		t.Errorf("Password: got %q", got)
	}
	if got := meta.AgentAPIKey(); got == "" {
		t.Error("AgentAPIKey: got empty")
	}

	cards := meta.NetworkCards()
	if len(cards) != 2 {
		t.Fatalf("NetworkCards: expected 2, got %d", len(cards))
	}

	lan := cards[0]
	if lan.MACAddr != "12:ca:8c:2b:6d:b5" {
		t.Errorf("lan MACAddr: got %q", lan.MACAddr)
	}
	if lan.Network.Gateway != nil {
		t.Errorf("lan gateway: expected nil, got %q", *lan.Network.Gateway)
	}
	if lan.Network.Subnet != "16" || lan.Network.Netmask != "255.255.0.0" {
		t.Errorf("lan subnet/netmask: got %q/%q", lan.Network.Subnet, lan.Network.Netmask)
	}
	if len(lan.IPList.Data) != 1 || lan.IPList.Data[0].ID != 1010 || lan.IPList.Data[0].IPAddr != "10.128.0.1/32" {
		t.Errorf("lan ip_list: got %+v", lan.IPList.Data)
	}

	wan := cards[1]
	if wan.MACAddr != "6a:e0:4f:84:09:86" {
		t.Errorf("wan MACAddr: got %q", wan.MACAddr)
	}
	if wan.Network.Gateway != nil {
		t.Errorf("wan gateway: expected nil, got %q", *wan.Network.Gateway)
	}
	if wan.Network.Subnet != "22" {
		t.Errorf("wan subnet: got %q", wan.Network.Subnet)
	}
	if len(wan.IPList.Data) != 1 || wan.IPList.Data[0].IPAddr != "185.255.172.16/32" {
		t.Errorf("wan ip_list: got %+v", wan.IPList.Data)
	}
}
