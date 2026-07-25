package pfsense

import (
	"net"
	"strings"

	"github.com/plusclouds/ubuntu-agent/pkg/isoconfig"
)

// matchedNIC pairs a metadata NIC with the local network interface whose
// MAC address matches it.
type matchedNIC struct {
	Card   isoconfig.VirtualNetworkCard
	IfName string
}

// matchNICsByMAC matches metadata NICs to local interfaces by MAC address
// (case-insensitive). Cards with no matching local interface are omitted —
// callers report them as skipped. Takes the interface list as an argument
// (rather than calling net.Interfaces() itself) so it's testable without
// real hardware.
func matchNICsByMAC(cards []isoconfig.VirtualNetworkCard, ifaces []net.Interface) []matchedNIC {
	byMAC := make(map[string]string, len(ifaces))
	for _, iface := range ifaces {
		mac := normalizeMAC(iface.HardwareAddr.String())
		if mac == "" {
			continue
		}
		byMAC[mac] = iface.Name
	}

	var matched []matchedNIC
	for _, card := range cards {
		ifname, ok := byMAC[normalizeMAC(card.MACAddr)]
		if !ok {
			continue
		}
		matched = append(matched, matchedNIC{Card: card, IfName: ifname})
	}
	return matched
}

func normalizeMAC(mac string) string {
	return strings.ToLower(strings.TrimSpace(mac))
}
