#!/usr/bin/env python3
"""Oracle: every cut sibling pair healed on its own, and the swarm agrees.

Independent reads, on fresh sessions with each node's own token:

- after the links settle, every link a node lists to a sibling is held by
  that sibling under the same id, with `outbound` flipped and the identities
  swapped; every pair holds 1 to 2 links, the same id set on both ends;
- those id sets equal the driver's final snapshot: no link came or went
  after the driver ended;
- every node's user.swarm_status lists exactly the three nodes and marks the
  other two linked.

Observed by the driver, judged here (fact `link_heal`):

- the baseline swarm was whole and quiet, so the per-cycle comparisons mean
  something;
- each of the 5 cycles closed at least one link and healed within 30 s on a
  link id both ends agree on and that was not closed;
- in each cycle, the pairs not cut kept their link ids, from both ends;
- in each cycle, node A or node B logged a maintain-task line about the
  other (`is broken, trying to reconnect`, `established`, `restored after`):
  only MaintainLinkTask.Run logs these, so the task woke;
- the log taps were whole and dropped nothing, else the log check is void.

Never an exact link id or an exact link count: a cross-dial can leave two
links to one sibling.
"""
import asyncio
from contextlib import AsyncExitStack

import astral

from lib import linkview
from lib.sessionio import load

NAMES = ("node1", "node2", "node3")
PAIRS = (("node1", "node2"), ("node1", "node3"), ("node2", "node3"))
HEAL = 30.0
SETTLE = 15.0


def pair_key(a: str, b: str) -> str:
    return f"{a}-{b}"


def judge_cycles(fact: dict) -> None:
    base = fact["baseline"]
    assert base["whole"] and base["quiet"], (
        f"the swarm was not whole and quiet before the first cut "
        f"(whole={base['whole']}, quiet={base['quiet']}); the cycles below "
        "compare against it")
    cycles = fact["cycles"]
    prev = base["pairs"]
    for cy in cycles:
        tag = f"cycle {cy['cycle']} ({cy['pair']} closed on {cy['closer']})"
        assert cy["closed"], f"{tag}: the closer listed no link to cut"
        assert cy["healed_after"] is not None and cy["healed_after"] <= HEAL, (
            f"{tag}: the pair did not relink on a new id agreed by both ends "
            f"within {HEAL:.0f} s (MaintainLinkTask did not heal it); closed "
            f"{cy['closed']}, last links {cy['links']}, swarm linked "
            f"{cy.get('swarm_linked')}")
        assert not set(cy["agreed"]) & set(cy["closed"]), (
            f"{tag}: the agreed id {cy['agreed']} is one the driver closed")
        for key, ends in cy["pairs"].items():
            if key == cy["pair"]:
                continue
            assert ends == prev[key], (
                f"{tag}: the pair {key}, which was not cut, changed its "
                f"links from {prev[key]} to {ends}")
        lines = sum(len(v) for v in cy["log"].values())
        assert lines >= 1, (
            f"{tag}: neither end logged a maintain-task line about the other "
            "(is broken, trying to reconnect / established / restored "
            "after): the link came back, but not through MaintainLinkTask")
        prev = cy["pairs"]
    assert len(cycles) == 5, f"the driver ran {len(cycles)} cycles, not 5"


def judge_taps(fact: dict) -> None:
    for n, st in fact["taps"].items():
        assert st["refused"] is None and st["broken"] is None, (
            f"{n}'s log tap is not whole ({st}); the log check is void")
    drops = {n: d for n, d in fact["drops"].items() if d}
    assert not drops, (
        f"log.listen dropped entries ({drops}); the log check is void")


async def fresh(nodes: dict, ids: dict):
    """Each node's links, read until all three views stay put for 5 polls."""
    async with AsyncExitStack() as stack:
        cs = {n: await stack.enter_async_context(
            await astral.connect(nodes[n]["endpoint"], token=nodes[n]["token"]))
            for n in NAMES}

        async def probe():
            return {n: sorted(await linkview.links(cs[n]),
                              key=lambda r: r["id"]) for n in NAMES}
        rows, settled = await linkview.settle(probe, timeout=SETTLE)
        status = {}
        for n in NAMES:
            members = await cs[n].user.swarm_status()
            status[n] = (sorted(str(m.identity) for m in members),
                         sorted(str(m.identity) for m in members if m.linked))
    return rows, settled, status


async def main():
    doc = load()
    nodes = {n: doc["nodes"][n] for n in NAMES}
    ids = {n: nodes[n]["identity"] for n in NAMES}
    fact = doc["facts"]["link_heal"]
    assert fact["ids"] == ids, f"the fact names other nodes: {fact['ids']}"
    for cy in fact["cycles"]:
        print(f"oracle: cycle {cy['cycle']} {cy['pair']} on {cy['closer']}: "
              f"healed after {cy['healed_after']} s; links "
              f"{ {n: len(v) for n, v in cy['links'].items()} }; wake lines "
              f"{ {n: len(v) for n, v in cy['log'].items()} }")

    judge_taps(fact)
    judge_cycles(fact)

    rows, settled, status = await fresh(nodes, ids)
    assert settled, (
        f"the links did not stay put for 5 s within {SETTLE:.0f} s; last "
        f"read {rows}")
    for a, b in PAIRS:
        problems = linkview.two_view(ids[a], rows[a], ids[b], rows[b])
        assert not problems, f"{a} and {b} disagree on their links: {problems}"
        ia = linkview.ids(rows[a], ids[b])
        ib = linkview.ids(rows[b], ids[a])
        assert ia == ib and 1 <= len(ia) <= 2, (
            f"{a} holds {sorted(ia)} to {b} and {b} holds {sorted(ib)} to "
            f"{a}; each pair must hold 1 to 2 links, the same on both ends")
        want = fact["final"][pair_key(a, b)]
        got = {a: sorted(ia), b: sorted(ib)}
        assert got == want, (
            f"{a}-{b} holds {got} now but {want} when the driver ended: the "
            "links flapped after the last cycle")
    roster = sorted(ids.values())
    for n in NAMES:
        members, linked = status[n]
        others = sorted(set(roster) - {ids[n]})
        assert members == roster and linked == others, (
            f"{n}'s swarm_status lists {members} with {linked} linked, not "
            "the three nodes with the other two linked")

    print("oracle: 5 cuts healed by the maintain task within "
          f"{max(c['healed_after'] for c in fact['cycles']):.2f} s at most; "
          "untouched pairs kept their ids; both ends agree on every link")


asyncio.run(main())
