#!/usr/local/bin/php -f
<?php
/*
 * Lists DHCP leases (dynamic and static) via pfSense's own
 * system_get_dhcpleases() — the same function that backs the
 * webConfigurator's Status > DHCP Leases page, so results match
 * regardless of whether the box runs the ISC dhcpd or Kea backend.
 * Invoked by the PlusClouds agent.
 *
 * No arguments, no stdin.
 */

require_once("globals.inc");
require_once("config.inc");
require_once("system.inc");

$data = system_get_dhcpleases();

$leases = [];
foreach ($data['lease'] ?? [] as $lease) {
	$leases[] = [
		'ip'          => $lease['ip'] ?? '',
		'mac'         => $lease['mac'] ?? '',
		'hostname'    => $lease['hostname'] ?? '',
		'description' => $lease['descr'] ?? '',
		'interface'   => $lease['if'] ?? '',
		'type'        => $lease['type'] ?? '',
		'state'       => $lease['act'] ?? '',
		'online'      => $lease['online'] ?? '',
		'starts'      => $lease['starts'] ?? '',
		'ends'        => $lease['ends'] ?? '',
	];
}

echo json_encode(['status' => 'ok', 'leases' => $leases]);
