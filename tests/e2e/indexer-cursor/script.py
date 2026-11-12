#!/usr/bin/env python3
"""Driver: an indexer acks one change, leaves one unacked, and a second app
tries to take over its subscription.

`indexing.subscribe` delivers each change of an enabled repository at least
once, in version order, and commits the indexer's persisted cursor only after
an ack that names the delivered repository and version
(mod/indexing/src/op_subscribe.go). Only the identity that registered the
indexer subscribes; any other caller is answered `index not found`.

The flow, as the User and two apps I and J, each registered with an
unconstrained `mod.auth.serve_objects_action` grant, which covers the indexer
role (`register_indexer` and `subscribe` both require it):

1. I registers an indexer, and the User enables `local` with
   `indexing.enable_repo`. I drains the snapshot of what `local` already
   holds, acking each change, so the objects below are the newest changes.
2. The User stores a, b and c. I subscribes, acks a, receives b, and closes
   the stream without acking b.
3. The User stores d and deletes c.
4. J registers an indexer of its own and drains it. This is the control that
   J holds the permit, and it records whether the live delete of c produced
   an `indexing.unindex`. J then unregisters its own indexer.
5. J subscribes with I's nonce, and acks any change it is sent, as a
   subscriber that took over the stream would.

The harness then restarts node1, and verify.py subscribes as I with the
token recorded here.
"""
import asyncio
import os

import astral
from astral.object import Query

from lib.sessionio import load, write_facts
from lib.indexing import (INDEX, UNINDEX, Subscription, answer, change,
                          drain, register_indexer)

SERVE = "mod.auth.serve_objects_action"
REPO = "local"
INDEXER = "indexer-cursor"
PROBE = "indexer-cursor-probe"

# why: the snapshot sync runs in a goroutine started by enable_repo, and the
# subscription waits silently once caught up, so silence is the only signal
# that the backlog is consumed. A local node writes a change in milliseconds.
IDLE = 3.0

# why: a change already in the index arrives at once; this bounds the wait
# for one the node never sends
STEP = 10.0


async def store_one(u, name: str, tag: str) -> str:
    """Store one typed record in `local`, alone, so store order is version
    order."""
    [oid] = await u.objects.store(
        Query(query_string=f"indexer-cursor {tag}: object {name}"), repo=REPO)
    return str(oid)


async def first_session(i, nonce: str, a: str) -> list[dict]:
    """Subscribe as I, ack every change up to and including a, receive the
    next one, and close without acking it."""
    seen = []
    async with Subscription(i, nonce) as sub:
        while True:
            doc = await sub.next(STEP)
            if doc is None:
                return seen
            c = change(doc)
            seen.append(c)
            if c["type"] not in (INDEX, UNINDEX):
                return seen
            if seen[-2:-1] and seen[-2]["object"] == a and seen[-2]["acked"]:
                c["acked"] = False
                return seen
            await sub.ack(c)
            c["acked"] = True


async def hostile_subscribe(j, nonce: str) -> list[dict]:
    """Subscribe as J with I's nonce, and ack whatever change it is sent."""
    seen = []
    async with Subscription(j, nonce) as sub:
        while True:
            try:
                doc = await sub.next(IDLE if seen else STEP)
            except TimeoutError:
                return seen
            if doc is None:
                return seen
            c = change(doc)
            seen.append(c)
            if c["type"] not in (INDEX, UNINDEX):
                return seen
            await sub.ack(c)


async def main():
    doc = load()
    endpoint = doc["nodes"]["node1"]["endpoint"]
    user = doc["facts"]["user_token"]
    tag = os.urandom(8).hex()

    async with await astral.connect(endpoint, token=user) as u:
        tokens = {k: await u.apphost.register(name=f"indexer-cursor-{k.lower()}",
                                              grant_permits=[SERVE])
                  for k in "IJ"}
    apps = {k: {"identity": str(t.identity), "token": t.token}
            for k, t in tokens.items()}

    async with await astral.connect(endpoint, token=apps["I"]["token"]) as i, \
            await astral.connect(endpoint, token=apps["J"]["token"]) as j, \
            await astral.connect(endpoint, token=user) as u:
        nonce = await register_indexer(i, INDEXER)
        enable = await answer(u, f"indexing.enable_repo?repo={REPO}&out=json")
        backlog = await drain(i, nonce, IDLE)

        ids = {n: await store_one(u, n, tag) for n in "abc"}
        session = await first_session(i, nonce, ids["a"])

        ids["d"] = await store_one(u, "d", tag)
        await u.objects.delete(REPO, ids["c"])

        probe_nonce = await register_indexer(j, PROBE)
        seen = await drain(j, probe_nonce, IDLE)
        probe = {
            "changes": [c for c in seen if c.get("object") in ids.values()],
            "count": len(seen),
            "unregister": await answer(
                j, f"indexing.unregister_indexer?nonce={probe_nonce}&out=json"),
        }

        j_subscribe = await hostile_subscribe(j, nonce)

    write_facts({
        "apps": apps,
        "nonce": nonce,
        "ids": ids,
        "enable": enable,
        "first_session": session,
        "probe": probe,
        "j_subscribe": j_subscribe,
    })
    live_unindex = any(c["type"] == UNINDEX and c["object"] == ids["c"]
                       for c in probe["changes"])
    print(f"driver: enable -> {enable['type']}; drained {len(backlog)} "
          f"backlog change(s); first session {[(c['type'], c.get('version'), c.get('acked')) for c in session[-2:]]}; "
          f"probe saw {probe['count']} change(s), live unindex of c: "
          f"{live_unindex}; J on I's nonce -> {j_subscribe[:1]}")


asyncio.run(main())
