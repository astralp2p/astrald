# app-blueprint-sync

An app on node2 with Go types no SDK ships syncs their blueprints to node2 at
start, advertises a service whose offering info uses one of them, and answers
a query with an object of the other; a consumer app on node1 decodes both.

- **env** node · **start** `two-nodes` · **saves** —
- **driver** `script.py` — runs `driver/` (Go, this repository's pinned
  astral-go) in `drive` mode. The provider is a child process of its own
  (`driver provide`): the types live in the process-wide registry, and the
  consumer must be a process that never had them.
  1. the provider registers on node2 with ServeApps, adds `Offer`
     (`Label string16`, `Tier uint8`) and `Reading` (`Label string16`,
     `Serial uint64`) to its registry, and serves through `apps.ServeWith`
     with the hooks in this order: `apps.WithBlueprintSync()`, a look at
     `objects.blueprints` on node2 (`types_held_before_advertise`), then the
     advertisement of `app-blueprint-sync-offer` whose info is an `Offer`;
     it answers the `app-blueprint-sync-reading` op with a `Reading`;
  2. the consumer registers on node1 and waits until `apphost.hosts` on node1
     names node2 as the provider's host;
  3. before anything is learned, the consumer calls the op and fails to
     decode the reply (`reply_before_learning_error`);
  4. as the User, the driver pulls both blueprints from node2 with
     `objects.get_blueprint` and registers them in the consumer's process
     only; node1 holds neither (`node1_held_types_at_reply`), and the reply
     decodes (`reply_*`);
  5. as the User, the driver registers both on node1 with
     `objects.register_blueprint`; the consumer's `services.discover`
     (`reach=swarm`) now finds the provider alone, complete, and the info
     decodes (`found_the_provider`, `info_*`).
- **oracle** `verify.py` — the facts above, and the nodes' own state: node2
  and node1 each list both types in `objects.blueprints` and return, from
  `objects.get_blueprint`, the field names and primitive types the app
  declared.

## Where decoding needs the blueprint

Measured on this host, not assumed:

- **The process that reads the typed reply.** Node1 relays the reply as bytes.
  The reply decodes in the consumer's process while node1 holds neither
  type. Without the schema the read fails with `blueprint not found:
  app_blueprint_sync.reading`.
- **Every node that decodes offering info on its way.** Discovery with
  `reach=swarm` is incomplete (`services.incomplete`, no provider) until node1
  holds the schemas, even when the consumer's process holds them already.
  Node1 decodes node2's `services.update` before it re-encodes it for the
  app. The driver records that run as `discover_with_types_in_process_only_found`
  and the oracle prints it without judging it: the ticket
  "Run blueprint sync first and fail on a changed ObjectID" lists, as later
  work, rejecting an advertisement whose types the node cannot decode in place
  of `incomplete`.
- **The provider's own node** holds the schemas from the sync, before the
  advertisement.

An app of node1 is not permitted to read blueprints on node2
(`objects.get_blueprint` target node2 answers `not permitted`), so the User
pulls them, as in `blueprints-two-node`, and the consumer then asks its own
node for them.

## Out of scope

Duplicate and conflicting registration of a type, and the order of the sync
against other registration hooks beyond this test's own, belong to the ticket
named above. This test registers each type once and puts the advertisement
after the sync.

The type and service names are unique to this test, so no other test in the
world observes them. The blueprints stay on node1 and node2 afterwards; nothing
else uses the names.
