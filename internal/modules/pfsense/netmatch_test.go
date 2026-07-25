package pfsense

import (
	"net"
	"testing"

	"github.com/plusclouds/ubuntu-agent/pkg/isoconfig"
)

func mustParseMAC(t *testing.T, s string) net.HardwareAddr {
	t.Helper()
	mac, err := net.ParseMAC(s)
	if err != nil {
		t.Fatalf("ParseMAC(%q): %v", s, err)
	}
	return mac
}

func TestMatchNICsByMAC_MatchesByMACCaseInsensitive(t *testing.T) {
	cards := []isoconfig.VirtualNetworkCard{
		{MACAddr: "6A:E0:4F:84:09:86"}, // uppercase in metadata
		{MACAddr: "12:ca:8c:2b:6d:b5"},
	}
	ifaces := []net.Interface{
		{Name: "vtnet0", HardwareAddr: mustParseMAC(t, "6a:e0:4f:84:09:86")},
		{Name: "vtnet1", HardwareAddr: mustParseMAC(t, "12:ca:8c:2b:6d:b5")},
		{Name: "lo0", HardwareAddr: nil},
	}

	got := matchNICsByMAC(cards, ifaces)
	if len(got) != 2 {
		t.Fatalf("expected 2 matches, got %d: %+v", len(got), got)
	}
	if got[0].IfName != "vtnet0" || got[0].Card.MACAddr != "6A:E0:4F:84:09:86" {
		t.Errorf("match[0]: got %+v", got[0])
	}
	if got[1].IfName != "vtnet1" {
		t.Errorf("match[1]: got %+v", got[1])
	}
}

func TestMatchNICsByMAC_SkipsUnmatchedCards(t *testing.T) {
	cards := []isoconfig.VirtualNetworkCard{
		{MACAddr: "aa:bb:cc:dd:ee:ff"}, // no local interface has this MAC
	}
	ifaces := []net.Interface{
		{Name: "vtnet0", HardwareAddr: mustParseMAC(t, "6a:e0:4f:84:09:86")},
	}

	got := matchNICsByMAC(cards, ifaces)
	if len(got) != 0 {
		t.Fatalf("expected no matches, got %d: %+v", len(got), got)
	}
}

func TestMatchNICsByMAC_NoCards(t *testing.T) {
	ifaces := []net.Interface{
		{Name: "vtnet0", HardwareAddr: mustParseMAC(t, "6a:e0:4f:84:09:86")},
	}
	if got := matchNICsByMAC(nil, ifaces); len(got) != 0 {
		t.Fatalf("expected no matches for nil cards, got %+v", got)
	}
}
