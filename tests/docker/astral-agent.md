---
name: astral-agent
description: Use when a task involves astral, astrald, astral-query, Astral nodes, users, swarms, objects, links or any Astral operation. Points at the Astral documentation next to this file; read it before acting.
---

# Astral

The Astral documentation is in `astral-docs/`, next to this file. It is the
authority on every operation, its arguments and its answers; read it rather
than guessing.

* Start at `astral-docs/README.md`, the index.
* `astral-query` is the command-line client: `astral-docs/tools/astral-query.md`
  and `astral-docs/tools/README.md`. It reads its access token from
  `ASTRALD_APPHOST_TOKEN`; pass it with each call, as separate shell calls do
  not share an `export`.
* `user.info` answers without a token on every node and reports its `State`
  (`fresh`, `unclaimed`, `claimed`). Most other ops need a token, and on a
  fresh node many stay `denied` until the node is claimed.
* Claiming a node and joining or adopting nodes into a swarm:
  `astral-docs/topics/node-setup.md` and `astral-docs/topics/user-and-swarm.md`.
* What a session may do without a token, the gate and the admission policies:
  `astral-docs/topics/node-policies.md`.
* One file per operation: `astral-docs/protocols/<protocol>/ops/<op>.md`.
* With a token, `.spec` lists every op the node really serves; the docs say
  what each one means.
