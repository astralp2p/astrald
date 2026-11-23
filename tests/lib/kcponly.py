"""Join a set of local nodes by kcp links alone.

Lifted from tests/e2e/kcp-only-restart-relink/script.py, which keeps its own
copy. `apply` turns tcp dial and listen off through each node's tree, gives
each node the other nodes' kcp rows, closes every link that is not kcp, links
each pair over kcp and waits until every pair holds kcp links only:

    from lib import kcponly

    async with ...:
        joined = await kcponly.apply(doc["nodes"], ["node1", "node2", "node3"])
    assert joined["settled"], joined["links"]

why the tree: a tree setting survives a node restart, while a local world
rewrites every module config at each start (lib/localnode.py).
"""
import time
from contextlib import AsyncExitStack

import astral
from astral.primitives import Bool

from lib import linkview

TCP_SETTINGS = ("/mod/tcp/settings/dial", "/mod/tcp/settings/listen")
# why: a kcp link on loopback settles within seconds; kcp-only-restart-relink
# waits the same
SETTLE = 60.0
DIAL = 30.0


def lan_ip(node: dict) -> str:
    """The address of node's `lan_endpoint`, `tcp:<ip>:<port>`."""
    return node["lan_endpoint"].split(":")[1]


async def kcp_rows(c, node: dict) -> list:
    """The kcp rows a peer dials to reach the node behind c, the row at its
    `lan_endpoint` address first.

    why the lan_endpoint address: the node advertises no loopback address
    (mod/ip/src/module.go LocalIPs), and a local world's peers meet there.
    """
    own = [e.endpoint for e in await c.nodes.resolve_endpoints("localnode", experimental=True)]
    kcp = [e for e in own if callable(getattr(e, "network", None)) and e.network() == "kcp"]
    first = [f"kcp:{lan_ip(node)}:{p}" for p in sorted({e.port for e in kcp})]
    rest = sorted({f"kcp:{e.address()}" for e in kcp} - set(first))
    return first + rest


async def close_non_kcp(c, peer: str) -> list:
    """Close every link to peer that is not kcp; the ids closed."""
    closed = []
    for r in linkview.to(await linkview.links(c), peer):
        if r["network"] != "kcp":
            await c.nodes.close_link(r["id"], experimental=True)
            closed.append(f"{r['network']}:{r['id']}")
    return closed


async def tcp_off(c) -> None:
    """Turn tcp dial and listen off on the node behind c."""
    for path in TCP_SETTINGS:
        await c.tree.set(path, Bool(False))


async def tcp_settings(c) -> dict:
    """{path: value read back} of the tcp settings, as plain values."""
    return {path: _plain(await c.tree.get(path)) for path in TCP_SETTINGS}


def _plain(value):
    if value == True:  # noqa: E712 — a Bool primitive compares to the bool
        return True
    if value == False:  # noqa: E712
        return False
    return str(value)


async def dial(c, peer: str, endpoint: str, timeout: float = DIAL) -> dict:
    """One `nodes.new_link` to endpoint: its link or its error, timed."""
    start = time.monotonic()
    try:
        info = await c.nodes.new_link(peer, endpoint=endpoint,
                                      experimental=True, timeout=timeout)
        r = {"id": str(info.id), "network": str(info.network)}
    except Exception as e:  # noqa: BLE001 — every outcome is recorded
        r = {"error": f"{type(e).__name__}: {e}"}
    r["elapsed"] = round(time.monotonic() - start, 3)
    return r


def pairs(names) -> list:
    """Every unordered pair of names, the lower name first."""
    names = sorted(names)
    return [(a, b) for i, a in enumerate(names) for b in names[i + 1:]]


def kcp_only(view: dict, names, ids: dict) -> bool:
    """Whether every node holds a link to every other, and every one is kcp."""
    for a in names:
        for b in names:
            if a == b:
                continue
            rows = linkview.to(view[a], ids[b])
            if not rows or any(r["network"] != "kcp" for r in rows):
                return False
    return True


async def apply(nodes: dict, names) -> dict:
    """Join the named nodes by kcp alone; what was done and seen.

    Returns {rows, closed, dialed, settled, links, tcp}: each node's kcp rows,
    the links closed, the kcp link each pair dialed, whether every pair held
    kcp links only within SETTLE seconds, each node's links then, and each
    node's tcp settings read back.
    """
    names = sorted(names)
    ids = {n: nodes[n]["identity"] for n in names}
    async with AsyncExitStack() as stack:
        clients = {n: await stack.enter_async_context(
            await astral.connect(nodes[n]["endpoint"], token=nodes[n]["token"]))
            for n in names}
        return await _apply(clients, nodes, names, ids)


async def _apply(clients: dict, nodes: dict, names: list, ids: dict) -> dict:
    out = {"rows": {}, "closed": {}, "dialed": {}}
    for n in names:
        out["rows"][n] = await kcp_rows(clients[n], nodes[n])
    for a in names:
        for b in names:
            if a != b:
                for r in out["rows"][b]:
                    await clients[a].nodes.add_endpoint(ids[b], r, experimental=True)
    for n in names:
        await tcp_off(clients[n])
    for n in names:
        out["closed"][n] = [x for p in names if p != n
                            for x in await close_non_kcp(clients[n], ids[p])]
    for a, b in pairs(names):
        rows = out["rows"][b]
        out["dialed"][f"{a}->{b}"] = (await dial(clients[a], ids[b], rows[0]) if rows
                                      else {"error": f"{b} resolved no kcp row"})

    async def view():
        return {n: sorted(await linkview.links(clients[n]), key=lambda r: r["id"])
                for n in names}

    out["links"], out["settled"] = await linkview.settle(
        view, timeout=SETTLE, want=lambda v: kcp_only(v, names, ids))
    out["tcp"] = {n: await tcp_settings(clients[n]) for n in names}
    return out
