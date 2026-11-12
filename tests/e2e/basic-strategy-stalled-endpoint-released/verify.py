#!/usr/bin/env python3
"""Oracle: a stalled endpoint neither delays a round with a winner nor is kept.

Observed by the driver, judged here (fact `stalled_endpoint`; every time is
the host's clock, which the bait and the driver share):

- each of the 2 rounds answered a link within 10 s; a round held by the
  stalled worker hangs until the caller's 20 s timeout;
- the bait accepted at least one connection that carried bytes: node1
  dialled it and began a Noise handshake that stalled. No bytes anywhere
  means the stall was never exercised;
- node1 closed every bait connection no later than 5 s after the round that
  opened it answered, and none was still open when the driver ended.

Independent reads, with each node's own token, once both nodes' link ids to
each other stay put for 5 s: node1 holds the baseline plus exactly the two
returned ids, node2 holds the same, and both ends agree on each
(`lib/linkview.py`).
"""
import asyncio

import astral

from lib import linkview
from lib.sessionio import load

ROUNDS = 2
PROMPT = 10.0
RELEASE = 5.0
SETTLE = 30.0


def owner(rounds: list, accepted_at: float) -> dict | None:
    """The round whose span holds a connection's accept time."""
    for r in rounds:
        if r["start"] <= accepted_at <= r["end"]:
            return r
    return None


def judge_bait(fact: dict) -> None:
    rounds, conns = fact["rounds"], fact["conns"]
    assert any(c["bytes_in"] > 0 for c in conns), (
        f"the bait at {fact['bait']} received no bytes ({len(conns)} "
        "connection(s)): node1 never began a handshake with it, so the stall "
        "was not exercised")
    for c in conns:
        r = owner(rounds, c["accepted_at"])
        assert r is not None, (
            f"node1 opened a bait connection outside every round: {c}")
        assert c["eof_at"] is not None, (
            f"node1 still held the bait connection of round {r['round']} "
            f"open when the driver ended: {c}")
        late = c["eof_at"] - r["end"]
        assert late <= RELEASE, (
            f"node1 closed the bait connection of round {r['round']} "
            f"{late:.1f} s after the round answered, more than {RELEASE:.0f} s:"
            f" {c}")
    assert fact["still_open"] == 0, (
        f"{fact['still_open']} bait connection(s) still open at the end")


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    id1, id2 = n1["identity"], n2["identity"]
    fact = doc["facts"]["stalled_endpoint"]
    rounds = fact["rounds"]
    print(f"oracle: rounds {rounds}")
    print(f"oracle: bait connections {fact['conns']}")

    assert len(rounds) == ROUNDS, f"the driver ran {len(rounds)} rounds"
    for r in rounds:
        assert "error" not in r and r["elapsed"] <= PROMPT, (
            f"round {r['round']} answered {r.get('error') or r['id']} after "
            f"{r['elapsed']} s, not a link within {PROMPT:.0f} s: the stalled "
            "endpoint held back a round that had a winner")
    judge_bait(fact)

    new = [r["id"] for r in rounds]
    base1, base2 = set(fact["baseline1"]), set(fact["baseline2"])
    assert len(set(new)) == ROUNDS and not set(new) & base1, (
        f"the rounds returned {new}, not {ROUNDS} new ids beside the baseline "
        f"{sorted(base1)}")
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c1, \
            await astral.connect(n2["endpoint"], token=n2["token"]) as c2:
        async def probe():
            return await linkview.links(c1), await linkview.links(c2)

        (r1, r2), settled = await linkview.settle(
            probe, timeout=SETTLE,
            want=lambda v: len(linkview.ids(v[0], id2)) == len(base1) + ROUNDS)
    ids1 = sorted(linkview.ids(r1, id2))
    ids2 = sorted(linkview.ids(r2, id1))
    assert settled, (
        f"node1's links to node2 did not settle at baseline + {ROUNDS} within "
        f"{SETTLE:.0f} s: node1 holds {ids1}, node2 {ids2}, baseline "
        f"{sorted(base1)}, returned {new}")
    assert set(ids1) == base1 | set(new), (
        f"node1 holds {ids1} to node2, not the baseline {sorted(base1)} plus "
        f"the returned {new}")
    assert base1 == base2 and ids2 == ids1, (
        f"node2 holds {ids2} to node1 but node1 holds {ids1}")
    problems = linkview.two_view(id1, r1, id2, r2)
    assert not problems, f"node1 and node2 disagree on their links: {problems}"
    print(f"oracle: {ROUNDS} rounds answered in "
          f"{max(r['elapsed'] for r in rounds):.3f} s at most; node1 released "
          f"all {len(fact['conns'])} bait connection(s); links agree")


asyncio.run(main())
