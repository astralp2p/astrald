#!/usr/bin/env python3
"""Oracle: node1 lists the unowned node2 from nearby, resolves its alias and address there, and adopts it.

node1's nearby.list carried a status of node2, newer than any before node1's
scan: a public profile naming node2 and no user, the `adoptable` flag, and the
status proof last, with no stealth hint. `.<alias>` resolved on node1 to node2,
and every endpoint node1 found for node2 before the adoption sits at node2's
LAN address, with tcp :1791 and kcp :1792 among them. user.adopt of
`.<alias>` answered a contract the User issued to node2, node2 holds it, and
each node lists the other linked.
"""
import asyncio
import time

import astral

from lib import nearby
from lib.sessionio import load

# why a window: the roster and the link back reach both nodes asynchronously
# after the adoption (adopt-node's oracle waits the same way)
CONVERGE = 60.0
POLL = 1.0


def lan_ip(node: dict) -> str:
    """The address a peer dials node at: the host of its lan_endpoint."""
    return node["lan_endpoint"].split(":")[1]


def check_listing(f: dict, n2: dict) -> None:
    s = f["listed"]
    assert s is not None, (
        f"node1's nearby.list carried no status of node2 within {f['waited']}s "
        f"of node1's scan (scan answered {f['scan']}; before it node1 listed "
        f"{f['before']})")
    assert not f["scan"]["refused"] and f["scan"]["types"] == ["ack"], (
        f"nearby.scan on node1 answered {f['scan']}")
    p = s["profile"]
    assert p is not None, f"node2's status carries no public profile: {s['types']}"
    assert p["node"] == n2["identity"], f"node2's profile names node {p['node']}"
    assert p["user"] is None, f"unowned node2's profile names user {p['user']}"
    assert p["alias"], "node2's public profile names no alias"
    assert "adoptable" in s["flags"], f"unowned node2's status flags {s['flags']}, not adoptable"
    assert nearby.HINT not in s["types"], f"a visible status carries a stealth hint: {s['types']}"
    assert s["types"] and s["types"][-1] == nearby.PROOF, (
        f"node2's status does not end with its proof: {s['types']}")


def check_resolution(f: dict, n2: dict) -> None:
    r = f.get("resolved", {})
    assert r.get("identity") == n2["identity"], (
        f"`.{f['alias']}` resolved on node1 to {r}, not node2 {n2['identity']}")
    eps = f["endpoints"]
    assert isinstance(eps, list), f"nodes.resolve_endpoints for node2 on node1 failed: {eps}"
    ip = lan_ip(n2)
    want = {f"tcp:{ip}:1791", f"kcp:{ip}:1792"}
    assert want <= set(eps), f"node1 found {eps} for node2; want nearby's {sorted(want)} among them"
    strays = [e for e in eps if not e.split(":", 1)[1].startswith(f"{ip}:")]
    assert not strays, (
        f"node1 found endpoints for node2 that nearby does not resolve: {strays} "
        "(the test gives none, so something else did)")


def check_adoption(f: dict, user_id: str, n2: dict) -> None:
    a = f.get("adopted", {})
    assert "error" not in a, f"user.adopt of `.{f['alias']}` failed: {a['error']}"
    assert a.get("issuer") == user_id, f"the adoption contract's issuer is {a.get('issuer')}, not the User"
    assert a.get("subject") == n2["identity"], f"the adoption contract names {a.get('subject')}, not node2"


async def linked_to(node: dict, token: str, peer: str) -> bool:
    deadline = time.monotonic() + CONVERGE
    while True:
        async with await astral.connect(node["endpoint"], token=token) as c:
            members = await c.user.swarm_status()
        if any(str(m.identity) == peer and m.linked for m in members):
            return True
        if time.monotonic() >= deadline:
            return False
        await asyncio.sleep(POLL)


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    user = doc["facts"]["nearby_user"]
    f = doc["facts"]["nearby_adopt"]

    check_listing(f, n2)
    check_resolution(f, n2)
    check_adoption(f, user["identity"], n2)

    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        info = await c.user.info()
    assert str(info.user_id) == user["identity"], f"node2 holds a contract of {info.user_id}, not the User"
    assert await linked_to(n1, user["token"], n2["identity"]), "node1 never listed node2 linked"
    assert await linked_to(n2, n2["token"], n1["identity"]), "node2 never listed node1 linked"

    print(f"oracle: node2 listed by node1 as `.{f['alias']}`, resolved and dialed "
          f"through nearby ({', '.join(f['endpoints'])}), adopted, linked both ways")


asyncio.run(main())
