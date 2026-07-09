# mod/nat

Establishes direct UDP paths between nodes behind cone NATs and exposes the resulting holes as a consumable pool for link strategies. Owns the punch signalling protocol, cone-NAT probe socket, keepalive and lock state for each hole, and the persisted enablement setting that gates NAT availability.

## Dependencies

| Module | Why |
|---|---|
| `dir` | resolves `nat.punch` targets and `nat.node_consume_hole` peer IDs with `ResolveIdentity` |
| `ip` | `PublicIPCandidates()` gates module enablement and supplies the local IPv4 address used in punch signals |
| `tree` | binds `Settings.Enabled` at `/mod/nat/settings`; `Run` follows that tree value to re-evaluate availability |
| `objects` | registers the module as an object receiver so endpoint observation events can refresh enablement |
| `events` | supplies the event object type consumed by `ReceiveObject` |
| `core/assets` | loader and dependency injection infrastructure for module construction |

## Flows

- Enablement: `LoadDependencies` binds `/mod/nat/settings` -> `Run` follows `settings.Enabled` -> `evaluateEnabled` combines nil-or-true setting with at least one public IP candidate -> `SetEnabled` swaps the atomic flag and wakes waiters.
- Initiator punch: `nat.punch` -> `getLocalIPv4` -> resolve target -> create cone puncher -> client opens remote `nat.node_punch` -> offer, answer, ready, go, result exchange -> close puncher -> `addHole(active=true)` -> return `*nat.Hole`.
- Participant punch: `nat.node_punch` -> `getLocalIPv4` -> receive offer -> open cone puncher -> send answer -> receive ready -> send go -> burst UDP probes -> exchange result -> close puncher -> `addHole(active=false)`.
- Cone probing: `newConePuncher` opens a UDP socket -> `HolePunch` probes the peer IP across the guessed port window -> receiver accepts packets matching the session token -> returns the observed endpoint pair.
- Add hole: `addHole` wraps the wire `nat.Hole` in runtime `Hole` -> binds the local UDP port -> starts keepalive -> inserts into `HolePool`; duplicate nonces are logged and rejected by the pool.
- Consume as initiator: `nat.node_consume_hole` with `Target` -> `pool.Take` deletes the hole -> resolve peer -> `BeginLock` -> remote `NodeConsumeHole` handshake -> `WaitLocked` -> send `Ack`.
- Consume as responder: `nat.node_consume_hole` without `Target` -> `pool.Take` deletes the hole -> receive `lock` -> `BeginLock` -> send `locked` -> receive `take` -> send `taken` and the `*nat.Hole`.
- Keepalive and lock: idle pinger sends periodic pings -> missing pongs expire the hole -> `BeginLock` moves idle to locking, drains pings, closes the UDP socket, and closes `lockedCh` when locked.
- List holes: `nat.list_holes` optionally resolves `with` -> snapshots pool entries -> filters by peer identity -> streams `*nat.Hole` -> sends `EOS`.
- Service discovery: `DiscoverServices` publishes a `nat` service update reflecting `mod.enabled`; `ReceiveObject` watches observed endpoint events and re-runs enablement against the current public IP candidates.

## Source

- `mod/nat/module.go`, `mod/nat/errors.go` - `ModuleName`, the (empty) public `Module` interface, node-side sentinels, and the `init()` that registers the `nat.PunchSignal` / `nat.ConsumeHoleSignal` blueprints via `astral.Add`. The `Hole`/`Endpoint` wire objects, `PunchSignal`/`ConsumeHoleSignal` protocol signals, puncher/punch-protocol types, `Method*` op-name constants, and typed clients live in astral-go `api/nat` (see astral-go .ai/knowledge/api/nat.md).
- `mod/nat/src/loader.go`, `mod/nat/src/deps.go`, `mod/nat/src/module.go` - loader, settings binding, enablement evaluation, router setup, hole creation.
- `mod/nat/src/hole.go`, `mod/nat/src/hole_pool.go` - runtime hole state machine and one-consumer pool semantics.
- `mod/nat/src/cone_nat_puncher.go`, `mod/nat/src/frames.go` - UDP probe loop, receiver filtering, ping and probe codecs.
- `mod/nat/src/op_punch.go`, `mod/nat/src/op_node_punch.go`, `mod/nat/src/op_node_consume_hole.go`, `mod/nat/src/op_list_holes.go`, `mod/nat/src/op_enable.go` - query handlers.
- `mod/nat/src/object_receiver.go`, `mod/nat/src/service_discoverer.go` - observed endpoint event handling and service advertisement.
- `mod/nat/views/endpoint_view.go` - endpoint presentation.
- `mod/nodes/src/nat_link_strategy.go` - primary consumer that turns holes into KCP-backed node links.

## Surface

| What | Why it matters |
|---|---|
| `nat.punch`, `nat.node_punch` | initiator and participant halves of UDP traversal |
| `nat.node_consume_hole` | transfers an idle hole from the NAT pool into another transport such as KCP |
| `nat.list_holes`, `nat.set_enabled` | inspection and persisted enablement control |
| `HolePool` | in-memory pool keyed by nonce; `Take` and `TakeAny` delete on access |
| `services.Discoverer` | advertises NAT availability to peers when enabled |
| `objects.Receiver` | re-evaluates NAT enablement after observed endpoint events |

## Invariants

- `pool.Take` and `pool.TakeAny` delete the hole on success; a hole has exactly one consumer.
- `pool.Add` rejects duplicate nonces with `ErrDuplicateHole`.
- Enabled means `settings.Enabled` is nil or true and at least one public IPv4 candidate is available.
- `BeginLock` is the only legal idle-to-locking transition and must happen before `WaitLocked`.
- `Expire` always closes `lockedCh`; `WaitLocked` returns `ErrHoleCantLock` if the final state is not locked.
- `finalizeLock` closes the UDP socket; the consumer must rebind the same local port if it wants to reuse the mapping.
- Punch sessions are 16 bytes; a mismatched session on any non-offer signal aborts the protocol.
- The cone puncher probes only the configured port-guess window around the announced port, so symmetric NAT is expected to fail.
