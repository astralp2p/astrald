#!/usr/bin/env python3
"""Oracle: after a kcp-only sibling restarts, its peer drops the stale kcp link and reaches it again.

Ticket akchpFNjbO criterion 7 (S5 on kcp). Observed by the driver, judged
here (fact `kcp_restart_stale`), and from what the nodes answer now:

- the restart took: node2 stopped answering and answered again;
- before the restart node1 held links to node2, all kcp: kcp-only-relink
  dropped tcp between the pair for good;
- node1 lists none of the links it held to node2's old process within DROP
  s of node2 going down. A new link probes the older ones
  (mod/nodes/src/link_pool.go probeOlderLinks), and the kcp keepalive closes
  an idle dead link after `link_idle_ping` 15 s plus `link_ping_timeout`
  10 s (mod/kcp/src/config.go);
- node1 still lists none of them now;
- a query from node1 to node2 as the User is answered. Neither the driver
  nor the oracle calls `nodes.close_link`.

why from `down`: the old process's links die when it is killed, and the
keepalive counts from the last frame received, so the bound holds from the
kill whatever the restart takes. `down` is the first poll node2 did not
answer, so it is at or after the kill.

Distinct from `kcp-only-restart-relink` (`main.suite`, local or Docker),
which judges the same drop from the oracle's start, after a runner restart.
"""
import asyncio

import astral

from lib import linkview
from lib.sessionio import load

DROP = 15.0 + 10.0 + 10.0
QUERY = 30.0


async def main():
    doc = load()
    f = doc["facts"]["kcp_restart_stale"]
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    id2 = n2["identity"]
    print(f"oracle: down {f['down']} s, up {f['up']} s; stale {f['stale']}; stale gone "
          f"{f['stale_gone_at']} s; new link {f['new_at']} s {f['new_links']}; node1 {f['links1']}")

    assert f["down"] is not None, f"node2 never stopped answering in {f['watched']} s: the restart did not take"
    assert f["up"] is not None, f"node2 did not answer again within {f['watched']} s of the driver's start"
    assert f["stale"], "node1 held no link to node2 before the restart: nothing was stale to drop"
    assert all(r["network"] == "kcp" for r in f["stale"]), (
        f"node1 held a non-kcp link to node2 before the restart: {f['stale']}")

    gone = f["stale_gone_at"]
    assert gone is not None, (
        f"node1 still lists a link to node2's old process {f['watched']} s into the driver: "
        f"stale {f['stale']}, node1 {f['links1']}")
    assert gone - f["down"] <= DROP, (
        f"node1 dropped its links to node2's old process {gone - f['down']:.2f} s after node2 "
        f"went down; want within {DROP} s")

    stale = {r["id"] for r in f["stale"]}
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c1:
        now = linkview.to(await linkview.links(c1), id2)
    assert not stale & {r["id"] for r in now}, (
        f"node1 lists a link to node2's old process again: stale {sorted(stale)}, node1 {now}")

    async with await astral.connect(n1["endpoint"], token=doc["facts"]["user_token"]) as cu:
        await cu.call("user.info", target=id2, timeout=QUERY)

    print(f"oracle: node1 dropped its stale kcp links {gone - f['down']:.2f} s after node2 went "
          f"down, holds {now}, and node2 answered a query from node1")


asyncio.run(main())
