"""What the endpoint-change tests share: a sibling's address moves, seen from the host, and the links they leave dead.

The mover's moves come from the `move-subnet` op on the test's manifest. A
driver polls the mover's own addresses (`ip.local_addrs`, read live from its
interfaces), closes on every node the links that rode an address the mover
lost, and polls another sibling's links to the mover and the endpoints two
siblings hold for it.

why the host's clock: the move happens on one guest and the link on another,
and an interval across two guests needs one clock. The tunnels reach each
node's apphost socket over the WAN NIC (lib/worlds/vm.py _tunnel), which a
move on the LAN NIC leaves standing.

why the driver closes the dead links: withdrawing an address disturbs no
established connection, so every link that rode it stays pooled until each
end's keepalive closes it (astral-docs topics/link-mux.md, Keepalive), and a
link task's wake does nothing while its target counts as linked
(mod/user/src/maintain_link_task.go Wake). The endpoint-change announcement
leaves dead links to the keepalive, as its ticket states, so the driver closes
them as soon as it sees the move, as lan-outage does for an address that left.

why facts of plain values: the driver acts and the oracle judges, so every
call returns a JSON-able value and asserts nothing.
"""
import asyncio
import shlex
import tomllib
from contextlib import AsyncExitStack
from pathlib import Path

import astral

from lib.nodelog import LogTap

OP = "move-subnet"
POLL = 0.25
# why: one query through a tunnel; a slower answer is a missed poll, not a verdict
CALL = 5.0


def move_args(test_dir: Path) -> dict:
    """The --to subnets, --in and --gap of the move-subnet op on test_dir's manifest."""
    ops = tomllib.loads((test_dir / "test.toml").read_text())["ops"]
    words = next(shlex.split(o) for o in ops if o.split()[0] == OP)
    out = {"to": [], "in": 30, "gap": 3.5}
    it = iter(words[1:])
    for w in it:
        if w == "--to":
            out["to"].append(next(it))
        elif w == "--in":
            out["in"] = int(next(it))
        elif w == "--gap":
            out["gap"] = float(next(it))
    return out


def targets(node: dict, move: dict) -> list:
    """The addresses node takes, one per --to, in order.

    note: move-subnet keeps the octet of the node's 10.77 address, which is
    the address of its lan_endpoint in a VM world.
    """
    octet = node["lan_endpoint"].split(":")[1].rsplit(".", 1)[1]
    return [f"{p}.{octet}" for p in move["to"]]


async def connect(node: dict):
    return await astral.connect(node["endpoint"], token=node["token"])


async def ask(coro, default=None):
    """coro's answer within CALL s, or default when it fails or is late."""
    try:
        return await asyncio.wait_for(coro, CALL)
    except Exception:  # noqa: BLE001 — a missed poll is retried, never judged
        return default


def ip_of(endpoint) -> str | None:
    """The IP of a tcp or kcp endpoint, else None."""
    ip = getattr(endpoint, "ip", None)
    return ip.text() if ip is not None else None


def link_row(info) -> dict:
    """One astral-py LinkInfo as plain values, with the IPs of both ends."""
    remote = info.remote_endpoint
    return {"id": str(info.id), "remote": str(info.remote_identity),
            "outbound": bool(info.outbound), "network": str(info.network),
            "local_ip": ip_of(info.local_endpoint), "remote_ip": ip_of(remote),
            "remote_endpoint": remote.address() if remote is not None else None}


async def links(c) -> list:
    return [link_row(i) for i in await c.nodes.links(experimental=True)]


def to(rows: list, peer: str) -> list:
    return [r for r in rows if r["remote"] == peer]


async def local_ips(c) -> list:
    """The node's own interface addresses, loopback excluded."""
    return sorted(str(a) for a in await c.ip.local_addrs())


async def endpoints_of(c, identity: str) -> list:
    """The endpoints the node resolves for identity, as `<network>:<address>`."""
    return sorted(f"{e.endpoint.network()}:{e.endpoint.address()}"
                  for e in await c.nodes.resolve_endpoints(identity, experimental=True))


def holds(rows: list | None, ip: str) -> bool:
    """Whether endpoint rows hold a tcp or kcp row at ip."""
    return any(r.split(":", 1)[1].rsplit(":", 1)[0] == ip
               for r in rows or [] if r.split(":", 1)[0] in ("tcp", "kcp"))


async def close_riding(c, gone: set) -> list:
    """Closes every link of the node whose either end is at an address in gone; what it closed."""
    out = []
    for r in await ask(links(c), []):
        if r["local_ip"] in gone or r["remote_ip"] in gone:
            err = await ask(c.nodes.close_link(r["id"], experimental=True), "late")
            out.append({"id": r["id"], "remote": r["remote"], "error": err})
    return out


async def follow(mover, clients: dict, final: str, clock, deadline: float) -> dict:
    """The mover's address changes until it holds final: each with its time, what
    it added and removed, and the links closed on each node for what it removed."""
    seen = await ask(local_ips(mover))
    # why kept apart from seen: seen follows every change, and start is what the mover held first
    start = seen
    changes = []
    while clock() < deadline:
        now = await ask(local_ips(mover))
        if now is not None and seen is not None and now != seen:
            removed = sorted(set(seen) - set(now))
            closed = {name: await close_riding(c, set(removed))
                      for name, c in clients.items()} if removed else {}
            changes.append({"t": clock(), "added": sorted(set(now) - set(seen)),
                            "removed": removed, "closed": closed})
            if final in now:
                return {"start": start, "changes": changes, "ips": now}
        seen = now if now is not None else seen
        await asyncio.sleep(POLL)
    return {"start": start, "changes": changes, "ips": seen}


async def watch(sibling, witness, mover_id: str, ip: str, clock, deadline: float,
                linked_for: float) -> dict:
    """Polls sibling's links to the mover and the endpoints sibling and witness
    resolve for it, until sibling holds an outbound link to ip for linked_for s
    and both resolve a row at ip, or deadline: when each first held, and every
    change of sibling's links to the mover."""
    out = {"linked_at": None, "sibling_row_at": None, "witness_row_at": None, "timeline": []}
    while clock() < deadline:
        rows = to(await ask(links(sibling), []), mover_id)
        if not out["timeline"] or out["timeline"][-1]["links"] != rows:
            out["timeline"].append({"t": clock(), "links": rows})
        if out["linked_at"] is None and any(r["outbound"] and r["remote_ip"] == ip for r in rows):
            out["linked_at"] = clock()
        for key, c in (("sibling_row_at", sibling), ("witness_row_at", witness)):
            if out[key] is None and holds(await ask(endpoints_of(c, mover_id)), ip):
                out[key] = clock()
        if (out["linked_at"] is not None and clock() >= out["linked_at"] + linked_for
                and out["sibling_row_at"] is not None and out["witness_row_at"] is not None):
            break
        await asyncio.sleep(POLL)
    return out


# the lines the oracles read: the announcement chain (mod/nodes/src/endpoints_changed.go),
# mod/ip's change line, and the link-task lines (mod/user/src/maintain_link_task.go)
NEEDLES = ("new endpoints of", "announcing changed endpoints", "forwarding endpoints change",
           "pulling endpoints of", "network addresses changed", "established",
           "restored after", "is broken", "trying to reconnect")
ROLES = ("node1", "node2", "reflector")


async def record(nodes: dict, move: dict, margin: float, watch_for: float) -> dict:
    """The whole observation of one move-subnet run, as a fact.

    node1 (B) moves, node2 (A) is the sibling behind NAT, the reflector (C)
    the sibling B relinks to. Before the move: every node's links and the rows
    node2 resolves for node1. Then node1's changes until it holds its last
    target, with the dead links closed (follow), then node2's link to that
    address and the rows node2 and the reflector resolve for it (watch), for
    watch_for s after the change that added it. Every node's log is tapped
    throughout. Times are seconds since the call began, on the host's clock.
    """
    idb = nodes["node1"]["identity"]
    final = targets(nodes["node1"], move)[-1]
    t0 = asyncio.get_running_loop().time()
    clock = lambda: round(asyncio.get_running_loop().time() - t0, 2)  # noqa: E731
    async with AsyncExitStack() as stack:
        taps = {n: await stack.enter_async_context(LogTap(nodes[n])) for n in ROLES}
        cl = {n: await stack.enter_async_context(await connect(nodes[n])) for n in ROLES}
        before = {"t": clock(), "links": {n: await ask(links(c), []) for n, c in cl.items()},
                  "node2_rows": await ask(endpoints_of(cl["node2"], idb))}
        moved = await follow(cl["node1"], cl, final, clock, move["in"] + margin)
        t_final = next((ch["t"] for ch in moved["changes"] if final in ch["added"]), None)
        seen = None
        if t_final is not None:
            seen = await watch(cl["node2"], cl["reflector"], idb, final, clock,
                               t_final + watch_for, 5.0)
        after = {"t": clock(), "links": {n: await ask(links(c), []) for n, c in cl.items()},
                 "node2_rows": await ask(endpoints_of(cl["node2"], idb)),
                 "reflector_rows": await ask(endpoints_of(cl["reflector"], idb))}
        logs = {n: t.find(*NEEDLES) for n, t in taps.items()}
        tapped = {n: t.state() for n, t in taps.items()}
    return {"move": move, "targets": targets(nodes["node1"], move), "before": before,
            "moved": moved, "watch": seen, "after": after, "logs": logs, "taps": tapped}
