#!/usr/bin/env python3
"""Oracle: a watched directory fed I its changes live, and after the restart
I's changelog folds to the files on disk.

Every id is the hash of a payload, computed here, and the files on disk are
hashed here too. Claims:

1. node1 was restarted exactly once since the driver ran.
2. Before the restart, I received Index c1, c2, c4 and c4p, in write order,
   and no other Index: the duplicate f3.txt (c2 again) added no change.
3. After the restart, I's first change is at version acked_through + 1, and
   every change before and after the restart is at the next version.
4. Over the whole changelog, each of c1, c2, c4 and c4p is indexed exactly
   once, and the unindexed ids are exactly {c1, c4}: c2 still has a holder
   (f3.txt) and c4p is on disk. Where an Unindex arrives, live or after the
   restart, is not judged.
5. The fold of the changelog (the ids whose last change is an Index) equals
   the hashes of the regular files under the watched directory and the ids
   `objects.scan` lists for the repository.
6. `objects.repositories` still lists the repository, and a second
   subscription is silent: everything was acked.

`live_tail` (what arrived in the driver's four-second windows) is printed and
never asserted on its own.
"""
import asyncio
from collections import Counter

import astral

from lib import fsfixture
from lib.indexing import INDEX, UNINDEX, Subscription, change
from lib.sessionio import load

# why: the restart's resync of the repository writes the Unindex changes in
# the background; a local node writes them in milliseconds
DEADLINE = 30.0

# why: a change already in the index arrives at once; silence this long after
# the last one means the resync wrote nothing more
IDLE = 4.0


async def resume(i, nonce: str, want_unindex: set) -> list[dict]:
    """Subscribe as I and ack every change until every id in `want_unindex`
    was unindexed and IDLE s pass in silence, or DEADLINE passes."""
    loop = asyncio.get_running_loop()
    end = loop.time() + DEADLINE
    seen, unindexed = [], set()
    async with Subscription(i, nonce) as sub:
        while (left := end - loop.time()) > 0:
            wait = min(IDLE, left) if want_unindex <= unindexed else left
            try:
                doc = await sub.next(wait)
            except TimeoutError:
                break
            if doc is None:
                break
            c = change(doc)
            seen.append(c)
            if c["type"] not in (INDEX, UNINDEX):
                break
            await sub.ack(c)
            if c["type"] == UNINDEX:
                unindexed.add(c["object"])
    return seen


async def silent(i, nonce: str) -> list[dict]:
    """What a second subscription receives within IDLE s."""
    async with Subscription(i, nonce) as sub:
        try:
            doc = await sub.next(IDLE)
        except TimeoutError:
            return []
    return [change(doc)] if doc else []


def fold(changes: list[dict]) -> set:
    held = set()
    for c in changes:
        if c["type"] == INDEX:
            held.add(c["object"])
        elif c["type"] == UNINDEX:
            held.discard(c["object"])
    return held


def short(changes: list[dict]) -> list:
    return [(c["type"].split(".")[-1], c.get("version"),
             str(c.get("object"))[:12]) for c in changes]


def check_live(live: list[dict], ids: dict) -> None:
    """Claim 2."""
    indexed = [c["object"] for c in live if c["type"] == INDEX]
    want = [ids["c1"], ids["c2"], ids["c4"], ids["c4p"]]
    assert indexed == want, (
        f"before the restart I received Index {[i[:12] for i in indexed]}, "
        f"not c1, c2, c4, c4p ({[i[:12] for i in want]}) in write order: "
        f"{short(live)}")


def check_versions(live: list[dict], after: list[dict], acked: int) -> None:
    """Claim 3."""
    assert after, "I's subscription after the restart delivered nothing"
    assert after[0]["version"] == acked + 1, (
        f"I's first change after the restart is {short(after[:1])}, not at "
        f"version {acked + 1} after the last ack ({acked})")
    versions = [c["version"] for c in live + after]
    assert versions == list(range(versions[0], versions[0] + len(versions))), (
        f"the changelog's versions are {versions}, not consecutive")


def check_changelog(changes: list[dict], ids: dict) -> None:
    """Claim 4."""
    indexed = Counter(c["object"] for c in changes if c["type"] == INDEX)
    want = Counter([ids["c1"], ids["c2"], ids["c4"], ids["c4p"]])
    assert indexed == want, (
        f"the changelog indexes {short(changes)}, not c1, c2, c4 and c4p "
        f"once each")
    unindexed = {c["object"] for c in changes if c["type"] == UNINDEX}
    assert unindexed == {ids["c1"], ids["c4"]}, (
        f"the changelog unindexes {sorted(i[:12] for i in unindexed)}, not "
        f"exactly c1 and c4 ({ids['c1'][:12]}, {ids['c4'][:12]}); c2 "
        f"({ids['c2'][:12]}) is still held by f3.txt and c4p "
        f"({ids['c4p'][:12]}) is on disk: {short(changes)}")


async def main():
    doc = load()
    f = doc["facts"]
    n1 = doc["nodes"]["node1"]
    ids = {k: fsfixture.object_id(v.encode()) for k, v in f["payloads"].items()}
    live, acked = f["live"], f["acked_through"]
    print(f"oracle: live {short(live)}; live tail {short(f['live_tail'])}")

    restarts = (n1.get("restarts") or 0) - f["restarts_before"]
    assert restarts == 1, (
        f"node1 was restarted {restarts} times between driver and oracle, "
        "not once")
    check_live(live, ids)

    async with await astral.connect(n1["endpoint"],
                                    token=f["apps"]["I"]["token"]) as i:
        after = await resume(i, f["indexer"], {ids["c1"], ids["c4"]})
        print(f"oracle: after the restart {short(after)}")
        check_versions(live, after, acked)
        check_changelog(live + after, ids)
        again = await silent(i, f["indexer"])
        assert not again, (
            f"a second subscription received {short(again)}: a change was "
            f"left unacked")

    on_disk = set(fsfixture.disk_ids(f["watch_dir"]).values())
    assert on_disk == {ids["c2"], ids["c4p"]}, (
        f"the watched directory holds {sorted(i[:12] for i in on_disk)}, not "
        f"c2 and c4p: the driver's changes did not land as recorded")
    held = fold(live + after)
    assert held == on_disk, (
        f"I's changelog folds to {sorted(i[:12] for i in held)}, not the "
        f"files on disk {sorted(i[:12] for i in on_disk)}")

    async with await astral.connect(n1["endpoint"],
                                    token=f["user_token"]) as u:
        listed = {str(r.name) for r in await u.objects.repositories()}
        assert f["repo"] in listed, (
            f"{f['repo']} is not listed after the restart")
        scanned = {str(i) for i in await u.objects.scan(f["repo"])}
    assert scanned == on_disk, (
        f"{f['repo']} lists {sorted(i[:12] for i in scanned)}, not the files "
        f"on disk {sorted(i[:12] for i in on_disk)}")

    print(f"oracle: I was indexed c1, c2, c4, c4p live and once each; the "
          f"changelog unindexes c1 and c4 alone and folds to the "
          f"{len(on_disk)} ids on disk, which {f['repo']} lists")


asyncio.run(main())
