# app-preexisting-provider-sync

A provider app registered and advertising on a node before that node is
adopted into the User's swarm becomes discoverable with `reach=swarm`, and
callable, from an app on the User's node once the node is adopted.

- **roster** `ppsyncuser`, `ppsyncjoiner`: a root test with a roster of its
  own, so it needs no shared two-node world. Both nodes start fresh;
  `ppsyncuser` is claimed by a User the driver creates, `ppsyncjoiner` is
  never claimed until the adoption. Names no other test uses.
- **local_only** the provider app serves its ops from the host, on a loopback
  port the node dials.
- **driver** `script.py`, with a Go program in `driver/` (this repository's
  pinned astral-go):
  1. astral-py creates a software User key on `ppsyncuser` and activates its
     node contract (the `bootstrap-user-software-key` steps);
  2. `ppsyncjoiner` reports state `fresh` (`joiner_state_before`);
  3. the Go `provider` mode registers an app on `ppsyncjoiner` with ServeApps,
     serves the `ppsync_echo` op through `apps.ServeWith`, advertises the
     `ppsync-echo` service, and stays up;
  4. only then, astral-py links the two nodes over loopback and the User adopts
     `ppsyncjoiner` (the `adopt-node` steps); the joiner reports `claimed`
     (`joiner_state_after`);
  5. the Go `consumer` mode registers an app on `ppsyncuser`, has the node's
     admin grant it discovery of `ppsync-echo`, and runs `services.discover`
     with `reach=swarm` for up to 30 s until the provider shows
     (`discovered_after_adoption`: the provider alone, outcome complete);
  6. the consumer calls `ppsync_echo` with `msg=hello` and gets `hello` back
     (`provider_echoed`); `apphost.hosts` on `ppsyncuser` names `ppsyncjoiner`
     alone as the provider's host (`user_node_knows_the_host`);
  7. the provider stops and reports the callers it answered
     (`provider_served`).
- **oracle** `verify.py` judges the recorded steps, then live state: the
  joiner was `fresh` before and is `claimed` after; the provider served the
  consumer alone; `ppsyncuser` lists the joiner as a linked swarm member;
  `apphost.hosts` on `ppsyncuser` still names the joiner for the provider
  (the relay contract is in the User node's index); the joiner answers
  `user.info` for the same User. The provider is stopped by then, so the
  story is not replayed.

The provider's relay contract can only reach `ppsyncuser` through the first-link
sync, `syncAppContracts` in `mod/user/src/sync.go` (run by
`SyncNodesTask`): the push in `apphost.register` ran when the joiner had no
siblings. The offering itself reaches swarm discovery through the member
carry in `mod/services/src/swarm.go`.

**Known race, not depended on.** The first call after discovery can fail with
`route_not_found` while the provider's relay contract trails the discovery
update
(https://wiki.satforge.dev/doc/swarm-discovery-first-connection-to-a-new-provider-fails-with-route_not_found-taD5d1d8Zn).
The consumer calls once straight after discovery. If that call fails with
`route_not_found`, the driver records `first_call_route_not_found` and
`first_call_error`, waits until `apphost.hosts` on `ppsyncuser` names the
joiner, and retries once. The oracle does not judge the fact; a run of this
host records it as `false`, so the retry is a guard only.

The flow runs services calls in Go because astral-py speaks the pre-v1
Services wire. Spec: `.ai/system/protocols/services/ops/services.discover.md`.
