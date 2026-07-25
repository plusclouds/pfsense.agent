package isoconfig_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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
		AgentAPIKey:      "SncBECa7ezSovHKCgsgRqN4vomcQuTgRRpAPCtyhAN8DvhfA5xh1SRW1oi4C6ffw",
		VirtualDisks: []isoconfig.VirtualDisk{
			{DiskType: "user", DeviceNumber: 0, TotalDisk: 85899345920},
			{DiskType: "cdrom", DeviceNumber: 3, TotalDisk: 0},
		},
		VirtualNetworkCards: []isoconfig.VirtualNetworkCard{
			{
				DeviceNumber: 0,
				MACAddr:      "7a:9c:c0:d0:ff:bc",
				NetworkName:  "Public Internet",
				Network: isoconfig.Network{
					Name:           "Public Internet",
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
						{ID: 1011, IPAddr: "185.255.172.129/32"},
					},
				},
			},
		},
		ServiceRoles: []isoconfig.ServiceRole{},
		SSHKeys:      []string{},
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
// Reader.Read — JSON
// ---------------------------------------------------------------------------

func TestRead_ParsesAllFields(t *testing.T) {
	dir := t.TempDir()
	writeJSONFile(t, dir, "pc-meta-data.json", sampleMetadata())

	meta, err := isoconfig.NewReader(dir).Read()
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}

	raw := meta.Raw()
	if raw == nil {
		t.Fatal("Raw() should not be nil after reading a valid pc-meta-data.json")
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

func TestRead_InvalidJSON_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pc-meta-data.json"), []byte("{not valid json"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := isoconfig.NewReader(dir).Read()
	if err == nil {
		t.Error("expected parse error for invalid JSON")
	}
}

// TestRead_NumericIPEntryID_Parses is a regression test: the real platform
// sends ip_list.data[].id as a JSON number (e.g. 1010), not a string. This
// must not break parsing of the whole document.
func TestRead_NumericIPEntryID_Parses(t *testing.T) {
	dir := t.TempDir()
	raw := `{
		"hostname": "tester-gw",
		"virtual_machine_id": "34a35003-a9c1-4935-86c4-77b420b04816",
		"agent_api_key": "SncBECa7ezSovHKCgsgRqN4vomcQuTgRRpAPCtyhAN8DvhfA5xh1SRW1oi4C6ffw",
		"virtual_disks": [],
		"virtual_network_cards": [
			{
				"device_number": 1,
				"mac_addr": "12:ca:8c:2b:6d:b5",
				"network_name": "tester-gw-3cac73c72a",
				"network": {
					"name": "tester-gw-3cac73c72a",
					"ip_addr": "10.128.0.0/16",
					"gateway": null,
					"dhcp_server": null,
					"dns_nameservers": ["8.8.4.4/32"],
					"mtu": 1500
				},
				"ip_list": {
					"data": [
						{ "id": 1010, "ip_addr": "10.128.0.1/32", "is_reserved": false }
					]
				}
			}
		],
		"service_roles": []
	}`
	if err := os.WriteFile(filepath.Join(dir, "pc-meta-data.json"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}

	meta, err := isoconfig.NewReader(dir).Read()
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if got := meta.PrimaryIP(); got != "10.128.0.1/32" {
		t.Errorf("PrimaryIP: got %q", got)
	}
	if id := meta.Raw().VirtualNetworkCards[0].IPList.Data[0].ID; id != 1010 {
		t.Errorf("IPEntry.ID: got %d", id)
	}
}

// TestRead_RealisticSample_ParsesAgentSettings reads testdata/pc-meta-data.sample.json,
// a sanitized copy of a real config-drive payload (secrets/identifying values
// replaced with placeholders), and checks that both the VM metadata and the
// "agent" runtime-config block parse correctly end to end.
func TestRead_RealisticSample_ParsesAgentSettings(t *testing.T) {
	meta, err := isoconfig.NewReader("testdata").Read()
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}

	if got := meta.VMID(); got != "00000000-0000-0000-0000-000000000001" {
		t.Errorf("VMID: got %q", got)
	}
	if got := meta.AgentAPIKey(); got != "REDACTED-AGENT-API-KEY" {
		t.Errorf("AgentAPIKey: got %q", got)
	}
	if got := meta.PrimaryIP(); got != "10.128.0.1/32" {
		t.Errorf("PrimaryIP: got %q", got)
	}

	agentSettings := meta.AgentSettings()
	if len(agentSettings) == 0 {
		t.Fatal("AgentSettings() should not be empty")
	}

	var parsed struct {
		NATS struct {
			WebSocketURL string `json:"websocket_url"`
		} `json:"nats"`
		Agent struct {
			AllowedOperations []string `json:"allowed_operations"`
		} `json:"agent"`
		Log struct {
			Level string `json:"level"`
		} `json:"log"`
	}
	if err := json.Unmarshal(agentSettings, &parsed); err != nil {
		t.Fatalf("AgentSettings() did not produce valid JSON: %v", err)
	}
	if parsed.NATS.WebSocketURL != "wss://nats.example.com:443" {
		t.Errorf("agent.nats.websocket_url: got %q", parsed.NATS.WebSocketURL)
	}
	if len(parsed.Agent.AllowedOperations) == 0 {
		t.Error("agent.agent.allowed_operations should not be empty")
	}
	if parsed.Log.Level != "debug" {
		t.Errorf("agent.log.level: got %q", parsed.Log.Level)
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
	writeJSONFile(t, dir, "pc-meta-data.json", sampleMetadata())
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

func TestAccessors_AgentAPIKey(t *testing.T) {
	if got := readSample(t).AgentAPIKey(); got != "SncBECa7ezSovHKCgsgRqN4vomcQuTgRRpAPCtyhAN8DvhfA5xh1SRW1oi4C6ffw" {
		t.Errorf("AgentAPIKey: got %q", got)
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
	writeJSONFile(t, dir, "pc-meta-data.json", m)
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
	writeJSONFile(t, dir, "pc-meta-data.json", m)
	meta, _ := isoconfig.NewReader(dir).Read()
	if got := meta.PrimaryIP(); got != "" {
		t.Errorf("PrimaryIP with no NICs: got %q", got)
	}
}
