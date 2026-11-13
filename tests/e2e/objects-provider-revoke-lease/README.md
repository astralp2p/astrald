# objects-provider-revoke-lease

An app finder that loses its ServeObjects grant is dropped on node1's next
`objects.find`, while a second finder is still asked once per find. A refused
re-registration stays refused until the grant is back. An unrenewed finder or
searcher lease lapses, and re-registering revives it.

- **roster** node1 · **after** `bootstrap-user-software-key` · `host_listener`
- **driver** `script.py` runs `driver/` (Go, on `tests/e2e/internal/provider`)
  in `drive` mode. The driver records and stops only on a wiring fault.
  1. The User registers four apps on node1: P, Q and E with ServeObjects for
     the finder role, Z for the searcher role. P, Q and E each name an
     identity of their own (pid, qid, eid) for any object; Z returns one
     object zx for any search.
  2. P registers as a finder for 120 s; Q registers twice, the second a
     renewal. find(X) answers {pid, qid}.
  3. The User revokes P's grant. find(X) answers {qid}, and P is not asked.
  4. P's `register_finder` is refused with `not permitted`. find(X) answers
     {qid}.
  5. The User grants P ServeObjects[finder] again (`apphost.grant`), and P
     registers. find(X) answers {pid, qid}.
  6. E registers for 3 s. find(X) about 0.5 s later answers {pid, qid, eid}.
     5 s after the grant, find(X) answers {pid, qid}, and E is not asked. E
     registers for 60 s, and find(X) answers {pid, qid, eid}.
  7. Z registers as a searcher for 3 s, and the same three searches answer
     zx from Z, nothing, and zx from Z.
  8. The User revokes all four grants. find(X) and the search answer nothing,
     and no app is asked.
- **oracle** `verify.py`:
  - Each find and search answers exactly its row of the oracle's own table,
    and the apps' counters match it: P 1, 1, 1, 2, 3, 4, 5, 5; Q 1 to 7 and
    7; E 0 until step 6, then 1, 1, 2, 2; Z 1, 1, 2, 2.
  - Q is asked once per find although it registered twice.
  - P's re-registration after the revoke answers exactly `not permitted`;
    after the grant it answers a lease.
  - node1's `astrald.log`, read through `lib/nodefs.py`, shows the same
    dials: 5 `objects.find` to P, 7 to Q, 2 to E, and 2 `objects.search` to
    Z.
  - node1's log holds `external finder <E>: registration expired` and
    `external searcher <Z>: registration expired`, and no expiry for Q.
  - `driver verify` reruns the flow with fresh apps.

## How node1 drops a registration

An external finder or searcher re-authorizes the grant on every fan-out and
removes itself when the grant is gone (`mod/objects/src/external_finder.go`,
`external_searcher.go`). A fan-out skips a registration whose lease has run
out (`find.go`, `search.go`), and the sweep removes it and logs the expiry
(`external_registration.go`). A registration of an identity already
registered renews the entry in place.

The 1 s sweep and the fan-out skip give the same observable here, so removing
only one of them is not caught. A local world renders the 1 s sweep for every
node (`lib/nodeconfig.py`); the manifest sets it for node1 in any world, since
a Docker world renders no objects config.
`registration-lease` covers the describer lease.
