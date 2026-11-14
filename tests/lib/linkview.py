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


def throughput_row(info) -> dict:
    """row(info) with the link's cumulative data-frame byte counter.

    why cumulative: astrald counts every data-frame payload a link carries
    (mod/nodes/src/link.go onBytes) and never resets it, whatever the
    astral-py docstring says.
    """
    r = row(info)
    r["bytes_throughput"] = int(info.bytes_throughput)
    return r


async def throughput(c) -> list:
    """Every link the node behind client c holds, with byte counters."""
    return [throughput_row(i) for i in await c.nodes.links(experimental=True)]


def session_row(info) -> dict:
    """One astral-py SessionInfo as {id, link_id, remote, outbound, query,
    bytes}."""
    return {"id": str(info.id), "link_id": str(info.link_id),
            "remote": str(info.remote_identity), "outbound": bool(info.outbound),
            "query": str(info.query), "bytes": int(info.bytes)}


async def sessions(c) -> list:
    """Every open session on the node's links, as rows, oldest first.

    A session that is migrating is not listed (astrald
    mod/nodes/src/op_sessions.go).
    """
    return [session_row(s) for s in await c.nodes.sessions(experimental=True)]


def carrying(rows: list, needle: str) -> list:
    """The session rows whose query contains needle."""
    return [r for r in rows if needle in r["query"]]


async def wait_both(c1, c2, id1: str, id2: str, link_id: str,
                    timeout: float = 10.0) -> bool:
    """Wait until node1 (c1) and node2 (c2) both list link_id to each other."""
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if (link_id in ids(await links(c1), id2)
                and link_id in ids(await links(c2), id1)):
            return True
        await asyncio.sleep(0.25)
    return False


async def migrate(c, session_id: str, link_id: str) -> dict:
    """nodes.migrate_session(start=true) on the node behind c, as
    {ok, error?, elapsed}; every outcome is recorded, none raised."""
    start = time.monotonic()
    try:
        await c.nodes.migrate_session(session_id, link_id, start=True,
                                      experimental=True, timeout=60.0)
        out = {"ok": True}
    except Exception as e:  # noqa: BLE001 — every outcome is recorded
        out = {"ok": False, "error": f"{type(e).__name__}: {e}"}
    out["elapsed"] = round(time.monotonic() - start, 3)
    return out
