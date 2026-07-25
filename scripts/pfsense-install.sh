#!/bin/sh
# Installs (or upgrades) the PlusClouds agent on pfSense (FreeBSD) as an
# rc.d service, from a GitHub release — no local checkout needed.
#
# Usage:
#   # Piped directly (recommended) — the "-s" here is a real sh(1) flag
#   # meaning "read the script from stdin", NOT part of the version:
#   fetch -qo - https://raw.githubusercontent.com/plusclouds/pfsense.agent/master/scripts/pfsense-install.sh | sh
#   fetch -qo - .../scripts/pfsense-install.sh | sh -s v1.3.0   # pin a release
#
#   # Already downloaded the file? Run it directly — no "-s" here, just the
#   # version as a plain argument (sh pfsense-install.sh -s v1.3.0 is WRONG:
#   # that passes "-s" itself as the version and 404s):
#   sh pfsense-install.sh
#   sh pfsense-install.sh v1.3.0
#
# Safe to re-run: upgrades the binary and rc.d script in place. There's no
# config file to manage — the agent reads all of its runtime configuration
# (identity, NATS, allowed operations, logging, autoheal) from the
# config-drive ISO (pc-meta-data.json) the platform attaches to the VM.
#
# Uses FreeBSD's built-in `fetch` (always present on pfSense) rather than
# curl, which isn't installed by default; and /bin/sh rather than bash,
# which also isn't installed by default on pfSense.

set -eu

REPO="plusclouds/pfsense.agent"
INSTALL_BIN="/usr/local/bin/plusclouds-agent"
CONFIG_DIR="/etc/plusclouds"
LOG_DIR="/var/log/plusclouds"
STATE_DIR="/var/db/plusclouds-agent"
CACHE_DIR="/var/lib/plusclouds/cache"
ISO_MOUNT_DIR="/media/plusclouds-config"
RC_D_SCRIPT="/usr/local/etc/rc.d/plusclouds-agent"
RC_CONF_LOCAL="/etc/rc.conf.local"
VERSION="${1:-${VERSION:-latest}}"

case "$VERSION" in
	-*) echo "error: version argument '${VERSION}' looks like a flag, not a version." >&2
	    echo "       If you ran this as 'sh pfsense-install.sh -s v1.3.0', drop the -s:" >&2
	    echo "       sh pfsense-install.sh v1.3.0" >&2
	    exit 1 ;;
esac

log()  { echo "==> $*"; }
die()  { echo "error: $*" >&2; exit 1; }

# fetch(1) is always present on FreeBSD/pfSense; prefer curl only if the
# operator has already installed it.
if command -v curl >/dev/null 2>&1; then
	dl() { curl -fsSL -o "$1" "$2"; }
else
	dl() { fetch -qo "$1" "$2"; }
fi

[ "$(id -u)" -eq 0 ] || die "must be run as root"
[ "$(uname -s)" = "FreeBSD" ] || die "this installer is for pfSense/FreeBSD only"

case "$(uname -m)" in
	amd64) ARCH_BIN="plusclouds.freebsd" ;;
	arm64) ARCH_BIN="plusclouds.freebsd-arm64" ;;
	*) die "unsupported architecture: $(uname -m)" ;;
esac

TMP_META=""
TMP_BIN=""
cleanup() {
	[ -n "$TMP_META" ] && rm -f "$TMP_META"
	[ -n "$TMP_BIN" ] && rm -f "$TMP_BIN"
}
trap cleanup EXIT

if [ "$VERSION" = "latest" ]; then
	log "resolving latest release..."
	TMP_META=$(mktemp)
	dl "$TMP_META" "https://api.github.com/repos/${REPO}/releases/latest"
	VERSION=$(grep -m1 '"tag_name"' "$TMP_META" | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
	[ -n "$VERSION" ] || die "could not resolve latest release version"
fi
log "installing version ${VERSION} (${ARCH_BIN})"

RELEASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"
RAW_URL="https://raw.githubusercontent.com/${REPO}/${VERSION}"

if service plusclouds-agent status >/dev/null 2>&1; then
	log "stopping existing plusclouds-agent"
	service plusclouds-agent stop
fi

log "downloading ${ARCH_BIN} ${VERSION}..."
TMP_BIN=$(mktemp)
dl "$TMP_BIN" "${RELEASE_URL}/${ARCH_BIN}" || die "download failed — check that ${VERSION} exists at https://github.com/${REPO}/releases"
log "sha256: $(sha256 -q "$TMP_BIN")"
install -m 0755 "$TMP_BIN" "$INSTALL_BIN"

log "downloading rc.d script..."
dl "$RC_D_SCRIPT" "${RAW_URL}/rc.d/plusclouds-agent" || die "could not download rc.d script from ${RAW_URL}/rc.d/plusclouds-agent"
chmod 0555 "$RC_D_SCRIPT"

log "creating directories"
mkdir -p "$CONFIG_DIR" "$LOG_DIR" "$STATE_DIR" "$CACHE_DIR" "$ISO_MOUNT_DIR"
chmod 0750 "$LOG_DIR"
chmod 0700 "$STATE_DIR" "$CACHE_DIR"

# Enable in /etc/rc.conf.local, NOT /etc/rc.conf — pfSense regenerates
# /etc/rc.conf from config.xml on every config save, which would silently
# drop a manually-added enable flag there.
if [ -f "$RC_CONF_LOCAL" ] && grep -q '^plusclouds_agent_enable=' "$RC_CONF_LOCAL"; then
	log "service already enabled in ${RC_CONF_LOCAL}"
else
	log "enabling service in ${RC_CONF_LOCAL} (not /etc/rc.conf, which pfSense regenerates)"
	echo 'plusclouds_agent_enable="YES"' >> "$RC_CONF_LOCAL"
fi

log "registering boot start via pfSense's afterbootupshellcmd"
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

log "starting plusclouds-agent"
service plusclouds-agent start

echo
echo "Done. plusclouds-agent ${VERSION} is enabled and will start on every boot"
echo "(via system/afterbootupshellcmd — see comment above for why)."
echo "  Check status:  service plusclouds-agent status"
echo "  Tail logs:     tail -f ${LOG_DIR}/agent.log"
echo
echo "There's no config file — the agent reads its identity and runtime"
echo "settings from the config-drive ISO attached to this VM. For local"
echo "testing without one, set PLUSCLOUDS_AGENT_* vars in"
echo "${CONFIG_DIR}/environment and run: service plusclouds-agent restart"
