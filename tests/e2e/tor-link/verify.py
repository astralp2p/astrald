#!/usr/bin/env python3
"""Oracle: queries reach node2 again within the keepalive bound, and the swarm holds over Tor.

Ported from the netsim link-over-tor verify.py. Asserted on both ends: a link
node1 believes in that node2 does not have is not a link.

From the `first_answer` fact: node2 first answered a query from node1 within
tcp's link_idle_ping plus link_ping_timeout plus one query of the driver's
start, over tor. No step closed the dead tcp link; node1's keepalive did.

note: the bound runs from the driver's start, which follows leave-lan and its
verifier, so the time from the address withdrawal is longer by those seconds.
"""
import asyncio

import astral

from lib.sessionio import load

# why constants: tor-link sets no tcp config, so node1 runs tcp's defaults
# (mod/tcp/src/config.go, LinkIdlePing and LinkPingTimeout).
TCP_IDLE_PING = 15.0
TCP_PING_TIMEOUT = 10.0
QUERY = 60.0     # one query: the driver's per-query timeout, the routing cap
SLACK = 2.0


async def networks_to(endpoint, token, peer):
    async with await astral.connect(endpoint, token=token) as c:
        links = await c.nodes.links(experimental=True)
    return sorted({l.network for l in links
                   if str(l.remote_identity) == peer})


def check_first_answer(answer: dict) -> None:
    limit = TCP_IDLE_PING + TCP_PING_TIMEOUT + QUERY + SLACK
    print(f"oracle: node2 first answered {answer['after']} s after the driver "
          f"started (limit {limit} s), over {answer['networks']}; tcp "
          f"{answer['tcp']}; {len(answer['failures'])} failed queries before: "
          f"{answer['failures']}")
    assert answer["after"] is not None, (
        f"node2 answered no query from node1 after leaving the LAN: "
        f"{answer['failures']}")
    assert answer["after"] <= limit, (
        f"node2 first answered {answer['after']} s after the driver started; "
        f"want within tcp's idle interval + ping timeout + one query, {limit} s")
    assert "tor" in answer["networks"], (
        f"node1 held no tor link to node2 when node2 first answered: "
        f"{answer['networks']}")


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]

    check_first_answer(doc["facts"]["first_answer"])

    from1 = await networks_to(n1["endpoint"], doc["facts"]["user_token"],
                              n2["identity"])
    from2 = await networks_to(n2["endpoint"], n2["token"], n1["identity"])

    assert "tor" in from1, f"node1 holds no tor link to node2: {from1}"
    assert "tor" in from2, f"node2 holds no tor link to node1: {from2}"
    assert "tcp" not in from1, (
        f"node1 still holds a LAN link to node2 ({from1}) — its keepalive did "
        "not close the dead tcp link, so the tor link proves nothing")
    print(f"oracle: tor link on both ends (node1 {from1}, node2 {from2})")


asyncio.run(main())
