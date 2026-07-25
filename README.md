# PlusClouds VM Agent

> **The foundation that thinks.**

The PlusClouds VM Agent is a lightweight, production-grade daemon that turns any Linux or Windows machine into an intelligent infrastructure node. It connects to the PlusClouds platform over NATS, streams real-time telemetry, executes remote commands, and announces its own capabilities — so your platform always knows exactly what each machine can do.

No REST API. No open ports. No certificates to manage. No config file, either — the agent reads its entire runtime configuration (identity, NATS connection, allowed operations, logging, autoheal) from the config-drive ISO the platform attaches at provisioning time, and caches a local copy so it keeps working on later boots even if the drive is detached.

---

## What it does

| Capability | Description |
|---|---|
| **Real-time telemetry** | CPU, memory, disk I/O, and network metrics pushed every 30 seconds |
| **Remote command execution** | Service management, system updates, and custom operations triggered from the platform |
| **Capability discovery** | On boot the agent publishes its full operation schema — the platform always knows what it can do |
| **Heartbeat** | Keeps the `is_alive` status current on the platform every 30 seconds |
| **Cross-platform** | Single codebase, two binaries: Linux (systemd/D-Bus) and Windows (SCM stub) |
| **Zero open ports** | Outbound-only NATS WebSocket connection — no inbound firewall rules needed |

---

## Architecture

The agent communicates exclusively over NATS. It subscribes to its own command subject and publishes events to its own event subject.

```
agent.vm.{uuid}.cmd   ←  platform sends commands
agent.vm.{uuid}.evt   →  agent sends telemetry, heartbeat, capabilities, results
vm.{uuid}.telemetry   →  client-facing telemetry stream (VM_TELEMETRY JetStream, 15-min retention)
```

Authentication uses the `agent_api_key` from the config-drive's `pc-meta-data.json` (written by the platform during provisioning). The NATS auth callout validates every connection against the platform database and issues a scoped JWT — no static passwords, no shared secrets.

### Message envelope

Every NATS message in either direction shares a common JSON envelope:

```json
{
  "v":          1,
  "id":         "550e8400-e29b-41d4-a716-446655440000",
  "type":       "command|telemetry|heartbeat|capabilities|result|...",
  "agent_type": "vm",
  "agent_uuid": "550e8400-e29b-41d4-a716-446655440001",
  "timestamp":  1748000000,
  "payload":    {}
}
```

See [docs/protocol.md](docs/protocol.md) for the full payload schema for every event type.

---

## Telemetry payload

The agent publishes a structured telemetry snapshot every 30 seconds:

```json
{
  "cpu": {
    "usage_pct":  12.5,
    "core_count": 4,
    "load_avg":   [0.18, 0.23, 0.09],
    "cores": [
      { "id": 0, "usage_pct": 15.2 },
      { "id": 1, "usage_pct": 9.8 }
    ]
  },
  "memory": {
    "total_bytes": 4051935232,
    "used_bytes":  564654080,
    "usage_pct":   13.9
  },
  "disks": [
    {
      "device": "/dev/xvda2", "mountpoint": "/",
      "total_bytes": 42156257280, "used_bytes": 7149359104, "usage_pct": 17.7,
      "io": {
        "read_bytes_per_s": 1048576, "write_bytes_per_s": 524288,
        "read_iops": 120, "write_iops": 45, "util_pct": 23.4
      }
    }
  ],
  "network": [
    { "interface": "eth0", "bytes_sent": 95310, "bytes_recv": 1652348, "is_up": true }
  ]
}
```

Disk I/O rates are calculated as a delta between consecutive snapshots — the first telemetry event after boot omits the `io` field. Pseudo-filesystems (`tmpfs`, `devtmpfs`, etc.) and virtual network interfaces (`lo`, `docker*`, `veth*`) are automatically excluded.

---

## Supported operations

The agent announces its available operations on boot via a `capabilities` event. The platform can also request a fresh capabilities list at any time by sending `agent.allowed_operations`.

| Operation | Description | Parameters |
|---|---|---|
| `agent.allowed_operations` | Re-publish the capabilities list | — |
| `services.list` | List all loaded systemd services | — |
| `services.get` | Get status of a single service | `name` (string) |
| `services.start` | Start a service | `name` (string) |
| `services.stop` | Stop a service | `name` (string) |
| `services.restart` | Restart a service | `name` (string) |
| `services.reload` | Reload a service | `name` (string) |
| `services.enable` | Enable a service on boot | `name` (string) |
| `services.disable` | Disable a service on boot | `name` (string) |
| `system.info` | Hostname, OS, kernel, uptime | — |
| `system.metrics` | Full resource snapshot | — |
| `system.cpu` | CPU usage + per-core breakdown | — |
| `system.memory` | RAM utilisation | — |
| `system.disk` | Disk usage + I/O rates | — |
| `system.network` | Network interface counters | — |
| `system.update` | `apt-get update && upgrade -y` | — (Ubuntu/Debian only) |
| `telemetry.set_interval` | Change telemetry push interval | `interval_s` (integer, min 5) |
| `vm.reboot` | Reboot the machine | — |
| `vm.shutdown` | Shut down the machine | — |
| `exec` | Run an allowed binary | `command` (string), `args` (array) |

All operations are opt-in. Remove any entry from `allowed_operations` in the config-drive's `agent` settings and the platform receives a `rejected` result instead of executing it.

---

## Installation

### 1. Deploy the binary

```bash
# Linux
scp bin/plusclouds.linux root@<server-ip>:/usr/local/bin/plusclouds-agent
chmod +x /usr/local/bin/plusclouds-agent

# Windows
# Copy bin/plusclouds.windows to the target machine and run it as a service
```

### 2. Create the runtime directories

```bash
mkdir -p /var/log/plusclouds /var/lib/plusclouds/cache
chmod 0750 /var/log/plusclouds
```

### 3. Attach the config-drive

The platform attaches a config-drive ISO (labelled `plusclouds-config`, containing `pc-meta-data.json`) to the VM during provisioning. The agent mounts it automatically at boot, reads its identity, NATS, and runtime settings from it, and caches a local copy under `/var/lib/plusclouds/cache/` so it keeps working on later boots even if the drive is later detached. There's nothing to deploy manually — if you're running the agent somewhere the config-drive isn't attached (e.g. local testing), see [Configuration reference](#configuration-reference) below for the built-in defaults and environment variable overrides.

### 4. Install and start the systemd service

```bash
scp systemd/plusclouds-agent.service root@<server-ip>:/etc/systemd/system/
systemctl daemon-reload
systemctl enable --now plusclouds-agent
```

### 5. Verify

```bash
journalctl -fu plusclouds-agent
# or
tail -f /var/log/plusclouds/agent.log | jq .
```

You should see:
```
agent identity resolved   {"agent_uuid": "..."}
connected to NATS         {"url": "wss://nats.plusclouds.com:443"}
capabilities published    {"operation_count": 17}
heartbeat published
telemetry published
```

---

## Configuration reference

Configuration is layered: **built-in defaults** → the `agent` object inside `pc-meta-data.json` on the config-drive (or its local cache) → **environment variables** (`PLUSCLOUDS_AGENT_*`, e.g. `PLUSCLOUDS_AGENT_NATS_API_KEY`), each layer overriding the previous one. There is no config file — this is the shape of the `agent` object the platform writes into `pc-meta-data.json`:

```json
{
  "agent": {
    "nats": {
      "connection_type": "websocket",
      "url": "nats://nats.plusclouds.com:4222",
      "websocket_url": "wss://nats.plusclouds.com:443",
      "agent_uuid": "<vm-uuid>",
      "api_key": "<agent-api-key>",
      "max_reconnects": -1,
      "reconnect_wait": "5s"
    },
    "agent": {
      "heartbeat_interval": "30s",
      "telemetry_interval": "30s",
      "allowed_operations": [
        "agent.allowed_operations",
        "agent.version",
        "services.list",
        "services.get",
        "services.start",
        "services.stop",
        "services.restart",
        "services.reload",
        "services.enable",
        "services.disable",
        "system.info",
        "system.metrics",
        "system.cpu",
        "system.memory",
        "system.disk",
        "system.network",
        "system.update",
        "telemetry.set_interval"
      ],
      "allowed_commands": ["/usr/bin/journalctl", "/usr/bin/df", "/usr/bin/free"]
    },
    "iso": {
      "mount_path": "/media/plusclouds-config"
    },
    "log": {
      "level": "info",
      "format": "json",
      "file": "/var/log/plusclouds/agent.log"
    },
    "autoheal": {
      "enabled": true,
      "restart_delay": "10s"
    }
  }
}
```

The top-level `virtual_machine_id`/`agent_api_key` fields of `pc-meta-data.json` (VM identity) always take precedence over `agent.nats.agent_uuid`/`agent.nats.api_key` if the two ever differ.

---

## Building from source

Requires Go 1.22+.

```bash
# Development build (current OS)
make build

# Production build — Linux amd64, static binary, stripped
make build-linux

# Production build — Windows amd64
make build-windows

# Both platforms at once
make build-all

# Run tests
make test
```

Outputs:
```
bin/plusclouds.linux    — ELF 64-bit, statically linked, ~12 MB
bin/plusclouds.windows  — PE32+, ~12 MB
```

---

## Platform compatibility

| Feature | Linux | Windows |
|---|---|---|
| NATS connection | ✅ | ✅ |
| Telemetry (CPU, RAM, disk, network) | ✅ | ✅ (load_avg = 0) |
| Heartbeat | ✅ | ✅ |
| Capabilities event | ✅ | ✅ |
| Service management | ✅ systemd/D-Bus | ⚙ stub (SCM planned) |
| `system.update` | ✅ Ubuntu/Debian | ✗ |
| `vm.reboot` / `vm.shutdown` | ✅ `systemctl` | ✅ `shutdown /r` |

---

## Security model

- **Outbound-only** — the agent never listens on any port
- **Scoped JWT** — the NATS auth callout issues a JWT granting publish/subscribe only to this agent's own subjects
- **Operation allowlist** — `allowed_operations` in the config-drive's `agent` settings is a hard gate; unknown or unlisted operations return `rejected`
- **Exec allowlist** — when `exec` is enabled, only binaries explicitly listed in `allowed_commands` can be invoked
- **Token revocation** — remove `events_token` from the platform database and the agent is rejected on its next connection attempt

---

## Support

Having trouble? We're here to help.

📧 **[support@plusclouds.com](mailto:support@plusclouds.com)**

For bug reports and feature requests, open an issue on GitHub.

---

## Our Libraries

This agent is part of the **PlusClouds open-source ecosystem** — precision infrastructure and intelligence tools built for SaaS companies and tech-forward businesses.

Browse all available libraries and building blocks:
[https://plusclouds.com/us/solutions/libraries](https://plusclouds.com/us/solutions/libraries)

---

## Join the Community

Great infrastructure is built together. The PlusClouds developer community is where engineers share ideas, ask questions, and help shape the direction of the platform. Whether you're integrating a single agent or building an entire infrastructure layer on our stack — you're welcome here.

[https://plusclouds.com/us/community](https://plusclouds.com/us/community)
