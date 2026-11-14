#!/usr/bin/env python3
"""Oracle: a live read moved to a second link lost, duplicated and reordered
nothing, and both nodes agree it moved.

The bytes, judged against bytes made here: the 32 MiB is regenerated from the
fact `seed` (lib/blobgen.py); its sha256 and size equal the hash and size in
`object_id`, so the generator matches what node2 stored. The driver's length,
final digest and per-MiB running digests equal the regeneration's; a mismatch
names the first differing MiB. The driver's digests are the one input this
oracle does not own.

Observed by the driver, judged here:

- the read neither stalled (no 15 s read without data, longest gap under
  10 s) nor failed, and reached EOF;
- the migrate started after 4 MiB, at least 8 MiB before the end, and node1
  answered an ack;
- before it, node1 and node2 each listed the stream's session under one id
  on link Ls; right after the ack, both list it on Lt;
- a second migrate of the session to Lt answered `session already on link`;
- Ls carried at most 256 KiB between the ack and its close, and Lt carried at
  least 2 MiB more than before the migrate (node1's cumulative counters);
- closing Ls on node1 answered an ack, and node1 dropped it.

Independent reads, with each node's own token: Lt is held by both nodes with
`outbound` flipped and Ls by neither (node2 is polled up to 10 s); within
10 s, neither node lists a session whose query names the object.
"""
import asyncio
import hashlib
import time

import astral
from astral.types import ObjectID

from lib import blobgen, linkview
from lib.blobgen import MiB
from lib.sessionio import load

MAX_GAP = 10.0
LS_AFTER_ACK = 256 * 1024
LT_MIN = 2 * MiB
GONE_WAIT = 10.0
LEAK_WAIT = 10.0
ALREADY = "session already on link"


def judge_bytes(facts: dict) -> None:
    size, st = facts["object_size"], facts["stream"]
    data = blobgen.blob(facts["seed"], size)
    oid = ObjectID.parse(facts["object_id"])
    assert oid.size == size and oid.hash == hashlib.sha256(data).digest(), (
        f"object {facts['object_id']} is not the {size} B the seed makes: "
        "node2 stored other bytes, or the generator changed")
    want = blobgen.running_digests(data)
    at = blobgen.first_difference(want, st["digests"])
    assert at is None, (
        f"the stream differs from the object from MiB {at} on "
        f"(offset {at * MiB}); read {st['total']} B of {size}; migrate at "
        f"{facts['migrate'].get('offset')}, ack at "
        f"{facts['migrate'].get('ack_offset')}: bytes were lost, duplicated "
        "or reordered across the move")
    assert st["total"] == size and st["digest"] == hashlib.sha256(data).hexdigest(), (
        f"the stream carried {st['total']} B with digest {st['digest'][:16]}…, "
        f"not the object's {size} B")


def judge_stream(st: dict) -> None:
    assert not st["stalled"], (
        f"the read stalled at {st['total']} B: no data for 15 s after the "
        f"move; node1 sessions {st.get('zombie1')}, node2 sessions "
        f"{st.get('zombie2')}")
    assert st["error"] is None, f"the read failed at {st['total']} B: {st['error']}"
    assert st["eof"], f"the read ended at {st['total']} B without EOF"
    assert st["max_gap"] < MAX_GAP, (
        f"the read waited {st['max_gap']} s for data, {MAX_GAP:.0f} s or more")


def one_row(rows: list, sid: str, link: str, who: str, when: str) -> None:
    assert len(rows) == 1 and rows[0]["id"] == sid, (
        f"{who} listed {rows} for the stream {when}, not one row of session "
        f"{sid}")
    assert rows[0]["link_id"] == link, (
        f"{who} listed session {sid} on link {rows[0]['link_id']} {when}, "
        f"not {link}")


def judge_migrate(facts: dict) -> None:
    size, mig = facts["object_size"], facts["migrate"]
    sid, ls, lt = facts["session_id"], facts["link_old"], facts["link_new"]
    assert ls != lt, f"the stream already rode the target link {lt}"
    assert lt == facts["link_new_created"], (
        f"the target {lt} is not the link the driver created "
        f"({facts['link_new_created']})")
    assert facts["link_new_seen"], (
        f"node2 never listed the new link {lt}")
    assert mig.get("ok"), (
        f"migrate_session({sid} → {lt}) on node1 answered "
        f"{mig.get('error')} after {mig.get('elapsed')} s")
    assert 4 * MiB <= mig["offset"] <= size - 8 * MiB, (
        f"the migrate started at {mig['offset']} B, outside 4 MiB .. "
        f"{size - 8 * MiB} B: not mid-stream")
    assert mig["ack_offset"] <= size - 8 * MiB, (
        f"the ack came at {mig['ack_offset']} B, less than 8 MiB before the "
        "end: too little of the stream ran after the move")
    one_row(mig["rows1_before"], sid, ls, "node1", "before the migrate")
    one_row(mig["rows2_before"], sid, ls, "node2", "before the migrate")
    one_row(mig["rows1_after"], sid, lt, "node1", "right after the ack")
    one_row(mig["rows2_after"], sid, lt, "node2", "right after the ack")
    again = mig["again"]
    assert not again["ok"] and ALREADY in again.get("error", ""), (
        f"a second migrate of {sid} to {lt} answered {again}, not "
        f"'{ALREADY}'")


def counter(rows: list, link: str) -> int | None:
    for r in rows:
        if r["id"] == link:
            return r["bytes_throughput"]
    return None


def judge_path(facts: dict, lt_now: int | None) -> None:
    mig, close = facts["migrate"], facts["close_old"]
    ls, lt = facts["link_old"], facts["link_new"]
    ls_ack = counter(mig["links_after"], ls)
    ls_close = counter(close["links_before"], ls)
    assert ls_ack is not None and ls_close is not None, (
        f"node1 did not list {ls} at the ack and at the close")
    assert ls_close - ls_ack <= LS_AFTER_ACK, (
        f"Ls {ls} carried {ls_close - ls_ack} B between the ack and its "
        f"close, more than {LS_AFTER_ACK} B: the stream stayed on it")
    lt_before = counter(mig["links_before"], lt)
    assert lt_before is not None and lt_now is not None, (
        f"node1 did not list {lt} before the migrate and now")
    assert lt_now - lt_before >= LT_MIN, (
        f"Lt {lt} carried {lt_now - lt_before} B since the migrate, less "
        f"than {LT_MIN} B: the stream did not move onto it")
    assert close["reached"] and close.get("result") == "ack", (
        f"closing Ls {ls} on node1 answered {close.get('result')} at "
        f"{close['offset']} B")
    assert close.get("gone"), f"node1 still listed Ls {ls} 10 s after its close"


async def links_now(c1, c2, id1, id2, ls: str):
    deadline = time.monotonic() + GONE_WAIT
    while True:
        r1, r2 = await linkview.links(c1), await linkview.links(c2)
        gone = (ls not in linkview.ids(r1, id2)
                and ls not in linkview.ids(r2, id1))
        if gone or time.monotonic() >= deadline:
            return r1, r2
        await asyncio.sleep(0.25)


async def leaked(c, object_id: str) -> list:
    return [str(s.id) for s in await c.nodes.sessions(experimental=True)
            if object_id in str(s.query)]


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    id1, id2 = n1["identity"], n2["identity"]
    facts = {k: doc["facts"][k] for k in (
        "seed", "object_size", "object_id", "link_new_created",
        "link_new_seen", "session_id", "link_old", "link_new", "migrate",
        "close_old", "stream")}
    ls, lt = facts["link_old"], facts["link_new"]
    st = facts["stream"]
    print(f"oracle: session {facts['session_id']} {ls} → {lt}; migrate "
          f"{facts['migrate'].get('ok')} in {facts['migrate'].get('elapsed')} "
          f"s at {facts['migrate'].get('offset')} B; read {st['total']} B, "
          f"max gap {st['max_gap']} s")

    judge_stream(st)
    judge_bytes(facts)
    judge_migrate(facts)

    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c1, \
            await astral.connect(n2["endpoint"], token=n2["token"]) as c2:
        lt_now = None
        for i in await c1.nodes.links(experimental=True):
            if str(i.id) == lt:
                lt_now = int(i.bytes_throughput)
        r1, r2 = await links_now(c1, c2, id1, id2, ls)
        deadline = time.monotonic() + LEAK_WAIT
        while True:
            left1, left2 = (await leaked(c1, facts["object_id"]),
                            await leaked(c2, facts["object_id"]))
            if not (left1 or left2) or time.monotonic() >= deadline:
                break
            await asyncio.sleep(0.5)

    judge_path(facts, lt_now)
    ids1, ids2 = linkview.ids(r1, id2), linkview.ids(r2, id1)
    assert lt in ids1 and lt in ids2, (
        f"Lt {lt} is not held by both nodes: node1 {sorted(ids1)}, node2 "
        f"{sorted(ids2)}")
    assert ls not in ids1 and ls not in ids2, (
        f"Ls {ls} is still held after its close: node1 {sorted(ids1)}, node2 "
        f"{sorted(ids2)}")
    problems = linkview.two_view(id1, r1, id2, r2)
    assert not problems, f"node1 and node2 disagree on their links: {problems}"
    assert not left1 and not left2, (
        f"sessions of the finished read are still open: node1 {left1}, node2 "
        f"{left2}")
    print(f"oracle: {facts['object_size']} B intact across the move "
          f"{ls} → {lt}; both nodes agree on the session and the links")


asyncio.run(main())
