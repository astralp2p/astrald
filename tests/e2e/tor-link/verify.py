#!/usr/bin/env python3
"""Oracle: queries reach node2 again within the keepalive bound, and the swarm holds over Tor.

Ported from the netsim link-over-tor verify.py. Asserted on both ends: a link
node1 believes in that node2 does not have is not a link.

From the `first_answer` fact: node2 first answered a query from node1 within
tcp's link_idle_ping plus link_ping_timeout plus one query of the driver's
start, over tor. No step closed the dead tcp link; node1's keepalive did.

From the driver's link watch (`first_answer.tcp`), ticket akchpFNjbO
criterion 6, netsim half:

- node1 lists none of its old links to node2, the ones that ran over the
  LAN, within DROP_AFTER_NEW s of the new link. A new link probes the older
  ones (mod/nodes/src/link_pool.go probeOlderLinks), and an idle dead link
  closes within its keepalive bound anyway;
- node1's endpoint table holds every tcp, kcp and tor endpoint node2
  advertises for itself, written after the new link, within ENDPOINTS_WITHIN
  s of it: the endpoint pull runs on the first link to a sibling and on a
  partial close (mod/nodes/src/object_receiver.go). script.py says how a
  row's TTL dates it.

note: the operator under `--driver agent` records `tcp` as an empty object
and watches no link table (prompt.md), so the link-watch criteria are judged
on scripted runs alone; the oracle says so when it skips them.

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
# why: ticket akchpFNjbO criterion 6
DROP_AFTER_NEW = 35.0
ENDPOINTS_WITHIN = 60.0


async def networks_to(endpoint, token, peer):
    async with await astral.connect(endpoint, token=token) as c:
        links = await c.nodes.links(experimental=True)
    return sorted({l.network for l in links
                   if str(l.remote_identity) == peer})


def failed_count(failures) -> int:
    """How many queries failed before the first answer.

    why: script.py records a list of the failures, while an operator under
    `--driver agent` may record their count; the oracle asserts neither, so
    it reports both forms rather than break on one.
    """
    return failures if isinstance(failures, int) else len(failures or [])


def check_first_answer(answer: dict) -> None:
    limit = TCP_IDLE_PING + TCP_PING_TIMEOUT + QUERY + SLACK
    print(f"oracle: node2 first answered {answer['after']} s after the driver "
          f"started (limit {limit} s), over {answer['networks']}; "
          f"{failed_count(answer['failures'])} failed queries before: "
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


def check_old_link_drop(seen: dict) -> None:
    """node1 dropped its LAN links to node2 within DROP_AFTER_NEW s of the new link."""
    new_at, gone_at = seen.get("new_at"), seen.get("old_gone_at")
    print(f"oracle: old links {seen.get('old_ids')} (networks at start "
          f"{seen.get('at_start')}) gone at {gone_at} s; new link at {new_at} s "
          f"over {seen.get('new_networks')}")
    assert new_at is not None, (
        f"node1 listed no new link to node2 while the driver watched: {seen}")
    assert gone_at is not None, (
        f"node1 still lists an old LAN link to node2 ({seen.get('old_ids')}) "
        f"at the driver's end: its keepalive and the new link's probe did not close it")
    assert gone_at - new_at <= DROP_AFTER_NEW, (
        f"node1 dropped its old links to node2 {gone_at - new_at:.2f} s after the "
        f"new link; want within {DROP_AFTER_NEW} s")


def check_endpoint_table(seen: dict) -> None:
    """node1's table held node2's own endpoints, written after the new link, within ENDPOINTS_WITHIN s of it."""
    ep, new_at = seen.get("endpoints") or {}, seen["new_at"]
    print(f"oracle: node2 advertises {ep.get('own')}; node1 holds {ep.get('held')}; "
          f"fresh at {ep.get('at')} s, last check {ep.get('checked_at')} s: "
          f"{ep.get('stale')}")
    assert ep.get("at") is not None, (
        f"node1's endpoint table never held node2's own endpoints written after "
        f"the new link (at {new_at} s): {ep.get('stale')}")
    assert ep["at"] - new_at <= ENDPOINTS_WITHIN, (
        f"node1's endpoint table held node2's own endpoints {ep['at'] - new_at:.2f} s "
        f"after the new link; want within {ENDPOINTS_WITHIN} s")


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]

    answer = doc["facts"]["first_answer"]
    check_first_answer(answer)
    seen = answer.get("tcp") or {}
    if seen:
        check_old_link_drop(seen)
        check_endpoint_table(seen)
    else:
        print("oracle: no link watch recorded (an operator-driven run); "
              "the old-link drop and the endpoint table are not judged")

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
