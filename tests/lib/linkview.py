"""A node's links as plain rows, and the checks that compare two nodes' views.

why rows of plain values: a driver records links as facts and an oracle
compares them, so a link is a JSON-able dict, never an astral-py object.

why both ends: a link has one id on both of its nodes. The inbound side picks
the nonce and the outbound side reads it during negotiation (astrald
mod/nodes/src/link_negotiator.go NegotiateOutbound, NegotiateInbound), so
the same id, with `outbound` flipped and the identities swapped, is what two
nodes holding one link must report.
"""
import asyncio
import time


def row(info) -> dict:
    """One astral-py LinkInfo as {id, local, remote, outbound, network}."""
    return {"id": str(info.id),
            "local": str(info.local_identity),
            "remote": str(info.remote_identity),
            "outbound": bool(info.outbound),
            "network": str(info.network)}


async def links(c) -> list:
    """Every link the node behind client c holds, as rows, oldest first."""
    return [row(i) for i in await c.nodes.links(experimental=True)]


def to(rows: list, peer: str) -> list:
    """The rows whose remote is peer."""
    return [r for r in rows if r["remote"] == peer]


def ids(rows: list, peer: str) -> set:
    """The ids of the links to peer."""
    return {r["id"] for r in to(rows, peer)}


def two_view(a: str, rows_a: list, b: str, rows_b: list) -> list:
    """What breaks the agreement of node a's and node b's links to each other.

    Each link between a and b must be held by both, under one id, with
    `outbound` flipped and local and remote swapped. An empty list means
    they agree.
    """
    problems = []
    for me, mine, peer, theirs in ((a, rows_a, b, rows_b),
                                   (b, rows_b, a, rows_a)):
        other = {r["id"]: r for r in to(theirs, me)}
        for r in to(mine, peer):
            o = other.get(r["id"])
            if o is None:
                problems.append(f"{me[:8]}… holds link {r['id']} to "
                                f"{peer[:8]}…, which {peer[:8]}… does not")
                continue
            if r["local"] != me or o["local"] != peer:
                problems.append(f"link {r['id']}: local identities "
                                f"{r['local'][:8]}…/{o['local'][:8]}… are not "
                                "the two ends")
            if r["outbound"] == o["outbound"]:
                problems.append(f"link {r['id']}: outbound={r['outbound']} on "
                                "both ends; exactly one end dialled it")
    return sorted(set(problems))


async def settle(probe, *, polls: int = 5, every: float = 1.0,
                 timeout: float = 30.0, want=None):
    """Poll probe() until its answer stays the same for `polls` polls in a
    row, and want(answer) holds when given. Returns (answer, settled).

    why a stability window: a link a node is about to close is briefly in its
    pool, so one read can catch a transient the node itself is undoing.
    """
    deadline = time.monotonic() + timeout
    last, same = await probe(), 1
    while time.monotonic() < deadline:
        if same >= polls and (want is None or want(last)):
            return last, True
        await asyncio.sleep(every)
        got = await probe()
        same = same + 1 if got == last else 1
        last = got
    return last, same >= polls and (want is None or want(last))
