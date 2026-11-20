#!/usr/bin/env python3
"""Oracle: node1 adopts the unowned node2, which listens on tcp 1800 and kcp 1810, through nearby alone.

node1's nearby.list carried a status of node2, newer than any before node1's
scan: a public profile naming node2 and no user, the `adoptable` flag, a
mod.nearby.listen_ports of tcp 1800 and kcp 1810 after the profile, and the
status proof last, with no stealth hint and no sealed ports. `.<alias>`
resolved on node1 to node2. The endpoints node1 found for node2 before the
adoption are exactly tcp and kcp at node2's LAN address on those ports, none
on 1791 or 1792. node1 held no link to node2 before the adoption; user.adopt
answered a contract the User issued to node2, and right after it every link
node1 holds to node2 is one node1 dialed at those endpoints. node2 holds the
contract, and each node lists the other linked.
"""
import asyncio
import time
import tomllib
from pathlib import Path

import astral

from lib import nearby
from lib.sessionio import load

# why a window: the roster and the link back reach both nodes asynchronously
# after the adoption (adopt-node's oracle waits the same way)
CONVERGE = 60.0
POLL = 1.0
CONFIG = tomllib.loads((Path(__file__).parent / "test.toml").read_text())["config"]
TCP, KCP = CONFIG["node2"]["tcp"]["listen_port"], CONFIG["node2"]["kcp"]["listen_port"]


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


def check_ports(f: dict) -> None:
    s = f["listed"]
    types = s["types"]
    assert types.count(nearby.LISTEN_PORTS) == 1, (
        f"node2, on tcp {TCP} and kcp {KCP}, sent no single listen ports object: {types}")
    assert types.index(nearby.LISTEN_PORTS) > types.index(nearby.PROFILE), (
        f"node2's listen ports precede its public profile: {types}")
    assert nearby.SEALED_PORTS not in types, f"a visible status carries sealed ports: {types}"
    assert s["ports"] == {"tcp": TCP, "kcp": KCP}, (
        f"node2's status carries ports {s['ports']}; it listens on tcp {TCP} and kcp {KCP}")


def check_resolution(f: dict, n2: dict) -> None:
    r = f.get("resolved", {})
    assert r.get("identity") == n2["identity"], (
        f"`.{f['alias']}` resolved on node1 to {r}, not node2 {n2['identity']}")
    eps = f["endpoints"]
    assert isinstance(eps, list), f"nodes.resolve_endpoints for node2 on node1 failed: {eps}"
    ip = lan_ip(n2)
    want = {f"tcp:{ip}:{TCP}", f"kcp:{ip}:{KCP}"}
    assert set(eps) == want, (
        f"node1 found {eps} for node2 before the adoption; want nearby's {sorted(want)} "
        "alone: the ports node2's status carries, and nothing at 1791 or 1792")


def check_adoption(f: dict, user_id: str, n2: dict) -> None:
    a = f.get("adopted", {})
    assert "error" not in a, f"user.adopt of `.{f['alias']}` failed: {a['error']}"
    assert a.get("issuer") == user_id, f"the adoption contract's issuer is {a.get('issuer')}, not the User"
    assert a.get("subject") == n2["identity"], f"the adoption contract names {a.get('subject')}, not node2"


def check_dial(f: dict, n2: dict) -> None:
    """Every link node1 holds to node2 right after the adoption is one node1 dialed at a carried port."""
    assert f["links_before"] == [], f"node1 held links to node2 before the adoption: {f['links_before']}"
    links = f["links_after"]
    assert links, "node1 held no link to node2 right after the adoption"
    ip = lan_ip(n2)
    want = {f"tcp:{ip}:{TCP}", f"kcp:{ip}:{KCP}"}
    for link in links:
        assert link["outbound"] and link["remote_endpoint"] in want, (
            f"node1's link {link['id']} to node2 is not one node1 dialed at "
            f"{sorted(want)}: {link}")


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
    f = doc["facts"]["nearby_ports_adopt"]
    user = f["user"]

    check_listing(f, n2)
    check_ports(f)
    check_resolution(f, n2)
    check_adoption(f, user["identity"], n2)
    check_dial(f, n2)

    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        info = await c.user.info()
    assert str(info.user_id) == user["identity"], f"node2 holds a contract of {info.user_id}, not the User"
    assert await linked_to(n1, user["token"], n2["identity"]), "node1 never listed node2 linked"
    assert await linked_to(n2, n2["token"], n1["identity"]), "node2 never listed node1 linked"

    print(f"oracle: node2 on tcp {TCP} and kcp {KCP} listed by node1 as `.{f['alias']}`, "
          f"resolved and dialed through nearby ({', '.join(f['endpoints'])}), adopted, "
          "linked both ways")


asyncio.run(main())
