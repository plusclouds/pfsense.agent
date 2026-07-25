#!/usr/local/bin/php -f
<?php
/*
 * Applies static IPv4 addressing (and optional gateway/DNS/MTU) to pfSense
 * interfaces. The PlusClouds agent resolves which physical NIC (ifname)
 * each plan entry targets by matching MAC addresses from provisioning
 * metadata before invoking this script; interface_configure() brings each
 * changed interface up live so no reboot is required.
 *
 * If a NIC's ifname isn't yet assigned to any pfSense interface role, it is
 * auto-assigned as "lan" — but ONLY if lan doesn't already exist. wan and
 * an existing lan are never reassigned or touched, and at most one NIC per
 * run is assigned this way (first unassigned NIC in the plan wins; a
 * second one is left unassigned and reported as such). Full interface
 * assignment (arbitrary opt roles, multi-NIC topologies) is still out of
 * scope — this covers only the common two-NIC wan+lan default.
 *
 * stdin: JSON {"interfaces":[{"ifname","ipaddr","subnet","gateway","dns":[],"mtu"}]}
 */

require_once("globals.inc");
require_once("config.inc");
require_once("interfaces.inc");

$input = json_decode(stream_get_contents(STDIN), true);
if (!is_array($input) || !is_array($input['interfaces'] ?? null)) {
	fwrite(STDERR, "invalid input\n");
	exit(1);
}

$results = [];
$all_dns = [];
$logicals_to_apply = [];

foreach ($input['interfaces'] as $plan) {
	$ifname = $plan['ifname'] ?? '';
	$result = ['ifname' => $ifname, 'applied' => false];

	$logical = null;
	$lan_exists = false;
	foreach (config_get_path('interfaces', []) as $name => $cfg) {
		if ($name === 'lan') {
			$lan_exists = true;
		}
		if (($cfg['if'] ?? null) === $ifname) {
			$logical = $name;
			break;
		}
	}

	if ($logical === null) {
		if ($lan_exists) {
			$result['message'] = "no pfSense interface assigned to {$ifname}";
			$results[] = $result;
			continue;
		}
		// Auto-assign as lan — config_set_path mutates the in-memory
		// config immediately, so the next iteration's fresh
		// config_get_path('interfaces', []) call will see lan as existing
		// and won't try to assign a second NIC this way.
		config_set_path('interfaces/lan', [
			'if'     => $ifname,
			'descr'  => 'LAN',
			'enable' => true,
		]);
		$logical = 'lan';
		$result['assigned'] = true;
	}

	config_set_path("interfaces/{$logical}/ipaddr", $plan['ipaddr']);
	config_set_path("interfaces/{$logical}/subnet", $plan['subnet']);

	if (!empty($plan['mtu'])) {
		config_set_path("interfaces/{$logical}/mtu", $plan['mtu']);
	}

	if (!empty($plan['gateway'])) {
		$gwname = strtoupper($logical) . "_PCGW";
		$existing_idx = null;
		foreach (config_get_path('gateways/gateway_item', []) as $idx => $gw) {
			if (($gw['name'] ?? null) === $gwname) {
				$existing_idx = $idx;
				break;
			}
		}
		$gw_item = [
			'interface'  => $logical,
			'gateway'    => $plan['gateway'],
			'name'       => $gwname,
			'weight'     => 1,
			'ipprotocol' => 'inet',
			'descr'      => 'Set by PlusClouds agent from provisioning metadata',
		];
		if ($logical === 'wan') {
			$gw_item['defaultgw'] = true;
		}
		if ($existing_idx !== null) {
			config_set_path("gateways/gateway_item/{$existing_idx}", $gw_item);
		} else {
			$next_idx = count(config_get_path('gateways/gateway_item', []));
			config_set_path("gateways/gateway_item/{$next_idx}", $gw_item);
		}
		config_set_path("interfaces/{$logical}/gateway", $gwname);
	} else {
		config_del_path("interfaces/{$logical}/gateway");
	}

	foreach (($plan['dns'] ?? []) as $dns) {
		if (!empty($dns) && !in_array($dns, $all_dns, true)) {
			$all_dns[] = $dns;
		}
	}

	$logicals_to_apply[] = $logical;
	$result['logical'] = $logical;
	$result['applied'] = true;
	$results[] = $result;
}

if (!empty($all_dns)) {
	config_set_path('system/dnsserver', $all_dns);
}

write_config("Network settings applied via PlusClouds agent from provisioning metadata");

foreach (array_unique($logicals_to_apply) as $logical) {
	interface_configure($logical, true);
}

echo json_encode(['status' => 'ok', 'interfaces' => $results]);
