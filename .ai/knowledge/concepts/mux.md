# Session Multiplexer

The `Mux` attaches to each `Link`, shares the link's `channel.Channel`, encodes outbound `frames.Frame` objects through it, and dispatches inbound objects via `Mux.Handle`. One brontide handshake carries many sessions; see [transport](transport.md) for the layer stack.

## Multiplexing Invariants

* **Cost**: reuse one `Link` per identity to amortize the handshake, NAT hole-punch, and Tor circuit cost.
* **Symmetry**: after link establishment, either side may send a `Query` or `RelayQuery` frame.
* **Isolation**: `Nonce` identifies the session; flow control, buffer state, and close/reset are per-session, so one slow reader does not stall unrelated sessions.
* **Router gate**: `Mux.SetRouter` is one-shot; inbound queries block in `waitRouter` until `LinkPool.AddLink` wires the node router.

## Flow control

Session flow-control lives here; `links.md` does not duplicate it.

* Each session has independent credit, expressed by `OutputBuffer.wsize`.
* `OutputBuffer.Write` consumes `wsize` and dispatches via the per-session send callback, which chunks at `maxPayloadSize` (8 KB) and emits `Data` frames.
* When `wsize == 0`, `Write` returns `ErrBufferEmpty` and `muxSessionWriter` waits on the buffer's wake channel.
* `muxSessionReader` drains the `InputBuffer`; each `Read` triggers the per-session `onRead` callback, which sends a `Read` frame granting that many bytes.
* `defaultBufferSize` is 4 MB.

## Session migration

A session migrates from one `Link` to another while it stays open and in-flight. The current link's pressure detector decides when to upgrade.

Upgrade selection (`connectivityUpgrade`):

1. Prefer any existing sibling link to the same peer with no pressure detector or no active pressure.
2. Otherwise force a new link with `StrategyNAT` and `WithForceNew()`.

Eligible sessions are then migrated; a session is eligible at age >= 30 s or bytes >= 1 MB.

### Preserved State

* Session `Nonce`; the application-level pipe identity does not change.
* Already-buffered unread data in the old `InputBuffer`; the reader drains it before switching to the next buffer.
* Query string, peer identities, and the cumulative `bytes` counter.

### Replaced State

* `muxSessionReader.buf` swaps to a new `InputBuffer` after the old one drains; `SetNextBuffer` queues it and `Advance` closes the current one.
* `muxSessionWriter.buf` swaps via `SwapBuf` to a new `OutputBuffer` bound to the new link's send callback; writes pause until `Resume`.
* The session moves between mux session maps when the link changes (`oldMux.removeSession` then `newMux.addSession`).

## Migration Protocol

Migration is signalled over a separate `nodes.migrate_session` query channel using `MigrateSignal` objects, while `frames.Migrate` runs on each link's mux to delimit the data hand-off. Both peers send a `Migrate` frame.

Initiator (`migrateSession`):

1. Open `nodes.migrate_session` to the peer with the target `LinkID`.
2. `migrator.Begin(target)`: CAS `stateOpen -> stateMigrating`, pause the writer, swap its buffer to the new link, and queue the new `InputBuffer` as `nextBuffer`.
3. Send `MigrateSignalReady` with the local buffer size.
4. Wait for `MigrateSignalSwitched` and record the peer buffer.
5. Send a `Migrate` frame on the old link; this tells the peer all old-link data has been emitted.
6. `WaitClosed`: block until the old `InputBuffer` is closed (the peer's `handleMigrate` calls `reader.Advance`, which closes it once empty).
7. Send `MigrateSignalResume`, then wait for `MigrateSignalDone`.
8. `migrator.Complete`: move the session between mux maps, `stateMigrating -> stateOpen`, grow writer credit to the peer buffer, resume the writer.

Responder (`OpMigrateSession`):

1. Receive `MigrateSignalReady`, call `migrator.Begin`.
2. Send a `Migrate` frame on the old link.
3. Send `MigrateSignalSwitched` with the local buffer size.
4. `WaitClosed`, then receive `MigrateSignalResume`.
5. `migrator.Complete`, then send `MigrateSignalDone`.

The session stays open; the application-facing `io.ReadWriter` keeps working over the new carrier. `migrateSessionTimeout` (30 s) caps each migration; auto-migrations also fail if the session is not yet eligible.
