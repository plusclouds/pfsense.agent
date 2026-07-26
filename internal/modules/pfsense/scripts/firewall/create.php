#!/usr/local/bin/php -f
<?php
/*
 * Creates a pfSense firewall (filter) rule via pfSense's own config
 * subsystem and applies it live with filter_configure(), so the rule
 * takes effect immediately instead of waiting on a manual "Apply
 * Changes". Invoked by the PlusClouds agent.
 *
 * The rule is given a "tracker" id using pfSense's own convention
 * (round(microtime(true) * 1000), the same scheme the webConfigurator
 * assigns internally) so later delete.php calls can address it without
 * relying on array position, which shifts as other rules are added or
 * removed.
 *
 * stdin: JSON {interface, action, protocol, source, source_port,
 *              destination, destination_port, description}
 */

require_once("globals.inc");
require_once("config.inc");
require_once("filter.inc");

$input = json_decode(stream_get_contents(STDIN), true);
if (!is_array($input)) {
	fwrite(STDERR, "invalid JSON input\n");
	exit(1);
}

foreach (['interface', 'action', 'source', 'destination'] as $field) {
	if (empty($input[$field])) {
		fwrite(STDERR, "missing required field: {$field}\n");
		exit(1);
	}
}

if (!in_array($input['action'], ['pass', 'block', 'reject'], true)) {
	fwrite(STDERR, "action must be one of: pass, block, reject\n");
	exit(1);
}

// Collapses a caller-supplied "any" / bare IP-or-CIDR / interface alias
// (lan, wan, optN, ...) string into the {any}/{address}/{network} shape
// pfSense's filter rule source/destination fields expect.
function parse_address_spec($spec) {
	$spec = trim((string)$spec);
	if ($spec === '' || strtolower($spec) === 'any') {
		return ['any' => ''];
	}
	$host = explode('/', $spec)[0];
	if (!filter_var($host, FILTER_VALIDATE_IP)) {
		return ['network' => $spec];
	}
	return ['address' => $spec];
}

$tracker = (string) round(microtime(true) * 1000);

$rule = [
	'type'       => $input['action'],
	'interface'  => $input['interface'],
	'ipprotocol' => 'inet',
	'tracker'    => $tracker,
	'descr'      => $input['description'] ?? '',
];

if (!empty($input['protocol']) && strtolower($input['protocol']) !== 'any') {
	$rule['protocol'] = strtolower($input['protocol']);
}

$rule['source'] = parse_address_spec($input['source']);
if (!empty($input['source_port'])) {
	$rule['source']['port'] = str_replace('-', ':', $input['source_port']);
}

$rule['destination'] = parse_address_spec($input['destination']);
if (!empty($input['destination_port'])) {
	$rule['destination']['port'] = str_replace('-', ':', $input['destination_port']);
}

$rules = config_get_path('filter/rule', []);
$rules[] = $rule;
config_set_path('filter/rule', $rules);
write_config("Firewall rule added via PlusClouds agent" . (!empty($input['description']) ? ": {$input['description']}" : ""));
filter_configure();

echo json_encode(['status' => 'ok', 'tracker' => $tracker]);
