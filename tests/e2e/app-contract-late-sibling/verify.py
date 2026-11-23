#!/usr/bin/env python3
"""Oracle: an app registered on node1 while node2 was down answers the first
query an app on node2 sends it after node2 returns.

- the runner restarted node2 once and node1 not at all, and node2 came back
  on the endpoint the driver saw;
- the provider app while_down.py started is alive and registered after the
  driver ended, so node2 was down;
- node2 lists a link to node1 within 30 s;
- one query, no retry, from a fresh app on node2 to the provider's `hello`
  answers `ok` within 5 s, and the reply names the calling app.

A member not linked when an app registers receives its contract at its next
first link to a member that holds it, and a member that missed both
deliveries obtains it when it routes a query to the app (astral-docs
topics/app-routing.md:164-168, Reach). The 5 s bound is the 3 s wait for that
pull (:69-70, Resolving the host) plus a relayed round trip on loopback.
node2's `apphost.hosts` for the app, read before the call, shows which path
delivered the contract; it is printed, not judged.
"""
import asyncio
import time

import astral

from lib import apps, jsonops, linkview, meshapps
from lib.sessionio import load

PREFIX = "m2-late"
LINKED = 30.0
DEADLINE = 15
BOUND_MS = 5000


async def await_link(node2: dict, node1_id: str) -> float | None:
    """Seconds until node2 listed a link to node1; None when not within LINKED."""
    async with await astral.connect(node2["endpoint"], token=node2["token"]) as c:
        start = time.monotonic()
        while time.monotonic() - start < LINKED:
            if linkview.to(await linkview.links(c), node1_id):
                return time.monotonic() - start
            await asyncio.sleep(0.5)
    return None


async def hosts_of(node2: dict, app_id: str) -> list:
    async with await astral.connect(node2["endpoint"], token=node2["token"]) as c:
        docs = await jsonops.call(c, f"apphost.hosts?app={app_id}&out=json")
    return [d["Object"] for d in docs if d["Type"] == "identity"]


def check_restart(nodes: dict, f: dict) -> None:
    restarts = (nodes["node2"].get("restarts") or 0) - f["node2_restarts_before"]
    assert restarts == 1, f"node2 was restarted {restarts} times between driver and oracle, not once"
    assert (nodes["node1"].get("restarts") or 0) == f["node1_restarts_before"], (
        "node1 was restarted; only node2 may be")
    assert nodes["node2"]["endpoint"] == f["node2_endpoint"], (
        f"node2 came back on {nodes['node2']['endpoint']}, not {f['node2_endpoint']}")


def check_app(app: apps.App, f: dict) -> dict:
    assert app.alive(), f"the provider app on node1 is gone:\n{app.log_tail()}"
    st = app.state()
    assert st.get("registered_at") and st["registered_at"] > f["driver_end"], (
        f"the provider registered at {st.get('registered_at')}, not after the driver "
        f"ended at {f['driver_end']}, when node2 went down")
    return st


def main():
    doc = load()
    nodes, f = doc["nodes"], doc["facts"]["late_sibling"]
    app = apps.get(meshapps.app_name(PREFIX, "node1"))
    try:
        check_restart(nodes, f)
        st = check_app(app, f)
        linked = asyncio.run(await_link(nodes["node2"], nodes["node1"]["identity"]))
        assert linked is not None, f"node2 listed no link to node1 within {LINKED:.0f} s"
        hosts = asyncio.run(hosts_of(nodes["node2"], st["app"]))
        r = meshapps.call(nodes["node2"], target=st["app"], as_app=True, timeout=DEADLINE)
    finally:
        app.stop()
    print(f"oracle: node2's apphost.hosts for the provider before the call: {hosts}")
    assert r["outcome"] == "ok", (
        f"the first query from an app on node2 to the provider ended {r['outcome']}: {r}")
    assert r["ms"] <= BOUND_MS, f"the first query took {r['ms']:.0f} ms; want ≤ {BOUND_MS}"
    assert r["reply"] == f"hello, {r['caller']}", (
        f"the provider answered {r['reply']!r}; want 'hello, {r['caller']}'")
    print(f"oracle: node2 linked node1 {linked:.1f} s into the oracle; the first query "
          f"from an app on node2 to the late provider answered ok in {r['ms']:.0f} ms")


main()
