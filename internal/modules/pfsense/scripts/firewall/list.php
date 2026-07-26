#!/usr/local/bin/php -f
<?php
/*
 * Lists pfSense firewall (filter) rules straight from config.xml via
 * pfSense's own config subsystem, so the result always matches what the
 * webConfigurator's Firewall > Rules page would show. Invoked by the
 * PlusClouds agent.
 *
 * No arguments, no stdin.
 */

require_once("globals.inc");
require_once("config.inc");

// Mirrors create.php's address shape: {any} | {network: <alias>} |
// {address: <ip/cidr>} collapses back down to the single string the agent
// sent when creating the rule.
function format_address_spec($addr) {
	if (!is_array($addr)) {
		return '';
	}
	if (array_key_exists('any', $addr)) {
		return 'any';
	}
	if (isset($addr['network'])) {
		return $addr['network'];
	}
	if (isset($addr['address'])) {
		return $addr['address'];
	}
	return '';
}

$rules = [];
foreach (config_get_path('filter/rule', []) as $rule) {
	$source = $rule['source'] ?? [];
	$destination = $rule['destination'] ?? [];
	$rules[] = [
		'tracker'          => (string)($rule['tracker'] ?? ''),
		'interface'        => $rule['interface'] ?? '',
		'action'           => $rule['type'] ?? '',
		'protocol'         => $rule['protocol'] ?? '',
		'source'           => format_address_spec($source),
		'source_port'      => str_replace(':', '-', $source['port'] ?? ''),
		'destination'      => format_address_spec($destination),
		'destination_port' => str_replace(':', '-', $destination['port'] ?? ''),
		'description'      => $rule['descr'] ?? '',
		'disabled'         => array_key_exists('disabled', $rule),
	];
}

echo json_encode(['status' => 'ok', 'rules' => $rules]);
