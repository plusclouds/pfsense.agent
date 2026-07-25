# PlusClouds VM Agent

> **The foundation that thinks.**

The PlusClouds VM Agent is a lightweight, production-grade daemon that turns any Linux or Windows machine into an intelligent infrastructure node. It connects to the PlusClouds platform over NATS, streams real-time telemetry, executes remote commands, and announces its own capabilities — so your platform always knows exactly what each machine can do.

No REST API. No open ports. No certificates to manage. Just a single binary, a config file, and a secure WebSocket connection to the platform.

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

Authentication uses the `agent_api_key` from `agent.yaml` (written by the platform during provisioning). The NATS auth callout validates every connection against the platform database and issues a scoped JWT — no static passwords, no shared secrets.

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
| `pfsense.set_password` | Change a local pfSense user's password (pfSense/FreeBSD only) | `username` (string), `password` (string, sensitive) |

All operations are opt-in. Remove any entry from `allowed_operations` in `agent.yaml` and the platform receives a `rejected` result instead of executing it.

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

### 2. Create the config directory

```bash
mkdir -p /etc/plusclouds /var/log/plusclouds
chmod 0750 /var/log/plusclouds
```

### 3. Deploy the config

```bash
scp configs/agent.yaml root@<server-ip>:/etc/plusclouds/agent.yaml
```

Edit `/etc/plusclouds/agent.yaml` and set the identity fields for this machine (written automatically during VM provisioning):

```yaml
nats:
  connection_type: websocket           # "nats" or "websocket"
  websocket_url: wss://nats.plusclouds.com:443
  agent_uuid: "<vm-uuid>"             # from iaas_virtual_machines
  api_key:     "<agent-api-key>"      # from iaas_virtual_machines.events_token
```

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

### pfSense/FreeBSD: automated install

`install.sh` installs the binary, config, and an rc.d service (pfSense uses FreeBSD's rc.d, not systemd) in one step, and starts it.

```bash
make build-freebsd            # or build-freebsd-arm64 for Netgate 1100/2100/4100
scp -r bin configs rc.d install.sh root@<pfsense-ip>:/root/plusclouds-agent-install/
ssh root@<pfsense-ip> 'cd /root/plusclouds-agent-install && ./install.sh'
```

This installs the binary to `/usr/local/bin/plusclouds-agent`, the config to `/etc/plusclouds/agent.yaml` (only if not already present), the rc.d script to `/usr/local/etc/rc.d/plusclouds-agent`, enables it in `/etc/rc.conf.local` (**not** `/etc/rc.conf` — pfSense regenerates that file from `config.xml` on every config save, which would silently drop a manually-added enable flag), and starts it. Manage it afterward with the standard `service plusclouds-agent {start,stop,status}`.

---

## Configuration reference

```yaml
nats:
  connection_type: websocket           # nats | websocket (ws:// needs no certificates)
  url: nats://nats.plusclouds.com:4222
  websocket_url: wss://nats.plusclouds.com:443
  agent_uuid: ""                       # VM UUID — set by provisioning
  api_key: ""                          # NATS auth token — set by provisioning
  max_reconnects: -1                   # -1 = unlimited
  reconnect_wait: 5s

agent:
  heartbeat_interval: 30s
  telemetry_interval: 30s             # changeable at runtime via telemetry.set_interval
  allowed_operations:
    - agent.allowed_operations
    - services.list
    - services.get
    - services.start
    - services.stop
    - services.restart
    - services.reload
    - services.enable
    - services.disable
    - system.info
    - system.metrics
    - system.cpu
    - system.memory
    - system.disk
    - system.network
    - system.update
    - telemetry.set_interval
    # - vm.reboot
    # - vm.shutdown
    # - exec
  allowed_commands:                    # only used when exec is enabled above
    - /usr/bin/journalctl
    - /usr/bin/df
    - /usr/bin/free

iso:
  mount_path: /media/plusclouds-config # optional, checked silently at boot

log:
  level: info                          # debug | info | warn | error
  format: json                         # json | console
  file: /var/log/plusclouds/agent.log  # leave empty to disable file logging

autoheal:
  enabled: true
  restart_delay: 10s
```

All values can be overridden by environment variables using the prefix `PLUSCLOUDS_AGENT_` (e.g. `PLUSCLOUDS_AGENT_NATS_API_KEY`).

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

## Boot-time provisioning from ISO metadata (pfSense)

On pfSense/FreeBSD, the agent applies two pieces of config from the ISO config-drive metadata (`pc-meta-data.json`) automatically on boot — **not** via a NATS command, since NATS itself is only reachable once the network config is correct. Each step runs at most once, tracked by its own marker file, and retries on the next boot until it succeeds:

| Step | Marker | Behavior |
|---|---|---|
| Network config | `/var/db/plusclouds-agent/netconfig.applied` | Matches each metadata NIC to a local interface by MAC address and sets static IP/subnet/gateway/DNS/MTU on interfaces that are **already assigned** in the base image (wan/lan/optN role assignment is out of scope). Snapshots the live config first; after applying, attempts a real, non-retrying NATS connect as a verification probe. If the platform can't be reached, it **reverts** to the snapshot and leaves the marker unset so it retries next boot — only a verified-successful apply is marked done. |
| Password | `/var/db/plusclouds-agent/password-set.applied` | Sets the `username`/`password` from the metadata as the matching local pfSense user's login password, via the same `pfsense.set_password` machinery available as a NATS command. No reachability risk, so no revert — just retried each boot until it succeeds. |

The two steps are independent — a reverted network change does not block password provisioning, and vice versa. A NIC whose `network.gateway` is `null` in the metadata gets its static IP/subnet set with no gateway/default route configured — the agent never guesses a route.

---

## Platform compatibility

| Feature | Linux | Windows | FreeBSD (pfSense) |
|---|---|---|---|
| NATS connection | ✅ | ✅ | ✅ |
| Telemetry (CPU, RAM, disk, network) | ✅ | ✅ (load_avg = 0) | ✅ (gopsutil) |
| Heartbeat | ✅ | ✅ | ✅ |
| Capabilities event | ✅ | ✅ | ✅ |
| Service management | ✅ systemd/D-Bus | ⚙ stub (SCM planned) | ⚙ stub (rc.d planned) |
| `system.update` | ✅ Ubuntu/Debian | ✗ | ✗ |
| `vm.reboot` / `vm.shutdown` | ✅ `systemctl` | ✅ `shutdown /r` | ✗ (uses `systemctl`, not yet FreeBSD-aware) |
| `pfsense.set_password` | ✗ | ✗ | ✅ pfSense config/auth subsystem |
| Boot-time metadata provisioning (network + password) | ✗ | ✗ | ✅ one-shot, see below |

---

## Security model

- **Outbound-only** — the agent never listens on any port
- **Scoped JWT** — the NATS auth callout issues a JWT granting publish/subscribe only to this agent's own subjects
- **Operation allowlist** — `allowed_operations` in `agent.yaml` is a hard gate; unknown or unlisted operations return `rejected`
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
