# Firewall & NAT API (for implementers)

This is the API reference for the six NATS operations `pfsense.agent` exposes
for managing a pfSense box's firewall rules and NAT/port-forwards:
`pfsense.firewall.list`, `pfsense.firewall.create`, `pfsense.firewall.delete`,
`pfsense.nat.list`, `pfsense.nat.create`, `pfsense.nat.delete`.

It's written for whoever is implementing a **caller** — e.g. a backend
`GatewayDriverInterface` implementation that talks to this agent over NATS
instead of SSH/REST directly to the box. If you just want the one-line
summary of each operation, see the README's
["Supported operations"](../README.md#supported-operations) table; this
document goes deeper on request/response shapes, field semantics, and error
handling.

There is no REST API and no inbound port on the agent side — everything
here goes over the NATS connection the agent already holds. See
[Transport](#transport) below for the wire format.

---

## Prerequisites

- The six operation names must be present in the target agent's
  `allowed_operations` (set via the config-drive at provisioning time).
  Sending an operation that isn't allowed gets a `rejected` result, not a
  `failed` one — no pfSense-side code runs at all.
- The target must actually be pfSense/FreeBSD. On Linux/Windows agents
  these operations return `failed` with `"pfSense operations are only
  supported on pfSense/FreeBSD"` (they exist in the allowlist schema for
  uniformity but never do anything off pfSense).
- Every operation call reaches live pfSense state directly — there is no
  staging/"apply changes" step. `create`/`delete` take effect immediately
  (`filter_configure()` runs synchronously inside the call) and persist to
  `config.xml` (`write_config()`), so a result of `completed` means the
  change is live, not just queued.

---

## Transport

### Envelope

Every message in either direction is a JSON envelope:

```json
{
  "v":          1,
  "id":         "550e8400-e29b-41d4-a716-446655440000",
  "type":       "command",
  "agent_type": "vm",
  "agent_uuid": "550e8400-e29b-41d4-a716-446655440001",
  "timestamp":  1748000000,
  "reply_to":   "_INBOX.abc123",
  "payload":    { "operation": "pfsense.firewall.list", "params": {} }
}
```

| Field | Description |
|---|---|
| `v` | Protocol version, always `1`. |
| `id` | Caller-generated UUID for the command. Echoed back as `command_id` in the result. |
| `type` | `"command"` for requests; the agent replies with `"result"`. |
| `agent_uuid` | The target agent's UUID. |
| `reply_to` | **Optional.** See [Sync vs. async](#sync-vs-async-request-reply) below. |
| `payload` | For commands: `{operation, params, timeout_s?}`. For results: `{command_id, status, message?, output?}`. |

Subjects: publish commands to `agent.vm.{agent_uuid}.cmd`; async results
arrive on `agent.vm.{agent_uuid}.evt`.

### Sync vs. async request/reply

- **Async (no `reply_to`)**: publish the command envelope, then watch
  `agent.vm.{uuid}.evt` for a `result` envelope whose `payload.command_id`
  matches the `id` you sent.
- **Sync (`reply_to` set)**: set `reply_to` to a unique inbox subject (NATS's
  `Request()` helper does this for you — under the hood it's the same
  mechanism, just point-to-point) and the agent publishes the result
  **directly to that inbox** instead of the shared `.evt` subject. This is
  the simpler integration for a backend driver making one call at a time —
  no separate subscriber/correlation loop needed, just a request/reply with
  a timeout.

Either way, the result's shape is the same (see [Result envelope](#result-envelope-all-operations)).

### Command payload

```json
{
  "operation": "pfsense.firewall.create",
  "params": { "...": "operation-specific, see below" },
  "timeout_s": 30
}
```

`timeout_s` is advisory (client-side wait budget); the agent doesn't enforce
it server-side. All six operations here complete in well under a second on
a healthy box (they're synchronous pfSense config-subsystem calls, no
network round-trip beyond the NATS hop itself).

### Result envelope (all operations)

```json
{
  "v": 1,
  "id": "8f14e...",
  "type": "result",
  "agent_uuid": "550e8400-e29b-41d4-a716-446655440001",
  "timestamp": 1748000001,
  "payload": {
    "command_id": "550e8400-e29b-41d4-a716-446655440000",
    "status": "completed",
    "message": "",
    "output": { "...": "operation-specific, see below" }
  }
}
```

`status` is one of:

| Status | Meaning |
|---|---|
| `completed` | Ran successfully. `output` is populated per-operation (see below). |
| `failed` | Operation was permitted and attempted, but errored — bad params, pfSense-side rejection, rule-not-found on delete, wrong platform, etc. `message` has the reason; `output` is absent. |
| `rejected` | Operation name isn't in this agent's `allowed_operations` (or the params JSON itself was malformed). Nothing ran. `message` has the reason. |

---

## Data model

### Interfaces

`interface` fields (both firewall and NAT) are **pfSense's logical role
names** — `lan`, `wan`, `opt1`, `opt2`, ... — not OS interface names
(`vtnet0`, `em0`, ...) and not the interface's description. This is the
same `Logical` value the boot-time netconfig operations already use.

### Address specs (`source` / `destination`)

A plain string, one of:

| Form | Example | Meaning |
|---|---|---|
| `"any"` (or empty) | `"any"` | Matches anything. |
| Bare IP or CIDR | `"10.0.0.5"`, `"10.0.0.0/24"` | A specific host or subnet. |
| Interface alias | `"lan"` | That interface's configured subnet (pfSense's network shorthand — the same string as the `interface` field can double as an address spec). |

`nat.create`'s `destination` defaults to `"<interface>ip"` (e.g. `"wanip"`)
when omitted — pfSense's shorthand for "this interface's own address",
matching the common port-forward case and the webConfigurator wizard's
default.

### Port ranges

`*_port` fields accept a single port (`"80"`) or a range using a dash
(`"8000-9000"`). Internally pfSense stores ranges with a colon; the agent
translates both directions, so you only ever see/send dashes.

### `tracker` (rule identity)

pfSense filter/NAT entries have no stable caller-facing ID by default. Every
rule created through this API is stamped with a `tracker` — a string of
pfSense's own `round(microtime(true) * 1000)` scheme, i.e. milliseconds
since epoch as an integer string. It's returned by `create` and by every
entry in `list`, and it's what `delete` takes to identify which entry to
remove.

**There is no edit.** To change a rule, `delete` the old `tracker` and
`create` a new one — same delete-then-recreate model the rest of the
gateways-style tooling already uses; there's no partial-update semantics to
get wrong.

---

## Operations

### `pfsense.firewall.list`

No params.

```json
{ "operation": "pfsense.firewall.list", "params": {} }
```

**Output** (`status: completed`):

```json
{
  "rules": [
    {
      "tracker": "1753500000123",
      "interface": "wan",
      "action": "pass",
      "protocol": "tcp",
      "source": "any",
      "source_port": "",
      "destination": "10.0.0.5",
      "destination_port": "443",
      "description": "Allow HTTPS to app server",
      "disabled": false
    }
  ]
}
```

Rules are returned in `config.xml` order (== pfSense's own evaluation
order — first match wins, same as the webConfigurator's Rules tab).

---

### `pfsense.firewall.create`

```json
{
  "operation": "pfsense.firewall.create",
  "params": {
    "interface": "wan",
    "action": "pass",
    "protocol": "tcp",
    "source": "any",
    "destination": "10.0.0.5",
    "destination_port": "443",
    "description": "Allow HTTPS to app server"
  }
}
```

| Field | Required | Notes |
|---|---|---|
| `interface` | yes | pfSense logical name (`lan`/`wan`/`optN`). |
| `action` | yes | One of `pass`, `block`, `reject`. |
| `protocol` | no | e.g. `tcp`, `udp`, `icmp`. Omit or `"any"` for all protocols. |
| `source` | yes | Address spec. |
| `source_port` | no | Port or `low-high` range. |
| `destination` | yes | Address spec. |
| `destination_port` | no | Port or `low-high` range. |
| `description` | no | Free text, shown in the webConfigurator's Description column. |

**Output** (`status: completed`): `{"tracker": "1753500000123"}` — save
this to delete the rule later.

**Failure modes** (`status: failed`): missing `interface`/`action`/`source`/
`destination`; `action` not one of `pass`/`block`/`reject`.

---

### `pfsense.firewall.delete`

```json
{
  "operation": "pfsense.firewall.delete",
  "params": { "tracker": "1753500000123" }
}
```

**Output** (`status: completed`): `{"tracker": "1753500000123"}` (echoed
back for convenience).

**Failure modes**: empty `tracker`; no rule with that `tracker` exists
(e.g. already deleted, or it belongs to a different rule set — `tracker`
values aren't namespaced per caller, so don't reuse/guess one you didn't
get from `create`/`list`).

---

### `pfsense.nat.list`

No params.

```json
{ "operation": "pfsense.nat.list", "params": {} }
```

**Output** (`status: completed`):

```json
{
  "port_forwards": [
    {
      "tracker": "1753500001456",
      "interface": "wan",
      "protocol": "tcp",
      "source": "any",
      "source_port": "",
      "destination": "wanip",
      "destination_port": "8443",
      "target_ip": "10.0.0.5",
      "target_port": "443",
      "description": "HTTPS port-forward to app server",
      "disabled": false
    }
  ]
}
```

---

### `pfsense.nat.create`

```json
{
  "operation": "pfsense.nat.create",
  "params": {
    "interface": "wan",
    "protocol": "tcp",
    "destination_port": "8443",
    "target_ip": "10.0.0.5",
    "target_port": "443",
    "description": "HTTPS port-forward to app server"
  }
}
```

| Field | Required | Notes |
|---|---|---|
| `interface` | yes | The interface traffic arrives on (usually `wan`). |
| `protocol` | yes | One of `tcp`, `udp`, `tcp/udp`. |
| `destination` | no | Address spec; defaults to `"<interface>ip"` (this interface's own address) if omitted — the common case. |
| `destination_port` | yes | The externally-facing port or range. |
| `target_ip` | yes | Internal host to forward to. |
| `target_port` | yes | Port or range on the target. A single-port range must match `destination_port`'s cardinality (pfSense will reject mismatched range sizes at apply time, surfacing as a `failed` result). |
| `source` | no | Address spec restricting which external hosts can hit this forward. Defaults to `any`. |
| `source_port` | no | Port or range. |
| `description` | no | Free text. |

**Output** (`status: completed`): `{"tracker": "1753500001456"}`.

No matching "allow" filter rule is auto-created — pfSense's own default
rules already pass traffic on most interfaces (notably LAN); if the
destination interface's ruleset blocks by default (commonly WAN), pair this
with a `pfsense.firewall.create` call to actually admit the traffic, the
same two-step the webConfigurator itself effectively performs (its "add
associated filter rule" checkbox does this for you there; this API keeps
the two concerns separate and explicit).

**Failure modes**: missing `interface`/`protocol`/`destination_port`/
`target_ip`/`target_port`; `protocol` not one of `tcp`/`udp`/`tcp/udp`.

---

### `pfsense.nat.delete`

```json
{
  "operation": "pfsense.nat.delete",
  "params": { "tracker": "1753500001456" }
}
```

**Output** (`status: completed`): `{"tracker": "1753500001456"}`.

**Failure modes**: same as `firewall.delete` — empty or unknown `tracker`.

---

## Worked example (sync request/reply)

Command, published to `agent.vm.550e8400-e29b-41d4-a716-446655440001.cmd`
with `reply_to` set to a private inbox:

```json
{
  "v": 1,
  "id": "a1b2c3d4-0000-0000-0000-000000000001",
  "type": "command",
  "agent_type": "vm",
  "agent_uuid": "550e8400-e29b-41d4-a716-446655440001",
  "timestamp": 1753500000,
  "reply_to": "_INBOX.xyz789",
  "payload": {
    "operation": "pfsense.firewall.create",
    "params": {
      "interface": "wan",
      "action": "pass",
      "protocol": "tcp",
      "source": "any",
      "destination": "10.0.0.5",
      "destination_port": "443",
      "description": "Allow HTTPS to app server"
    }
  }
}
```

Result, published straight to `_INBOX.xyz789`:

```json
{
  "v": 1,
  "id": "f9e8d7c6-0000-0000-0000-000000000002",
  "type": "result",
  "agent_type": "vm",
  "agent_uuid": "550e8400-e29b-41d4-a716-446655440001",
  "timestamp": 1753500000,
  "payload": {
    "command_id": "a1b2c3d4-0000-0000-0000-000000000001",
    "status": "completed",
    "output": { "tracker": "1753500000123" }
  }
}
```

---

## Implementation notes for a `GatewayDriverInterface`-style caller

- `ListFirewallRules`/`ListPortForwards` → `pfsense.firewall.list` /
  `pfsense.nat.list`, mapping `rules[]`/`port_forwards[]` 1:1 onto your
  domain model.
- `CreateFirewallRule` / `CreatePortForward` → the corresponding `.create`
  call; persist the returned `tracker` as the durable ID for that rule in
  your own storage (it's the only handle you'll have for deleting it
  later).
- `DeleteFirewallRule` / `DeletePortForward` → the corresponding `.delete`
  call keyed by the stored `tracker`.
- There's no dedicated health-check operation for this feature area —
  reachability is already implied by whether the agent's NATS connection is
  up (heartbeat/capabilities), which your platform is presumably already
  tracking generically for every agent. If you need pfSense-specific
  signal beyond connectivity, `system.info`/`system.network` (already
  implemented, see README) cover interface/uptime state.
- Treat `rejected` and `failed` differently in your error handling:
  `rejected` means a config problem on the agent side (operation not
  allowlisted) that a retry won't fix; `failed` means the call reached
  pfSense and something about the request or box state was wrong (bad
  params, unknown tracker) — worth surfacing to the caller, not silently
  retrying either.
