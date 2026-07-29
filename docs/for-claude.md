# What this project is (for Claude)

This is `pfsense.agent`, a fork of PlusClouds' Go VM agent (`plusclouds/vm.agent`,
module name still `github.com/plusclouds/ubuntu-agent`) specialized for running
on **pfSense/FreeBSD**, in addition to the original Linux/Windows targets.

The agent is a small daemon that runs on a managed machine, connects outbound to
the PlusClouds platform over NATS (no inbound ports, no REST API), streams
telemetry/heartbeat, and executes remote commands the platform sends it
(service management, `pfsense.set_password`, etc.). Everything it needs to
know at boot — identity, NATS credentials, allowed operations — comes from a
config-drive ISO the platform attaches at provisioning time; there is no
config file to hand-edit.

**Start with the root [README.md](../README.md)** — it's the accurate,
up-to-date reference for the protocol, operations table, install flow, and
config schema. The files under `docs/` (`architecture.md`, `api.md`,
`overview.md`, `protocol.md`, `cli.md`, `integration.md`, `orchestration.md`)
describe an earlier/aspirational design (e.g. an "Event Bus", `instance.json`)
and don't all match the current code — treat them as background, not ground
truth, and prefer reading `internal/` directly when they disagree.

## Why this fork exists

The upstream agent targets Ubuntu/Windows VMs. This fork adds the pieces
needed to run the same agent on pfSense firewalls:

- `internal/modules/pfsense/` — pfSense-specific logic: matching config-drive
  NIC metadata to local interfaces by MAC, applying static network config,
  and setting the default pfSense superuser's password through pfSense's
  config/auth subsystem (`pfsense_freebsd.go`; `pfsense_stub.go` is the no-op
  build tag stub for non-FreeBSD platforms).
- `rc.d/` — a FreeBSD rc.d service script (pfSense doesn't use systemd).
- `scripts/pfsense-install.sh` — curl/fetch-based installer for pasting
  directly into a pfSense shell.
- Two boot-time provisioning steps documented in the README under
  **"Boot-time provisioning from ISO metadata (pfSense)"**: a one-shot network
  config apply (marker-gated, self-verifying against a real NATS connect, and
  reverts on failure) and an every-boot password apply (deliberately
  *not* marker-gated — see the README for why a marker there caused a
  real incident).

Most recent work on this repo (see `git log`) has been hardening those two
boot-time steps: pfSense's non-standard boot sequence
(`afterbootupshellcmd`, not a generic rc.d scan), routing quirks on
single-NIC boxes, and bugs where a "success" marker or a reverted probe
masked real failures.

## Layout

```
cmd/agent/     the daemon entrypoint, per-platform main (linux/windows/freebsd)
cmd/ctl/       plsctl CLI — targets the old v1 REST API, currently non-functional (v2 uses NATS)
internal/
  config/      layered config: built-in defaults -> config-drive JSON -> PLUSCLOUDS_AGENT_* env
  dispatcher/  routes inbound NATS commands to module handlers
  executor/    runs allowed operations, enforces the allowlist
  modules/
    system/    CPU/mem/disk/network telemetry (gopsutil)
    services/  service start/stop/enable (systemd/D-Bus on Linux; stubs elsewhere)
    diskresize/
    pfsense/   pfSense-only: netconfig apply, password apply (see above)
  nats/        connection, auth, JetStream durable consumers
  protocol/    the envelope + payload types shared with the platform
  publisher/   telemetry/heartbeat/capabilities publishing
pkg/isoconfig/ config-drive ISO mounting + pc-meta-data.json parsing
rc.d/          FreeBSD service script
systemd/       Linux service unit
```

## Working in this repo

- `make build-freebsd` (or `build-freebsd-arm64` for Netgate hardware) to
  cross-compile for pfSense; `make test` for unit tests.
- Bugs here are usually boot-sequence or platform-quirk bugs, not generic Go
  bugs — read the relevant README section (network provisioning, password
  provisioning, rc.d start note) before assuming the fix is in application
  logic.
- No `graphify-out/` exists in this repo yet, so the graphify-first rule in
  the root `CLAUDE.md` doesn't apply here — use normal grep/read/explore.
