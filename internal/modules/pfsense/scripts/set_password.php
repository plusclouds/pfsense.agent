#!/usr/local/bin/php -f
<?php
/*
 * Changes the password of an existing pfSense local user by reusing
 * pfSense's own config/auth subsystem (src/etc/inc/auth.inc), so the
 * hashing scheme, config.xml write, and OS pw-db sync exactly match what
 * the pfSense webConfigurator does. Invoked by the PlusClouds agent.
 *
 * argv[1]: username
 * stdin:   new password (never passed as an argv or env var)
 */

require_once("globals.inc");
require_once("config.inc");
require_once("auth.inc");

if ($argc < 2 || trim($argv[1]) === "") {
	fwrite(STDERR, "usage: set_password.php <username> (password on stdin)\n");
	exit(1);
}

$username = $argv[1];
$password = trim(stream_get_contents(STDIN));

if ($password === "") {
	fwrite(STDERR, "password must not be empty\n");
	exit(1);
}

$index = null;
foreach (config_get_path('system/user', []) as $idx => $u) {
	if ($u['name'] === $username) {
		$index = $idx;
		break;
	}
}

if ($index === null) {
	fwrite(STDERR, "user not found: {$username}\n");
	exit(1);
}

$user_item_config = [
	'idx'  => $index,
	'item' => config_get_path("system/user/{$index}"),
];

local_user_set_password($user_item_config, $password);
write_config("Password changed via PlusClouds agent for user {$username}");
local_user_set($user_item_config['item']);

echo json_encode(['status' => 'ok', 'username' => $username]);
