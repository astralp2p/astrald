#!/usr/bin/env python3
"""Oracle: after a restart, I's subscription resumes from its last ack, and
only I may consume or delete it.

Claims, each falsifiable on its own:

1. Before the restart, I acked a and received b without acking it, and the
   node sent them in that order with consecutive versions.
2. J's subscription with I's nonce was answered `index not found`, and J was
   sent no change. J's own indexer delivered changes, so J holds the permit
   and the refusal is the owner check.
3. After the restart, I's first message is b at the version it carried
   before: the cursor stands after a, so the unacked b is delivered again and
   J's subscription moved nothing.
4. Then c, d and the unindex of c follow, each at the next version. The
   unindex of c is written by the restart's resync of `local`; a live delete
   emits none (README.md, the probe fact records it).
5. J's unregister with I's nonce answers `index not found`; I's own answers
   an ack, and a subscribe after it answers `index not found`.
"""
import asyncio

import astral

from lib.sessionio import load
from subscription import (ERROR, INDEX, INDEX_NOT_FOUND, UNINDEX,
                          Subscription, answer, change)

# why: the resync of `local` after the restart runs in the background, so the
# unindex of c arrives eventually; a local node writes it in milliseconds
DEADLINE = 30.0

REFUSED = {"type": ERROR, "object": INDEX_NOT_FOUND}


async def resume(i, nonce: str, c: str) -> list[dict]:
    """Subscribe as I and ack every change until the unindex of c arrives,
    the stream ends, or DEADLINE passes."""
    loop = asyncio.get_running_loop()
    end = loop.time() + DEADLINE
    seen = []
    async with Subscription(i, nonce) as sub:
        while loop.time() < end:
            try:
                doc = await sub.next(end - loop.time())
            except TimeoutError:
                break
            if doc is None:
                break
            ch = change(doc)
            seen.append(ch)
            if ch["type"] not in (INDEX, UNINDEX):
                break
            await sub.ack(ch)
            if ch["type"] == UNINDEX and ch["object"] == c:
                break
    return seen


def position(seen: list[dict], kind: str, oid: str) -> int:
    for n, ch in enumerate(seen):
        if ch["type"] == kind and ch["object"] == oid:
            return n
    return -1


def check_first_session(session: list[dict], ids: dict) -> dict:
    """Claim 1. Answers b as the node sent it."""
    assert len(session) >= 2, f"the first session saw only {session}"
    a, b = session[-2:]
    assert (a["type"], a["object"], a.get("acked")) == (INDEX, ids["a"], True), (
        f"the first session's second-to-last change is {a}, not a acked")
    assert (b["type"], b["object"], b.get("acked")) == (INDEX, ids["b"], False), (
        f"the change after a is {b}, not b left unacked")
    assert b["version"] == a["version"] + 1, (
        f"a is version {a['version']} and b is {b['version']}: the stream "
        f"skipped a version")
    return b


def check_owner_only(f: dict) -> None:
    """Claim 2."""
    assert f["j_subscribe"] == [REFUSED], (
        f"J subscribed with I's nonce and was answered {f['j_subscribe']}, "
        f"not only {REFUSED}: a non-owner consumes I's stream")
    probe = f["probe"]
    assert any(ch["type"] == INDEX and ch["object"] == f["ids"]["d"]
               for ch in probe["changes"]), (
        f"J's own indexer never delivered d ({probe}): the control that J "
        f"holds the indexer permit failed")
    assert probe["unregister"]["type"] == "ack", (
        f"J's unregister of its own indexer answered {probe['unregister']}")


def check_resume(seen: list[dict], b: dict, ids: dict) -> None:
    """Claims 3 and 4."""
    assert seen, "I's subscription after the restart delivered nothing"
    first = seen[0]
    assert (first["type"], first["object"], first["version"]) == \
        (INDEX, ids["b"], b["version"]), (
        f"I's first message after the restart is {first}, not b at version "
        f"{b['version']}: the cursor moved past an unacked change, or "
        f"another caller moved it")
    versions = [ch["version"] for ch in seen]
    assert versions == list(range(b["version"], b["version"] + len(seen))), (
        f"versions after the restart are {versions}, not consecutive from "
        f"{b['version']}")
    order = [position(seen, INDEX, ids["c"]), position(seen, INDEX, ids["d"]),
             position(seen, UNINDEX, ids["c"])]
    assert -1 not in order and order == sorted(order), (
        f"index c, index d and unindex c sit at positions {order} of {seen}: "
        f"each must arrive, in that order")


async def main():
    doc = load()
    f = doc["facts"]
    endpoint = doc["nodes"]["node1"]["endpoint"]
    ids, nonce = f["ids"], f["nonce"]

    assert f["enable"]["type"] == "ack", (
        f"indexing.enable_repo answered {f['enable']}")
    b = check_first_session(f["first_session"], ids)
    check_owner_only(f)

    async with await astral.connect(endpoint, token=f["apps"]["I"]["token"]) as i, \
            await astral.connect(endpoint, token=f["apps"]["J"]["token"]) as j:
        seen = await resume(i, nonce, ids["c"])
        check_resume(seen, b, ids)

        unreg = f"indexing.unregister_indexer?nonce={nonce}&out=json"
        j_unregister = await answer(j, unreg)
        assert j_unregister == REFUSED, (
            f"J's unregister of I's indexer answered {j_unregister}, not "
            f"{REFUSED}")
        i_unregister = await answer(i, unreg)
        assert i_unregister["type"] == "ack", (
            f"I's unregister of its own indexer answered {i_unregister}")
        async with Subscription(i, nonce) as sub:
            after = change(await sub.next(10.0) or {"Type": None})
        assert after == REFUSED, (
            f"a subscribe after the unregister answered {after}, not "
            f"{REFUSED}")

    live = position(f["probe"]["changes"], UNINDEX, ids["c"]) >= 0
    print(f"oracle: after the restart I resumed at b (version "
          f"{b['version']}), then {len(seen) - 1} more change(s) in version "
          f"order ending with the unindex of c; J was refused on subscribe and "
          f"unregister; I's unregister acked and later subscribe refused; "
          f"live delete unindexed c before the restart: {live}")


asyncio.run(main())
