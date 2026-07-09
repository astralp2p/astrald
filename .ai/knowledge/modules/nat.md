# mod/nat

Establishes direct UDP paths between nodes behind cone NATs and exposes the resulting holes as a consumable pool for link strategies. Owns the hole state machine, cone-NAT probe socket, and the persisted enablement setting. The primary consumer is `mod/nodes/src/nat_link_strategy.go`, which turns a hole into a KCP-backed link.

## Dependencies

| Module | Why |
|---|---|
| `dir` | resolves `nat.punch` targets and `nat.node_consume_hole` peer IDs |
| `ip` | `PublicIPCandidates()` gates enablement and supplies the local IPv4 used in punch signals |
| `tree` | binds `Settings.Enabled` at `/mod/nat/settings`; `Run` follows it to re-evaluate availability |
| `objects` | registers an object receiver so endpoint-observation events refresh enablement |
| `events` | event object type consumed by `ReceiveObject` |

## Invariants

* `pool.Take` and `pool.TakeAny` delete the hole on success; a hole has exactly one consumer.
* `pool.Add` rejects duplicate nonces with `ErrDuplicateHole`.
* Enabled means `settings.Enabled` is nil or true AND at least one public IPv4 candidate exists.
* `BeginLock` is the only idle-to-locking transition and must precede `WaitLocked`.
* `Expire` always closes `lockedCh`; `WaitLocked` returns `ErrHoleCantLock` if the final state is not locked.
* `finalizeLock` closes the UDP socket; the consumer must rebind the same local port to reuse the mapping. This finalizeLock-then-rebind rule is what makes the `nat.node_consume_hole` handoff of an idle hole to KCP work.
* The punch session token is 16 bytes; a mismatched token on any non-offer signal aborts the protocol.
* The cone puncher probes only the guessed port window around the announced port, so symmetric NAT fails by design.
