#!/usr/bin/env python3
"""Oracle: each forced basic round left exactly one new link, and losers died.

Independent reads, with each node's own token:

- node1's book for node2 (nodes.resolve_endpoints) lists P1, L2, L3, L4 and
  D, so the round really had those endpoints to dial (and node2's own
  published ones besides);
- once both nodes' link ids to each other stay put for 5 polls: node1's set
  is the baseline plus exactly the three ids the rounds returned, so no loser
  survived and no baseline link died; node2's set equals node1's; every new
  link is outbound on node1 and inbound on node2, with the identities
  swapped, on tcp.

Observed by the driver, judged here (fact `excess_prune`):

- every round answered a link and no error: the refused endpoint D and the
  losers did not fail the round;
- the three returned ids are distinct and none was in the baseline;
- node1's log tap is whole and holds at least 1 line
  `closed out-link with <node2> (tcp): excess link`, the only trace of
  ErrExcessLink (a link's close reason reaches no op), and at most one per
  losing tcp endpoint of the book a round. None means the pruning path never
  ran.
"""
import asyncio
import json

import astral

from lib import jsonops, linkview
from lib.sessionio import load

ROUNDS = 3
SETTLE = 30.0


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    id1, id2 = n1["identity"], n2["identity"]
    fact = doc["facts"]["excess_prune"]
    rounds, prt = fact["rounds"], fact["ports"]
    print(f"oracle: rounds {rounds}")
    print(f"oracle: {len(fact['log_lines'])} excess-link line(s); tap "
          f"{fact['tap']}")

    errors = [r for r in rounds if "error" in r]
    assert len(rounds) == ROUNDS and not errors, (
        f"a forced basic round did not answer a link: {errors or rounds}; "
        "a loser or the refused endpoint failed the round")
    new = [r["id"] for r in rounds]
    base1, base2 = set(fact["baseline1"]), set(fact["baseline2"])
    assert len(set(new)) == ROUNDS, f"the rounds returned repeated ids {new}"
    assert not set(new) & base1, (
        f"a round returned a link node1 already held: {sorted(set(new) & base1)}"
        " (new_link without an endpoint must force a new link)")
    assert base1 == base2, (
        f"before the rounds node1 held {sorted(base1)} and node2 "
        f"{sorted(base2)} to each other")

    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c1, \
            await astral.connect(n2["endpoint"], token=n2["token"]) as c2:
        book = await jsonops.stream(
            c1, f"nodes.resolve_endpoints?identity={id2}&out=json")
        raw = json.dumps(book)
        tcp = [e for e in book
               if e["Object"]["Endpoint"]["Type"] == "mod.tcp.endpoint"]
        want = [prt["p1"], *prt["live"], prt["dead"]]
        missing = [p for p in want if f"{prt['host']}:{p}" not in raw]
        assert not missing, (
            f"node1's book for node2 lacks ports {missing}; the setup did not "
            f"happen: {raw[:400]}")

        async def probe():
            r1, r2 = await linkview.links(c1), await linkview.links(c2)
            return (sorted(linkview.ids(r1, id2)), sorted(linkview.ids(r2, id1)),
                    sorted(map(json.dumps, linkview.to(r1, id2) +
                               linkview.to(r2, id1))))
        (ids1, ids2, rows), settled = await linkview.settle(
            probe, timeout=SETTLE,
            want=lambda v: len(v[0]) == len(base1) + ROUNDS)
    rows = [json.loads(r) for r in rows]
    assert settled, (
        f"node1's links to node2 did not settle at baseline + {ROUNDS} within "
        f"{SETTLE:.0f} s: node1 holds {ids1}, node2 holds {ids2}, baseline "
        f"{sorted(base1)}, returned {new} (a loser was left open)")
    assert set(ids1) == base1 | set(new), (
        f"node1 holds {ids1} to node2, not the baseline {sorted(base1)} plus "
        f"the returned {new}: a loser survived or a kept link died")
    assert ids2 == ids1, (
        f"node2 holds {ids2} to node1 but node1 holds {ids1}: one end kept a "
        "link the other dropped")
    r1 = [r for r in rows if r["local"] == id1]
    r2 = [r for r in rows if r["local"] == id2]
    problems = linkview.two_view(id1, r1, id2, r2)
    assert not problems, f"node1 and node2 disagree on their links: {problems}"
    for r in rows:
        if r["id"] not in new:
            continue
        assert r["outbound"] == (r["local"] == id1) and r["network"] == "tcp", (
            f"new link {r['id']} reads {r} on {r['local'][:8]}…; it must be "
            "an outbound tcp link on node1 and an inbound one on node2")

    tap = fact["tap"]
    assert tap["refused"] is None and tap["broken"] is None, (
        f"node1's log tap is not whole ({tap}); the pruning witness is void")
    lines = fact["log_lines"]
    assert lines, (
        "node1 logged no `closed out-link with <node2> (tcp): excess link` "
        f"in {ROUNDS} rounds: the basic strategy's pruning path never ran")
    # why from the book: besides the five endpoints the driver named, node1
    # holds the ones node2 publishes for itself, one per interface of its
    # host, so the losers a round can have depend on the host. Each endpoint
    # yields at most one link a round, D none, and one link wins.
    most = ROUNDS * (len(tcp) - 2)
    assert len(lines) <= most, (
        f"node1 closed {len(lines)} links as excess in {ROUNDS} rounds over "
        f"{len(tcp)} tcp endpoints, one of them dead: at most {most}")
    print(f"oracle: {ROUNDS} forced rounds each left one link {new}; "
          f"{len(lines)} losers closed as excess; both ends agree")


asyncio.run(main())
