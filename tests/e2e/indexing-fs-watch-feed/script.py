#!/usr/bin/env python3
"""Driver: a watched directory feeds a subscribed indexer through live
fsnotify events.

The flow, as the User and one app I with an unconstrained
`mod.auth.serve_objects_action` grant (it covers the indexer role):

1. Write seed.txt (c1) into W, then attach W with `fs.new_watch`, persisted,
   and wait until the repository contains c1.
2. I registers an indexer, and the User enables the repository with
   `indexing.enable_repo`. I subscribes, receives Index c1, and acks it.
3. Write f2.txt = c2 and receive Index c2. Write f3.txt = c2 again and wait
   until `objects.describe` places c2 at f3.txt: a second holder, no change.
4. Write f4.txt = c4 and receive Index c4. Rewrite f4.txt in place,
   truncating, with c4p and receive Index c4p.
5. Delete f2.txt (f3.txt still holds c2), then seed.txt.

After each step the driver keeps reading for four seconds and records what
arrives in `live_tail`. Nothing asserts on it: whether a live delete writes an
Unindex is not this test's claim. Every change received is acked and kept in
`live`, in receipt order; a change that never arrives is recorded as missing,
and the oracle judges. The subscription then closes, and the runner restarts
node1.

Every new file is created empty, then written 0.5 s later: on macOS kqueue a
file created and written in one go can report only its Create before the
per-file watch exists, and a Create indexes nothing.
"""
import asyncio
import os
import time
from pathlib import Path

import astral

from lib import fsfixture, nodefs
from lib.indexing import (INDEX, UNINDEX, Subscription, answer, change,
                          register_indexer)
from lib.sessionio import load, write_facts

SERVE = "mod.auth.serve_objects_action"
REPO = "indexing-fs-watch-feed"
INDEXER = "indexing-fs-watch-feed"

# why: the write debounce is 3 s, then the indexer hashes on a rate-limited
# queue; this bounds the wait for a change the node never sends
STEP = 15.0

# why: a duplicate holder writes no change, so only silence shows it was
# taken in; four seconds outlast one debounce
TAIL = 4.0

# why: kqueue's per-file watch must exist before the write
CREATE_PACE = 0.5

INDEX_DEADLINE = 20.0


class Feed:
    """I's live subscription: every change is acked and recorded."""

    def __init__(self, sub: Subscription):
        self.sub = sub
        self.live: list[dict] = []
        self.tail: list[dict] = []
        self.started = time.monotonic()

    async def _take(self, timeout: float, step: str) -> dict | None:
        try:
            doc = await self.sub.next(timeout)
        except TimeoutError:
            return None
        if doc is None:
            raise AssertionError(f"{step}: the subscription ended")
        c = change(doc)
        c["step"] = step
        c["at"] = round(time.monotonic() - self.started, 1)
        self.live.append(c)
        if c["type"] in (INDEX, UNINDEX):
            await self.sub.ack(c)
        return c

    async def expect(self, step: str) -> dict | None:
        """The next change, or None when none arrives within STEP s."""
        return await self._take(STEP, step)

    async def drain_tail(self, step: str) -> None:
        loop = asyncio.get_running_loop()
        end = loop.time() + TAIL
        while (left := end - loop.time()) > 0:
            c = await self._take(left, step)
            if c is None:
                return
            self.tail.append(c)


def write_paced(n1: dict, rel: str, data: bytes) -> None:
    nodefs.write_bytes(n1, rel, b"")
    time.sleep(CREATE_PACE)
    nodefs.write_bytes(n1, rel, data)


async def wait_located(c, oid: str, path: str, present: bool) -> bool:
    loop = asyncio.get_running_loop()
    end = loop.time() + STEP
    while True:
        paths = [p for p, _ in await fsfixture.file_locations(c, oid)]
        if (path in paths) == present:
            return True
        if loop.time() >= end:
            return False
        await asyncio.sleep(0.5)


async def run_steps(u, feed: Feed, n1: dict, w: str, payloads: dict) -> dict:
    """Steps 2 to 5; answers what each wait observed."""
    seen = {"c1": await feed.expect("seed")}
    await feed.drain_tail("seed")

    write_paced(n1, f"{w}/f2.txt", payloads["c2"])
    seen["c2"] = await feed.expect("f2")
    await feed.drain_tail("f2")

    write_paced(n1, f"{w}/f3.txt", payloads["c2"])
    seen["f3_located"] = await wait_located(
        u, fsfixture.object_id(payloads["c2"]), nodefs.path(n1, f"{w}/f3.txt"),
        True)
    await feed.drain_tail("f3")

    write_paced(n1, f"{w}/f4.txt", payloads["c4"])
    seen["c4"] = await feed.expect("f4")
    await feed.drain_tail("f4")

    nodefs.write_bytes(n1, f"{w}/f4.txt", payloads["c4p"])
    seen["c4p"] = await feed.expect("f4-rewrite")
    await feed.drain_tail("f4-rewrite")

    nodefs.remove(n1, f"{w}/f2.txt")
    seen["f2_unlocated"] = await wait_located(
        u, fsfixture.object_id(payloads["c2"]), nodefs.path(n1, f"{w}/f2.txt"),
        False)
    await feed.drain_tail("f2-delete")

    nodefs.remove(n1, f"{w}/seed.txt")
    seen["seed_unlocated"] = await wait_located(
        u, fsfixture.object_id(payloads["c1"]),
        nodefs.path(n1, f"{w}/seed.txt"), False)
    await feed.drain_tail("seed-delete")
    return seen


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    endpoint, user = n1["endpoint"], doc["facts"]["user_token"]

    session_dir = Path(os.environ["ASTRAL_TESTS_SESSION"]).resolve().parent
    stamp = session_dir.parent.name
    # why under node1's root: the watched directory must live on node1's
    # machine, which in a Docker world sees no host directory
    w = f"{REPO}/watched"
    nodefs.mkdir(n1, w)
    w_path = nodefs.path(n1, w)
    payloads = {k: f"{REPO} {stamp} payload {k}\n".encode()
                for k in ("c1", "c2", "c4", "c4p")}
    nodefs.write_bytes(n1, f"{w}/seed.txt", payloads["c1"])
    c1 = fsfixture.object_id(payloads["c1"])

    async with await astral.connect(endpoint, token=user) as u:
        err = await fsfixture.new_watch(u, w_path, REPO)
        assert err is None, f"fs.new_watch {w_path}: {err}"
        # why not asserted: a seed the scan never indexed is node1's fault,
        # and the oracle reports it as a missing Index c1
        seed_missing = await fsfixture.wait_contains(u, REPO, {c1},
                                                     INDEX_DEADLINE)
        token = await u.apphost.register(name=f"{REPO}-i",
                                         grant_permits=[SERVE])
    app = {"identity": str(token.identity), "token": token.token}

    async with await astral.connect(endpoint, token=app["token"]) as i, \
            await astral.connect(endpoint, token=user) as u:
        nonce = await register_indexer(i, INDEXER)
        enable = await answer(u, f"indexing.enable_repo?repo={REPO}&out=json")
        assert enable["type"] == "ack", f"indexing.enable_repo: {enable}"
        async with Subscription(i, nonce) as sub:
            feed = Feed(sub)
            seen = await run_steps(u, feed, n1, w, payloads)

    acked = [c["version"] for c in feed.live if c["type"] in (INDEX, UNINDEX)]
    write_facts({
        "apps": {"I": app},
        "indexer": nonce,
        "repo": REPO,
        "watch_dir": w_path,
        "watch_rel": w,
        "paths": {n: nodefs.path(n1, f"{w}/{n}.txt")
                  for n in ("seed", "f2", "f3", "f4")},
        "payloads": {k: v.decode() for k, v in payloads.items()},
        "live": feed.live,
        "live_tail": feed.tail,
        "acked_through": max(acked, default=0),
        "restarts_before": n1.get("restarts") or 0,
    })
    print(f"driver: seed indexed before enable: {not seed_missing}; "
          f"{REPO} fed I {[(c['type'], c.get('version'), c['step'], c['at']) for c in feed.live]}; "
          f"waits {({k: bool(v) for k, v in seen.items()})}; "
          f"live tail {[(c['type'], c['step']) for c in feed.tail]}")


asyncio.run(main())
