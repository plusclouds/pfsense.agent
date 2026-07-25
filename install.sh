#!/bin/sh
# Installs the PlusClouds agent on pfSense (FreeBSD) as an rc.d service and
# starts it. Run as root on the target pfSense box.
#
# Expects to be run from a directory that also contains, alongside this
# script:
#   bin/plusclouds.freebsd (or plusclouds.freebsd-arm64)  — from `make build-freebsd`
#   configs/agent.yaml                                    — sample config
#   rc.d/plusclouds-agent                                 — rc.d script
#
# i.e. copy (scp -r) the repo checkout, or an extracted release containing
# these paths, to the pfSense box and run ./install.sh from there.

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

INSTALL_BIN="/usr/local/bin/plusclouds-agent"
CONFIG_DIR="/etc/plusclouds"
CONFIG_FILE="${CONFIG_DIR}/agent.yaml"
LOG_DIR="/var/log/plusclouds"
STATE_DIR="/var/db/plusclouds-agent"
CACHE_DIR="/var/db/plusclouds/cache"
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

CONFIG_SRC="${SCRIPT_DIR}/configs/agent.yaml"
[ -f "$CONFIG_SRC" ] || die "sample config not found at ${CONFIG_SRC}"

log "Installing plusclouds-agent binary (${ARCH}) to ${INSTALL_BIN}"
install -m 0755 "$BINARY" "$INSTALL_BIN"

log "Creating directories"
mkdir -p "$CONFIG_DIR" "$LOG_DIR" "$STATE_DIR" "$CACHE_DIR"
chmod 0750 "$LOG_DIR"
chmod 0700 "$STATE_DIR" "$CACHE_DIR"

if [ -f "$CONFIG_FILE" ]; then
	log "Config already exists at ${CONFIG_FILE} — leaving it untouched"
else
	log "Installing default config to ${CONFIG_FILE}"
	# /var/lib isn't a standard FreeBSD path; point the ISO metadata cache at
	# /var/db instead, where the boot-provisioning marker files also live.
	sed 's#^\( *cache_path: \).*#\1/var/db/plusclouds/cache/pc-meta-data.json#' \
		"$CONFIG_SRC" > "$CONFIG_FILE"
	chmod 0640 "$CONFIG_FILE"
fi

log "Installing rc.d script to ${RC_D_SCRIPT}"
install -m 0555 "$RC_D_SRC" "$RC_D_SCRIPT"

# Enable in /etc/rc.conf.local, NOT /etc/rc.conf — pfSense regenerates
# /etc/rc.conf from config.xml on every config write, which would silently
# drop a manually-added enable flag there.
if [ -f "$RC_CONF_LOCAL" ] && grep -q '^plusclouds_agent_enable=' "$RC_CONF_LOCAL"; then
	log "Service already enabled in ${RC_CONF_LOCAL}"
else
	log "Enabling service in ${RC_CONF_LOCAL} (not /etc/rc.conf, which pfSense regenerates)"
	echo 'plusclouds_agent_enable="YES"' >> "$RC_CONF_LOCAL"
fi

log "Starting plusclouds-agent"
service plusclouds-agent start

echo
echo "Done. Next steps:"
echo "  1. If this VM isn't provisioned via the config-drive ISO, edit ${CONFIG_FILE}"
echo "     and set nats.agent_uuid and nats.api_key."
echo "  2. Check status:  service plusclouds-agent status"
echo "  3. Tail logs:     tail -f ${LOG_DIR}/agent.log"
