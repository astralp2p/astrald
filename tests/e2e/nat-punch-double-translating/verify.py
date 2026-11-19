#!/usr/bin/env python3
"""Oracle: two port-translating, port-restricted NATs punch and link over kcp.

X06 Step 2, acceptance criterion 3: `nat.punch` between the NATed nodes
succeeds within 1 s, each side of the hole binds its local port while naming
its translated external port, `nodes.new_link -strategies nat` yields a kcp
link on both ends, and that link alone carries a 60 s echo with no loss after
its first second.
"""
import asyncio

import astral

from lib import natpunch
from lib.sessionio import load

PUNCH_LIMIT_S = 1.0
GRACE_S = 1.0
ECHO_SECONDS = 60.0


def judge_echo(echo: list) -> None:
    assert echo, "no echo was recorded"
    span = echo[-1]["t"] - echo[0]["t"]
    assert span >= ECHO_SECONDS - 1, f"the echo ran {span:.1f} s, not {ECHO_SECONDS:.0f} s"
    late = [e for e in echo if e["t"] >= GRACE_S]
    lost = [e["t"] for e in late if not e["ok"]]
    assert not lost, f"{len(lost)} of {len(late)} round trips after {GRACE_S:.0f} s were lost, at t={lost[:10]}"
    off_kcp = [(e["t"], e["networks"]) for e in late if e["networks"] != ["kcp"]]
    assert not off_kcp, f"the pair held other links than kcp during the echo: {off_kcp[:5]}"


async def networks_to(endpoint, token, peer):
    async with await astral.connect(endpoint, token=token) as c:
        return await natpunch.networks_to(c, peer)


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    facts = doc["facts"]

    punch = facts["punch"]
    assert punch["hole"], f"nat.punch returned no hole: {punch['errors']}"
    assert punch["seconds"] < PUNCH_LIMIT_S, (
        f"nat.punch took {punch['seconds']} s, not under {PUNCH_LIMIT_S} s")
    for name, node in (("node1", n1), ("node2", n2)):
        ports = natpunch.hole_ports(punch["hole"], node["identity"])
        # why: a side whose bind port equals its external port did not cross
        # a translating NAT, and this test would prove nothing about X06.
        assert ports["bind"] != ports["external"], (
            f"{name}'s side of the hole binds {ports['bind']} and is seen at "
            f"{ports['external']}: the NAT did not translate the port")

    assert "kcp" in facts["nat_link_networks"], (
        f"node1 holds no kcp link to node2: {facts['nat_link_networks']}")
    from2 = await networks_to(n2["endpoint"], n2["token"], n1["identity"])
    assert "kcp" in from2, f"node2 holds no kcp link to node1: {from2}"

    judge_echo(facts["echo"])
    print(f"oracle: punched in {punch['seconds']} s through two translating NATs; "
          f"kcp carried a {ECHO_SECONDS:.0f} s echo")


asyncio.run(main())
