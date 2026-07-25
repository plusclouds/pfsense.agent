#!/usr/bin/env bash
# Installs (or upgrades) the PlusClouds VM agent as a systemd service.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/plusclouds/vm.agent/master/scripts/install.sh | sudo bash
#   sudo ./scripts/install.sh              # install the latest release
#   sudo ./scripts/install.sh v2.0.0       # pin a specific release
#
# (VERSION=v2.0.0 as an env var also works, but only when run directly as
# root or via `sudo -E` — plain `sudo` strips arbitrary env vars by
# default, so the positional argument above is the reliable way to pin.)
#
# Safe to re-run: upgrades the binary and systemd unit in place. There is no
# config file to manage (since v2.0.0) — the agent reads all of its runtime
# configuration (identity, NATS, allowed operations, logging, autoheal) from
# the config-drive ISO (pc-meta-data.json) the platform attaches to the VM.

set -euo pipefail

REPO="plusclouds/vm.agent"
BINARY_NAME="plusclouds-agent"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/plusclouds"
LOG_DIR="/var/log/plusclouds"
SERVICE_DIR="/etc/systemd/system"
SERVICE_NAME="plusclouds-agent.service"
VERSION="${1:-${VERSION:-latest}}"

log()  { echo "[install] $*"; }
fail() { echo "[install] ERROR: $*" >&2; exit 1; }

# --- pre-flight --------------------------------------------------------------

[ "$(id -u)" -eq 0 ] || fail "must be run as root (try: sudo $0)"
command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v systemctl >/dev/null 2>&1 || fail "systemd (systemctl) is required — this script only supports systemd-based Linux"

case "$(uname -m)" in
  x86_64|amd64) : ;;
  *) fail "unsupported architecture: $(uname -m) (only amd64 is published today)" ;;
esac

# --- resolve version -----------------------------------------------------------

if [ "$VERSION" = "latest" ]; then
  log "resolving latest release..."
  VERSION="$(curl -fsSL -H "User-Agent: plusclouds-agent-installer" \
    "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
  [ -n "$VERSION" ] || fail "could not resolve latest release version"
fi
log "installing version ${VERSION}"

RELEASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"
RAW_URL="https://raw.githubusercontent.com/${REPO}/${VERSION}"

# --- stop existing service before replacing the binary ------------------------

if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
  log "stopping existing ${SERVICE_NAME}..."
  systemctl stop "$SERVICE_NAME"
fi

# --- download + install binary --------------------------------------------------

TMP_BIN="$(mktemp)"
trap 'rm -f "$TMP_BIN"' EXIT

log "downloading plusclouds.linux ${VERSION}..."
curl -fsSL -o "$TMP_BIN" "${RELEASE_URL}/plusclouds.linux" \
  || fail "download failed — check that ${VERSION} exists at https://github.com/${REPO}/releases"
log "sha256: $(sha256sum "$TMP_BIN" | cut -d' ' -f1)"

install -m 0755 "$TMP_BIN" "${INSTALL_DIR}/${BINARY_NAME}"
log "installed ${INSTALL_DIR}/${BINARY_NAME}"

# --- systemd unit ----------------------------------------------------------------

log "installing systemd unit..."
curl -fsSL -o "${SERVICE_DIR}/${SERVICE_NAME}" "${RAW_URL}/systemd/${SERVICE_NAME}" \
  || fail "could not download systemd unit from ${RAW_URL}/systemd/${SERVICE_NAME}"
chmod 0644 "${SERVICE_DIR}/${SERVICE_NAME}"

# --- directories -------------------------------------------------------------------

# $CONFIG_DIR only holds the optional /etc/plusclouds/environment override
# file the systemd unit loads; there's no agent.yaml to install anymore.
mkdir -p "$CONFIG_DIR" "$LOG_DIR"
chmod 0750 "$LOG_DIR"

# --- enable + start ------------------------------------------------------------------

systemctl daemon-reload
systemctl enable --now "$SERVICE_NAME"

log ""
log "Install complete. ${SERVICE_NAME} is enabled and will start on every boot."
log "  journalctl -fu ${SERVICE_NAME}"
log ""
log "The agent reads its identity and runtime config from the config-drive"
log "ISO (pc-meta-data.json) the platform attaches to this VM — nothing to"
log "edit by hand. If this VM has no config-drive attached (e.g. local"
log "testing), set PLUSCLOUDS_AGENT_NATS_AGENT_UUID / _API_KEY in"
log "${CONFIG_DIR}/environment and run: systemctl restart ${SERVICE_NAME}"
