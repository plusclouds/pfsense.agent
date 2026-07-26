#!/usr/local/bin/php -f
<?php
/*
 * Deletes a pfSense firewall (filter) rule matched by the "tracker" id
 * assigned at creation time (see create.php) and applies the change live
 * with filter_configure(). Invoked by the PlusClouds agent.
 *
 * argv[1]: tracker
 */

require_once("globals.inc");
require_once("config.inc");
require_once("filter.inc");

if ($argc < 2 || trim($argv[1]) === "") {
	fwrite(STDERR, "usage: delete.php <tracker>\n");
	exit(1);
}

$tracker = $argv[1];
$rules = config_get_path('filter/rule', []);

$index = null;
foreach ($rules as $idx => $rule) {
	if ((string)($rule['tracker'] ?? '') === $tracker) {
		$index = $idx;
		break;
	}
}

if ($index === null) {
	fwrite(STDERR, "rule not found: {$tracker}\n");
	exit(1);
}

unset($rules[$index]);
config_set_path('filter/rule', array_values($rules));
write_config("Firewall rule {$tracker} removed via PlusClouds agent");
filter_configure();

echo json_encode(['status' => 'ok', 'tracker' => $tracker]);
