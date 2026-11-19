#!/usr/bin/env python3
"""Oracle: a new link closes the black-holed one, and node1 syncs over the new one.

node2 linked to node1 through the driver's relay alone, the relay then dropped
every byte, and an app registered on node1 while node2 was out of reach. When
node2 linked to node1 directly, node1 pinged the older link once
(astrald mod/nodes/src/link_pool.go, probeOlderLinks), closed it within its
3 s ping timeout, and the LinkClosedEvent that left one link ran the sibling
sync (mod/user/src/object_receiver.go, onPartialLinkClose), which pushed the
app's relay contract to node2. The oracle reads the driver's facts, checks in
node1's log that a sibling sync with node2 started once the black-holed link
closed on its ping timeout, and asks node2 once more.
"""
import asyncio

import astral

from lib import jsonops
from lib.sessionio import load

STEPS = ["only_the_relayed_link_left", "node2_missed_the_app",
         "node1_dropped_the_relayed_link", "node2_knows_the_app_on_node1"]

# the probe's ping timeout (test.toml) plus slack for the close to show in nodes.links
DROP_LIMIT_S = 3 + 7


def check_sync_after_close(log: dict) -> None:
    """node1's log shows the close on the ping timeout and a sync started after it.

    why: the sync the new link starts also reaches node2 once the dead link
    closes; only a sync that starts after the close is the one the close ran.
    """
    tap = log.get("tap", {})
    assert not tap.get("refused") and not tap.get("broken"), (
        f"node1's log record is incomplete: {tap}")
    closes = log.get("closed_on_ping_timeout", [])
    assert closes, (
        f"node1 logged no close of a link to node2 on its ping timeout; "
        f"syncs: {log.get('syncs')}")
    closed_at = closes[0]["t"]
    after = [e for e in log.get("syncs", []) if e["t"] >= closed_at]
    assert after, (
        f"node1 started no sync with node2 after it closed the black-holed link "
        f"({closes[0]['text']}); syncs: {log.get('syncs')}")


async def main():
    doc = load()
    facts = doc["facts"].get("reconnect_sync", {})
    missing = [s for s in STEPS if facts.get(s) is not True]
    assert not missing, f"steps that did not hold: {missing}; facts: {facts}"

    assert facts["seconds_to_drop"] <= DROP_LIMIT_S, (
        f"node1 dropped the black-holed link {facts['seconds_to_drop']:.1f} s after "
        f"the new link; want at most {DROP_LIMIT_S} s")

    check_sync_after_close(facts.get("node1_log", {}))

    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        docs = await jsonops.call(c, f"apphost.hosts?app={facts['app']}&out=json")
    hosts = [d["Object"] for d in docs if d["Type"] == "identity"]
    assert n1["identity"] in hosts, (
        f"apphost.hosts on node2 named {hosts} for the app; want node1 "
        f"{n1['identity']}: {docs}")

    print(f"oracle: node1 dropped the black-holed link {facts['seconds_to_drop']:.1f} s "
          f"after the new link, and node2 knew node1's app "
          f"{facts['seconds_to_learn']:.1f} s after it")


asyncio.run(main())
