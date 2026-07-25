#!/usr/local/bin/php -f
<?php
/*
 * Restores interfaces/gateways/DNS config from a snapshot previously taken
 * by snapshot.php, then brings all interfaces back up with the restored
 * config. Used by the PlusClouds agent when a metadata-derived network
 * change can't be verified (e.g. the agent can no longer reach the
 * platform afterward) and needs to revert.
 *
 * stdin: the JSON snapshot produced by snapshot.php.
 */

require_once("globals.inc");
require_once("config.inc");
require_once("interfaces.inc");

$snapshot = json_decode(stream_get_contents(STDIN), true);
if (!is_array($snapshot)) {
	fwrite(STDERR, "invalid snapshot\n");
	exit(1);
}

config_set_path('interfaces', $snapshot['interfaces'] ?? []);
config_set_path('gateways/gateway_item', $snapshot['gateways'] ?? []);
config_set_path('system/dnsserver', $snapshot['dnsserver'] ?? []);

write_config("Network settings reverted by PlusClouds agent (metadata apply could not be verified)");

interfaces_configure();

echo json_encode(['status' => 'ok']);
