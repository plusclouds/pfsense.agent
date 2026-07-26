#!/usr/local/bin/php -f
<?php
/*
 * Dumps the live interfaces/gateways/DNS config as JSON so the PlusClouds
 * agent can restore it if the metadata-derived network change can't be
 * verified (e.g. the agent can no longer reach the platform afterward).
 */

require_once("globals.inc");
require_once("config.inc");
require_once("interfaces.inc");

echo json_encode([
	'interfaces' => config_get_path('interfaces', []),
	'gateways'   => config_get_path('gateways/gateway_item', []),
	'defaultgw4' => config_get_path('gateways/defaultgw4', ''),
	'dnsserver'  => config_get_path('system/dnsserver', []),
]);
