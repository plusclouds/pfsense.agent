//go:build freebsd

package pfsense

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/plusclouds/ubuntu-agent/internal/executor"
	"github.com/plusclouds/ubuntu-agent/pkg/isoconfig"
)

// setPasswordScript is pfSense's PHP config/auth subsystem glue for changing
// a local user's password. It is embedded in the binary (rather than
// installed as a separate file) so it survives pfSense firmware upgrades
// that may wipe custom files outside packaged locations.
//
//go:embed scripts/set_password.php
var setPasswordScript string

//go:embed scripts/set_default_password.php
var setDefaultPasswordScript string

//go:embed scripts/netconfig/snapshot.php
var netconfigSnapshotScript string

//go:embed scripts/netconfig/apply.php
var netconfigApplyScript string

//go:embed scripts/netconfig/restore.php
var netconfigRestoreScript string

const phpBinary = "/usr/local/bin/php"

// freebsdManager runs pfSense operations by shelling out to pfSense's own
// PHP includes, so hashing/config.xml/pw-db behavior always matches the
// webConfigurator for whatever pfSense version is installed.
type freebsdManager struct {
	exec   *executor.Executor
	logger *zap.Logger
}

// New returns the real pfSense/FreeBSD Manager.
func New(exec *executor.Executor, logger *zap.Logger) Manager {
	return &freebsdManager{exec: exec, logger: logger}
}

type setPasswordScriptOutput struct {
	Status   string `json:"status"`
	Username string `json:"username"`
}

// runEmbeddedPHP writes an embedded script to a private temp file and runs
// it via the pfSense php binary, cleaning up the temp file afterward.
func runEmbeddedPHP(ctx context.Context, exec *executor.Executor, script, scriptName string, stdin []byte, args ...string) (stdout, stderr string, err error) {
	dir, err := os.MkdirTemp("", "plusclouds-pfsense-")
	if err != nil {
		return "", "", fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	scriptPath := filepath.Join(dir, scriptName)
	if err := os.WriteFile(scriptPath, []byte(script), 0600); err != nil {
		return "", "", fmt.Errorf("writing script: %w", err)
	}

	fullArgs := append([]string{"-f", scriptPath}, args...)
	return exec.ExecuteWithStdin(ctx, stdin, phpBinary, fullArgs...)
}

// SetPassword runs the embedded script with the username as an argument and
// the password piped over stdin — the password is never placed in argv or
// the environment, where it would be visible to other users via ps/procfs.
func (m *freebsdManager) SetPassword(ctx context.Context, username, password string) (*SetPasswordResult, error) {
	if username == "" {
		return nil, fmt.Errorf("username must not be empty")
	}
	if password == "" {
		return nil, fmt.Errorf("password must not be empty")
	}

	stdout, stderr, err := runEmbeddedPHP(ctx, m.exec, setPasswordScript, "set_password.php", []byte(password), username)
	if err != nil {
		msg := stderr
		if msg == "" {
			msg = err.Error()
		}
		return &SetPasswordResult{Username: username, Success: false, Message: msg}, nil
	}

	var out setPasswordScriptOutput
	if jsonErr := json.Unmarshal([]byte(stdout), &out); jsonErr != nil {
		return nil, fmt.Errorf("parsing script output %q: %w", stdout, jsonErr)
	}
	if out.Status != "ok" {
		return &SetPasswordResult{Username: username, Success: false, Message: "unexpected script status: " + out.Status}, nil
	}

	return &SetPasswordResult{Username: username, Success: true, Message: "password updated"}, nil
}

// SetDefaultUserPassword runs the embedded script that matches pfSense's
// default superuser account by uid 0 rather than by name, since the
// account's display name (usually "admin") isn't reliable to guess from
// generic cross-platform VM metadata and can be renamed by the box owner.
func (m *freebsdManager) SetDefaultUserPassword(ctx context.Context, password string) (*SetPasswordResult, error) {
	if password == "" {
		return nil, fmt.Errorf("password must not be empty")
	}

	stdout, stderr, err := runEmbeddedPHP(ctx, m.exec, setDefaultPasswordScript, "set_default_password.php", []byte(password))
	if err != nil {
		msg := stderr
		if msg == "" {
			msg = err.Error()
		}
		return &SetPasswordResult{Success: false, Message: msg}, nil
	}

	var out setPasswordScriptOutput
	if jsonErr := json.Unmarshal([]byte(stdout), &out); jsonErr != nil {
		return nil, fmt.Errorf("parsing script output %q: %w", stdout, jsonErr)
	}
	if out.Status != "ok" {
		return &SetPasswordResult{Success: false, Message: "unexpected script status: " + out.Status}, nil
	}

	return &SetPasswordResult{Username: out.Username, Success: true, Message: "password updated"}, nil
}

// interfacePlanEntry is one entry of the JSON plan sent to netconfig/apply.php.
type interfacePlanEntry struct {
	IfName  string   `json:"ifname"`
	IPAddr  string   `json:"ipaddr"`
	Subnet  string   `json:"subnet"`
	Gateway string   `json:"gateway,omitempty"`
	DNS     []string `json:"dns,omitempty"`
	MTU     int      `json:"mtu,omitempty"`
}

type applyPlan struct {
	Interfaces []interfacePlanEntry `json:"interfaces"`
}

type applyScriptOutput struct {
	Status     string `json:"status"`
	Interfaces []struct {
		IfName   string `json:"ifname"`
		Logical  string `json:"logical,omitempty"`
		Applied  bool   `json:"applied"`
		Assigned bool   `json:"assigned,omitempty"`
		Message  string `json:"message,omitempty"`
	} `json:"interfaces"`
}

type restoreScriptOutput struct {
	Status string `json:"status"`
}

// stripCIDRSuffix removes a trailing "/NN" from an address string. The
// metadata represents single addresses as "/32" CIDRs; pfSense wants bare
// addresses for ipaddr/gateway/dns fields.
func stripCIDRSuffix(s string) string {
	if i := strings.IndexByte(s, '/'); i >= 0 {
		return s[:i]
	}
	return s
}

// ApplyBootNetworkConfig matches metadata NICs to local interfaces by MAC,
// snapshots the live config, and applies static addressing to every matched,
// resolvable NIC via the embedded netconfig scripts (which also handle the
// lan-only auto-assignment described on the Manager interface). NICs with
// no local MAC match, or with no usable IP/subnet in the metadata, are
// reported as unapplied rather than causing the whole call to fail.
func (m *freebsdManager) ApplyBootNetworkConfig(ctx context.Context, cards []isoconfig.VirtualNetworkCard) (*BootNetworkResult, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("listing local network interfaces: %w", err)
	}
	matched := matchNICsByMAC(cards, ifaces)
	matchedByMAC := make(map[string]matchedNIC, len(matched))
	for _, mn := range matched {
		matchedByMAC[normalizeMAC(mn.Card.MACAddr)] = mn
	}

	results := make([]InterfaceApplyResult, 0, len(cards))
	var plan applyPlan
	for _, card := range cards {
		mn, ok := matchedByMAC[normalizeMAC(card.MACAddr)]
		if !ok {
			m.logger.Warn("boot network config: no local interface matches NIC MAC", zap.String("mac_addr", card.MACAddr))
			results = append(results, InterfaceApplyResult{
				MACAddr: card.MACAddr,
				Message: "no local network interface with this MAC address",
			})
			continue
		}
		if len(card.IPList.Data) == 0 || card.Network.Subnet == "" {
			results = append(results, InterfaceApplyResult{
				MACAddr: card.MACAddr,
				IfName:  mn.IfName,
				Message: "no static IP/subnet in metadata for this NIC",
			})
			continue
		}

		entry := interfacePlanEntry{
			IfName: mn.IfName,
			IPAddr: stripCIDRSuffix(card.IPList.Data[0].IPAddr),
			Subnet: card.Network.Subnet,
			MTU:    card.Network.MTU,
		}
		if card.Network.Gateway != nil {
			entry.Gateway = stripCIDRSuffix(*card.Network.Gateway)
		}
		for _, dns := range card.Network.DNSNameservers {
			entry.DNS = append(entry.DNS, stripCIDRSuffix(dns))
		}
		plan.Interfaces = append(plan.Interfaces, entry)
	}

	if len(plan.Interfaces) == 0 {
		return &BootNetworkResult{Interfaces: results}, nil
	}

	snapStdout, snapStderr, err := runEmbeddedPHP(ctx, m.exec, netconfigSnapshotScript, "snapshot.php", nil)
	if err != nil {
		return nil, fmt.Errorf("taking network config snapshot: %w (%s)", err, snapStderr)
	}
	snapshot := json.RawMessage(append([]byte(nil), snapStdout...))

	planJSON, err := json.Marshal(plan)
	if err != nil {
		return nil, fmt.Errorf("marshalling network plan: %w", err)
	}

	applyStdout, applyStderr, err := runEmbeddedPHP(ctx, m.exec, netconfigApplyScript, "apply.php", planJSON)
	if err != nil {
		msg := applyStderr
		if msg == "" {
			msg = err.Error()
		}
		results = append(results, InterfaceApplyResult{Message: "apply script failed: " + msg})
		return &BootNetworkResult{Snapshot: snapshot, Interfaces: results}, nil
	}

	var out applyScriptOutput
	if jsonErr := json.Unmarshal([]byte(applyStdout), &out); jsonErr != nil {
		return nil, fmt.Errorf("parsing apply script output %q: %w", applyStdout, jsonErr)
	}
	for _, r := range out.Interfaces {
		results = append(results, InterfaceApplyResult{
			IfName:   r.IfName,
			Logical:  r.Logical,
			Applied:  r.Applied,
			Assigned: r.Assigned,
			Message:  r.Message,
		})
	}

	m.logger.Info("boot network config applied", zap.Int("interfaces_applied", len(out.Interfaces)))
	return &BootNetworkResult{Snapshot: snapshot, Interfaces: results}, nil
}

// Revert restores interfaces/gateways/DNS config from a snapshot previously
// returned by ApplyBootNetworkConfig.
func (m *freebsdManager) Revert(ctx context.Context, snapshot json.RawMessage) error {
	if len(snapshot) == 0 {
		return fmt.Errorf("empty snapshot")
	}
	stdout, stderr, err := runEmbeddedPHP(ctx, m.exec, netconfigRestoreScript, "restore.php", snapshot)
	if err != nil {
		return fmt.Errorf("restore script failed: %w (%s)", err, stderr)
	}
	var out restoreScriptOutput
	if jsonErr := json.Unmarshal([]byte(stdout), &out); jsonErr != nil {
		return fmt.Errorf("parsing restore script output %q: %w", stdout, jsonErr)
	}
	if out.Status != "ok" {
		return fmt.Errorf("unexpected restore script status: %s", out.Status)
	}
	return nil
}
