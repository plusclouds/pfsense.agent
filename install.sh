#!/bin/sh
# Installs the PlusClouds agent on pfSense (FreeBSD) as an rc.d service and
# starts it. Run as root on the target pfSense box.
#
# There's no config file to install — the agent reads its identity and
# runtime settings from the config-drive ISO (pc-meta-data.json) the
# platform attaches to the VM, and caches a local copy so it keeps working
# on later boots even if the drive is detached. If this VM has no
# config-drive attached (e.g. local testing), set PLUSCLOUDS_AGENT_* env
# vars in /etc/plusclouds/environment and restart the service.
#
# Expects to be run from a directory that also contains, alongside this
# script:
#   bin/plusclouds.freebsd (or plusclouds.freebsd-arm64)  — from `make build-freebsd`
#   rc.d/plusclouds-agent                                 — rc.d script
#
# i.e. copy (scp -r) the repo checkout, or an extracted release containing
# these paths, to the pfSense box and run ./install.sh from there.

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

INSTALL_BIN="/usr/local/bin/plusclouds-agent"
CONFIG_DIR="/etc/plusclouds"
LOG_DIR="/var/log/plusclouds"
STATE_DIR="/var/db/plusclouds-agent"
CACHE_DIR="/var/lib/plusclouds/cache"
ISO_MOUNT_DIR="/media/plusclouds-config"
RC_D_SCRIPT="/usr/local/etc/rc.d/plusclouds-agent"
RC_CONF_LOCAL="/etc/rc.conf.local"

log() { echo "==> $*"; }
die() { echo "error: $*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "must be run as root"
[ "$(uname -s)" = "FreeBSD" ] || die "this installer is for pfSense/FreeBSD only"

ARCH=$(uname -m)
case "$ARCH" in
	amd64) BINARY="${SCRIPT_DIR}/bin/plusclouds.freebsd" ;;
	arm64) BINARY="${SCRIPT_DIR}/bin/plusclouds.freebsd-arm64" ;;
	*) die "unsupported architecture: ${ARCH}" ;;
esac
[ -f "$BINARY" ] || die "binary not found at ${BINARY} — build it first with 'make build-freebsd' (or build-freebsd-arm64) on a dev machine and copy bin/ alongside this script"

RC_D_SRC="${SCRIPT_DIR}/rc.d/plusclouds-agent"
[ -f "$RC_D_SRC" ] || die "rc.d script not found at ${RC_D_SRC}"

if service plusclouds-agent status >/dev/null 2>&1; then
	log "Stopping existing plusclouds-agent"
	service plusclouds-agent stop
fi

log "Installing plusclouds-agent binary (${ARCH}) to ${INSTALL_BIN}"
install -m 0755 "$BINARY" "$INSTALL_BIN"

log "Creating directories"
mkdir -p "$CONFIG_DIR" "$LOG_DIR" "$STATE_DIR" "$CACHE_DIR" "$ISO_MOUNT_DIR"
chmod 0750 "$LOG_DIR"
chmod 0700 "$STATE_DIR" "$CACHE_DIR"

log "Installing rc.d script to ${RC_D_SCRIPT}"
install -m 0555 "$RC_D_SRC" "$RC_D_SCRIPT"

# Enable in /etc/rc.conf.local, NOT /etc/rc.conf — pfSense regenerates
# /etc/rc.conf from config.xml on every config save, which would silently
# drop a manually-added enable flag there.
if [ -f "$RC_CONF_LOCAL" ] && grep -q '^plusclouds_agent_enable=' "$RC_CONF_LOCAL"; then
	log "Service already enabled in ${RC_CONF_LOCAL}"
else
	log "Enabling service in ${RC_CONF_LOCAL} (not /etc/rc.conf, which pfSense regenerates)"
	echo 'plusclouds_agent_enable="YES"' >> "$RC_CONF_LOCAL"
fi

log "Registering boot start via pfSense's afterbootupshellcmd"
# pfSense replaces the entire boot sequence with its own /etc/pfSense-rc,
# which only calls a hardcoded handful of specific scripts by name and
# never does a generic scan of /usr/local/etc/rc.d/ — rc.conf(.local)'s
# enable flag alone will never make this service start automatically. The
# supported hook for a one-off custom boot command is the built-in (no
# package needed) system/afterbootupshellcmd config.xml field, run once at
# the end of pfSense's own rc.bootup. Appended rather than overwritten, in
# case something else is already using it.
/usr/local/bin/php <<'PHPEOF'
<?php
require_once("globals.inc");
require_once("config.inc");

$cmd = "/usr/sbin/service plusclouds-agent start";
$existing = config_get_path('system/afterbootupshellcmd', '');

if (strpos($existing, $cmd) !== false) {
	exit(0);
}

$new = trim($existing) === '' ? $cmd : rtrim(trim($existing), '; ') . '; ' . $cmd;
config_set_path('system/afterbootupshellcmd', $new);
write_config("Registered plusclouds-agent boot start via afterbootupshellcmd");
PHPEOF

log "Starting plusclouds-agent"
service plusclouds-agent start

echo
echo "Done. plusclouds-agent is enabled and will start on every boot"
echo "(via system/afterbootupshellcmd — see comment above for why)."
echo "  Check status:  service plusclouds-agent status"
echo "  Tail logs:     tail -f ${LOG_DIR}/agent.log"
echo
echo "There's nothing to configure by hand — the agent reads its identity"
echo "and runtime settings from the config-drive ISO attached to this VM."
echo "If this VM has no config-drive (e.g. local testing), set"
echo "PLUSCLOUDS_AGENT_NATS_AGENT_UUID / PLUSCLOUDS_AGENT_NATS_API_KEY in"
echo "${CONFIG_DIR}/environment and run: service plusclouds-agent restart"
