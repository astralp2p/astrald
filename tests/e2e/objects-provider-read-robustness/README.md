# objects-provider-read-robustness

With bad object-provider candidates in play, node1 still serves a swarm read
from the real holder, names each identity once in `objects.find`, rejects an
unheld object within its timers, and replaces a false `SourceID` on a
descriptor or search result with the provider's own identity.

- **roster** node1, node2 · **after** `adopt-node` · `host_listener`
- **driver** `script.py` stores a unique raw blob O1 on node2 alone, as the
  User on node1, then runs `driver/` (Go, on `tests/e2e/internal/provider`) in
  `drive` mode. The Go driver records and stops only on a wiring fault.
  1. The User registers six apps on node1 and the driver makes one identity:
     - F, a finder (ServeObjects, unconstrained) whose claim list is mutable;
     - H, a finder (ServeObjects, unconstrained) that accepts `objects.find`
       and holds it open;
     - L, a liar (ServeObjects, roles describer and searcher) that puts
       node2's identity as `SourceID` on what it answers;
     - G, an app with no permit that accepts `objects.read` and writes
       nothing;
     - D, an app that is registered and never served;
     - R, an identity no node knows.

     F and H register with `objects.register_finder`, L with
     `register_describer` and `register_searcher`, each for 60 s.
  2. Phase A, F claims `[G, G, node2, node2, node1, D, R]`. The User runs
     `objects.find` for O1 (zone `dvn`), `objects.read` for O1 with no
     repository, and `objects.read` for O2, an object nobody stores.
  3. Phase B, F claims `[node1, node2, D, R]` and H answers at once. The User
     reads O2 again.
  4. The User runs `objects.describe` for O1 and `objects.search` for a
     unique word; L answers a labelled descriptor and a result for an object
     X, both claiming node2 as source.
  5. The User revokes ServeObjects from F, H and L.
- **oracle** `verify.py`:
  - Ground truth through astral-py: node2's `local` repository holds O1, and
    its bytes hash to O1; node1's `local` does not hold O1; neither node's
    `main` holds O2 (zone `dv`).
  - read(O1) returns node2's bytes in under 10 s.
  - find(O1) names no identity twice and node2 once, in under 25 s. G and R
    are named too, since finder claims are relayed unverified.
  - Phase A read(O2) is rejected in under 25 s. G is dialed once for O2
    although F claims it twice, and node1 closes G's silent attempt in under
    20 s.
  - Phase B read(O2) is rejected with code 1, the plain rejection of
    `objects.read`, in under 8 s.
  - The one descriptor carrying L's label, and the one result for X, carry
    L's identity.
  - `driver verify` reruns the flow with fresh apps and a fresh O1' that the
    oracle stores on node2.

A wiring fault fails the driver: F or H never asked for O1, or G never dialed
within 2 s of read(O1). Without them the flow says nothing about a bad
candidate.

## Timers

- H holds every find until node1's 15 s external-discoverer timeout ends it
  (`mod/objects/src/module.go`), so find(O1) takes about 15 s.
- node1 tries read candidates concurrently and takes the first that delivers
  a byte (`mod/objects/src/op_read.go`). node2 wins read(O1) at once and the
  losers are cancelled.
- G accepts and stays silent. node1's 15 s first-byte timer ends that
  attempt; G records when node1 closed it (`provider.Holds`).
- `objects.read` accepts its query only once a candidate has won. The op
  router rejects a query not accepted within 5 s with code 6, `CodeTimeout`
  (astral-go `lib/routing/op.go`). Phase A read(O2) is therefore rejected at
  about 5 s with code 6, while node1 keeps trying G and waiting on H in the
  background until about 15 s. The oracle bounds that read at 25 s and does
  not pin its code. The caller cannot see the 15 s timer, so G's hold is
  what checks it.

## Lingering state

Every registration is a 60 s lease. The apps exit with the driver, and the
revoke in step 5 removes their grants, so node1 drops the registrations on
its next fan-out at the latest.
