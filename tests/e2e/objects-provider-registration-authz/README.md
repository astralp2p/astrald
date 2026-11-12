# objects-provider-registration-authz

node1 lets an app register as a finder, describer or searcher only when the
app is a local, non-node caller whose ServeObjects grant covers that role. A
refused registrant is never dialed.

- **roster** node1, node2 · **after** `adopt-node` · `local_only`
- **driver** `script.py` runs `driver/` (Go, on `tests/e2e/internal/provider`)
  in `drive` mode:
  1. The User mints eight apps on node1 with `apphost.register`. They differ
     only in their permit: `all` (ServeObjects, unconstrained), `d`
     (describer), `f` (finder), `s` (searcher), `df` (describer and finder),
     `i` (indexer), `store` (StoreObjects only), `none` (no permit). Each app
     serves `objects.find`, `objects.describe` and `objects.search` through
     `apps.ServeWith` with counting handlers.
  2. Each app calls `objects.register_finder`, `register_describer` and
     `register_searcher` with its own token and a 10 s lease: 24 cells.
  3. node1's own token calls the three ops on node1.
  4. node2's own token calls the three ops on node1, over the link.
  5. The User runs `objects.find`, `objects.describe` and `objects.search` on
     node1 at once, for a run-unique id and word, within 8 s of the first
     lease. The driver reads every app's counter for each op.
  6. The User revokes ServeObjects from every app that holds it.
- **oracle** `verify.py`:
  - The cells covered by the grant answer a lease of exactly 10 s: `all` x3,
    `d` describer, `f` finder, `s` searcher, `df` describer and finder. Every
    other cell answers exactly `not permitted`. The oracle holds its own
    table.
  - node1's own token is refused on each op, with `not permitted` or
    `node identity cannot register as an external discoverer`. The text seen
    is recorded, not pinned.
  - node2's token over the link answers exactly `origin not allowed: network`
    on each op.
  - node1 dialed each accepted cell once and every other (app, role) pair
    never.
  - Live, with astral-py: an app with no grant and node1's own token are both
    refused by `objects.register_finder`.
  - `driver verify` reruns the whole matrix with fresh apps.

## Why these texts

The three register ops refuse a network-origin caller before the grant check,
and answer `not permitted` when the grant check fails
(`mod/objects/src/op_register_finder.go`, and the describer and searcher
twins). node2 holds no ServeObjects grant on node1, so without the origin
guard its reply reads `not permitted`. The exact origin text is what proves the
guard fired.

The grant check runs before the node-identity check. node1's own token
authenticates as node1, which holds no ServeObjects grant, so the observed
reply is `not permitted` and the self-registration branch is not reached. The
oracle accepts either text.

## Lingering state

Every accepted lease is 10 s and the apps exit with the driver. The revoke in
step 6 removes each app's grant, so node1 drops the registration on its next
fan-out at the latest.
