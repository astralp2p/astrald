#!/usr/bin/env python3
"""Oracle: a live read moved by the serving node lost, duplicated and
reordered nothing, and the move happened.

Invariants that hold whatever the migrate answered:

- the read neither stalled (no 15 s read without data, longest gap under
  10 s outside the hold) nor failed, and reached EOF;
- the bytes, judged against bytes made here: the 32 MiB is regenerated from
  the fact `seed` (lib/blobgen.py); its sha256 and size equal the hash and
  size in `object_id`; the driver's length, final digest and per-MiB running
  digests equal the regeneration's, and a mismatch names the first differing
  MiB;
- before the move both nodes listed the stream's session under one id on
  link Ls: node1 outbound to node2, node2 inbound for the User, relayed by
  node1 (the session's logical peer is the User, not the link's peer);
- right after the migrate answered, both nodes listed the session on one
  link: Lt after an ack, Ls after an error. A split is red;
- the migrate answered within 45 s (the op's own 30 s budget and slack);
- within 10 s after the read, neither node lists a session of it.

The contested claim, judged last: the migrate started on node2 answered an
ack, and both nodes list the session on Lt. Its failure names the defect:
the serving node addressed the session's logical peer, the User, instead of
the node across the link.
"""
import asyncio
import hashlib
import time

import astral
from astral.types import ObjectID

from lib import blobgen
from lib.blobgen import MiB
from lib.sessionio import load

MAX_GAP = 10.0
ANSWER = 45.0
LEAK_WAIT = 10.0


def judge_stream(st: dict) -> None:
    assert not st["stalled"], (
        f"the read stalled at {st['total']} B: no data for 15 s after the "
        f"move; node1 sessions {st.get('zombie1')}, node2 sessions "
        f"{st.get('zombie2')}")
    assert st["error"] is None, f"the read failed at {st['total']} B: {st['error']}"
    assert st["eof"], f"the read ended at {st['total']} B without EOF"
    assert st["max_gap"] < MAX_GAP, (
        f"the read waited {st['max_gap']} s for data, {MAX_GAP:.0f} s or more")


def judge_bytes(facts: dict) -> None:
    size, st = facts["object_size"], facts["stream"]
    data = blobgen.blob(facts["seed"], size)
    oid = ObjectID.parse(facts["object_id"])
    assert oid.size == size and oid.hash == hashlib.sha256(data).digest(), (
        f"object {facts['object_id']} is not the {size} B the seed makes")
    at = blobgen.first_difference(blobgen.running_digests(data), st["digests"])
    assert at is None, (
        f"the stream differs from the object from MiB {at} on "
        f"(offset {at * MiB}); read {st['total']} B of {size}; held and "
        f"moved at {facts['migrate'].get('offset')} B: bytes were lost, "
        "duplicated or reordered across the move")
    assert st["total"] == size and st["digest"] == hashlib.sha256(data).hexdigest(), (
        f"the stream carried {st['total']} B, not the object's {size} B")


def one_row(rows: list, sid: str, who: str, when: str) -> dict:
    assert len(rows) == 1 and rows[0]["id"] == sid, (
        f"{who} listed {rows} for the stream {when}, not one row of session "
        f"{sid}")
    return rows[0]


def judge_views(facts: dict, ids: dict) -> None:
    mig, sid = facts["migrate"], facts["session_id"]
    ls, lt = facts["link_old"], facts["link_new"]
    assert ls != lt and lt == facts["link_new_created"], (
        f"the move went from {ls} to {lt}, not onto the link the driver "
        f"created ({facts['link_new_created']})")
    r1 = one_row(mig["rows1_before"], sid, "node1", "before the migrate")
    r2 = one_row(mig["rows2_before"], sid, "node2", "before the migrate")
    assert r1["link_id"] == ls == r2["link_id"], (
        f"before the migrate node1 listed the session on {r1['link_id']} and "
        f"node2 on {r2['link_id']}, not both on {ls}")
    assert r1["outbound"] and r1["remote"] == ids["node2"], (
        f"node1's row is not outbound to node2: {r1}")
    assert not r2["outbound"] and r2["remote"] == ids["user"], (
        f"node2's row is not an inbound session of the User: {r2}; the test "
        "no longer exercises a relayed session")
    a1 = one_row(mig["rows1_after"], sid, "node1", "right after the migrate")
    a2 = one_row(mig["rows2_after"], sid, "node2", "right after the migrate")
    want = lt if mig["ok"] else ls
    assert a1["link_id"] == a2["link_id"] == want, (
        f"right after the migrate answered {'an ack' if mig['ok'] else mig.get('error')}, "
        f"node1 lists the session on {a1['link_id']} and node2 on "
        f"{a2['link_id']}, not both on {want}")
    assert mig["elapsed"] < ANSWER, (
        f"the migrate took {mig['elapsed']} s to answer, {ANSWER:.0f} s or "
        "more")


async def leaked(c, object_id: str) -> list:
    return [str(s.id) for s in await c.nodes.sessions(experimental=True)
            if object_id in str(s.query)]


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    ids = {"node1": n1["identity"], "node2": n2["identity"],
           "user": doc["facts"]["user_id"]}
    facts = {k: doc["facts"][k] for k in (
        "seed", "object_size", "object_id", "link_new_created", "session_id",
        "link_old", "link_new", "migrate", "stream")}
    mig, st = facts["migrate"], facts["stream"]
    print(f"oracle: session {facts['session_id']} {facts['link_old']} → "
          f"{facts['link_new']} on node2: ok {mig.get('ok')} "
          f"{mig.get('error')} in {mig.get('elapsed')} s at "
          f"{mig.get('offset')} B; read {st['total']} B")

    judge_stream(st)
    judge_bytes(facts)
    judge_views(facts, ids)

    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c1, \
            await astral.connect(n2["endpoint"], token=n2["token"]) as c2:
        deadline = time.monotonic() + LEAK_WAIT
        while True:
            left1, left2 = (await leaked(c1, facts["object_id"]),
                            await leaked(c2, facts["object_id"]))
            if not (left1 or left2) or time.monotonic() >= deadline:
                break
            await asyncio.sleep(0.5)
    assert not left1 and not left2, (
        f"sessions of the finished read are still open: node1 {left1}, node2 "
        f"{left2}")

    assert mig["ok"], (
        f"migrate_session started on node2, the serving node, answered "
        f"{mig.get('error')} after {mig['elapsed']} s: the serving node cannot "
        "move a relayed session (it addresses the session's logical peer, the "
        "User, instead of the node across the link)")
    print(f"oracle: node2 moved the read {facts['link_old']} → "
          f"{facts['link_new']}; {facts['object_size']} B intact; both nodes "
          "agree")


asyncio.run(main())
