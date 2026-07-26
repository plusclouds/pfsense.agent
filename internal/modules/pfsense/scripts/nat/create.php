#!/usr/local/bin/php -f
<?php
/*
 * Creates a pfSense NAT port-forward rule via pfSense's own config
 * subsystem and applies it live with filter_configure() (NAT rules are
 * applied by the same filter reload as regular firewall rules). Invoked
 * by the PlusClouds agent.
 *
 * No matching "allow" filter rule is auto-created (associated-rule-id is
 * left unset) — pfSense's own default rules already pass traffic on most
 * interfaces, and auto-generating a paired filter rule here would need
 * its own tracker/delete lifecycle in lockstep with this one, which
 * wasn't required for this pass.
 *
 * The rule is given a "tracker" id using pfSense's own convention
 * (round(microtime(true) * 1000)), matching create.php in ../firewall/,
 * so later delete.php calls can address it without relying on array
 * position.
 *
 * stdin: JSON {interface, protocol, destination, destination_port,
 *              target_ip, target_port, source, source_port, description}
 */

require_once("globals.inc");
require_once("config.inc");
require_once("filter.inc");

$input = json_decode(stream_get_contents(STDIN), true);
if (!is_array($input)) {
	fwrite(STDERR, "invalid JSON input\n");
	exit(1);
}

foreach (['interface', 'protocol', 'destination_port', 'target_ip', 'target_port'] as $field) {
	if (empty($input[$field])) {
		fwrite(STDERR, "missing required field: {$field}\n");
		exit(1);
	}
}

if (!in_array(strtolower($input['protocol']), ['tcp', 'udp', 'tcp/udp'], true)) {
	fwrite(STDERR, "protocol must be one of: tcp, udp, tcp/udp\n");
	exit(1);
}

// Collapses a caller-supplied "any" / bare IP-or-CIDR / interface alias
// (lan, wan, optN, ...) string into the {any}/{address}/{network} shape
// pfSense's NAT rule source/destination fields expect.
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
	'interface'  => $input['interface'],
	'ipprotocol' => 'inet',
	'protocol'   => strtolower($input['protocol']),
	'tracker'    => $tracker,
	'target'     => $input['target_ip'],
	'local-port' => str_replace('-', ':', $input['target_port']),
	'descr'      => $input['description'] ?? '',
];

$rule['source'] = parse_address_spec($input['source'] ?? 'any');
if (!empty($input['source_port'])) {
	$rule['source']['port'] = str_replace('-', ':', $input['source_port']);
}

// Default destination to this interface's own address (pfSense's "<if>ip"
// shorthand) — the common port-forward case of forwarding traffic aimed
// at the WAN's own IP, and the same default the webConfigurator's
// port-forward wizard uses when no destination is picked.
$destination_spec = $input['destination'] ?? ($input['interface'] . 'ip');
$rule['destination'] = parse_address_spec($destination_spec);
$rule['destination']['port'] = str_replace('-', ':', $input['destination_port']);

$rules = config_get_path('nat/rule', []);
$rules[] = $rule;
config_set_path('nat/rule', $rules);
write_config("NAT port-forward added via PlusClouds agent" . (!empty($input['description']) ? ": {$input['description']}" : ""));
filter_configure();

echo json_encode(['status' => 'ok', 'tracker' => $tracker]);
