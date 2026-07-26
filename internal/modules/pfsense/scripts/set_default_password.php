#!/usr/local/bin/php -f
<?php
/*
 * Changes the password of pfSense's default superuser account, matched by
 * uid 0 rather than by name — pfSense ships this account named "admin",
 * but it can be renamed, so a name guessed from generic cross-platform VM
 * metadata (e.g. "root") isn't reliable. Reuses pfSense's own config/auth
 * subsystem (src/etc/inc/auth.inc), same as set_password.php, so hashing
 * scheme, config.xml write, and OS pw-db sync exactly match what the
 * pfSense webConfigurator does. Used only by the agent's boot-time
 * provisioning.
 *
 * stdin: new password (never passed as an argv or env var)
 */

require_once("globals.inc");
require_once("config.inc");
require_once("auth.inc");

$password = trim(stream_get_contents(STDIN));

if ($password === "") {
	fwrite(STDERR, "password must not be empty\n");
	exit(1);
}

$index = null;
$username = null;
foreach (config_get_path('system/user', []) as $idx => $u) {
	if ((int)($u['uid'] ?? -1) === 0) {
		$index = $idx;
		$username = $u['name'];
		break;
	}
}

if ($index === null) {
	fwrite(STDERR, "no uid=0 (default superuser) account found in config\n");
	exit(1);
}

// local_user_set_password(&$user, $password) mutates $user in place (sets
// the new hash field, clears the old ones) but does NOT persist anything
// itself — config_set_path()/write_config() are the caller's job. (An
// earlier version of this script assumed a newer/different pfSense API
// that wrapped $user in a ['idx'=>..,'item'=>..] array and persisted
// internally; that doesn't exist on pfSense 2.7.2 and silently no-op'd —
// this caller-persists form was confirmed against the actual deployed
// src/etc/inc/auth.inc via ReflectionFunction on a real box.)
$user = config_get_path("system/user/{$index}");
local_user_set_password($user, $password);
config_set_path("system/user/{$index}", $user);
write_config("Password changed via PlusClouds agent for default superuser account ({$username})");
local_user_set($user);

echo json_encode(['status' => 'ok', 'username' => $username]);
