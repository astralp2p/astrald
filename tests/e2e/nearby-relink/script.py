#!/usr/bin/env python3
"""Driver: watch node1 and node2 while node2 is off the LAN and comes back.

The driver acts and judges nothing. lan-outage took node2 off the LAN just
before it ran, closed the pair's links, and will bring node2 back at the same
address after --for seconds. The driver records both nodes' link-task and
nearby-contact log lines throughout, and polls node1 every second: whether it
lists node2 in nearby, under which proof nonce, and whether it holds a link to
node2, and in which direction. It stops once the link has been back for a
while, or well after node2's return was due.

why the log: what is under test is a link task cutting its backoff short when
nearby hears the sibling, and only the node's log tells a woken attempt from
one whose backoff ran out (mod/user/src/maintain_link_task.go). The lines it
needs: `still trying to reconnect to X (attempt M)`, logged as attempt M+1
starts, every fifth attempt; `sibling X is nearby at IP, linking now`, the
wake; `link to X restored after N attempts`, the success.

why the taps open first: the first anchor line falls about half a minute after
the cut, once five quick attempts have failed, and a tap opened later would
miss it.
"""
import asyncio
import shlex
import time
import tomllib
from pathlib import Path

import astral

from lib import nearby
from lib.nodelog import LogTap
from lib.sessionio import load, write_facts

POLL = 1.0
# why: lets the woken link task log its success, and the other side's too
AFTER = 20.0
# why: past node2's return, the cache entry it refreshes and the relink a
# missed wake would leave to a 256 s backoff -- that case still ends here
MARGIN = 120.0
# the user module's link-task and nearby-contact lines
NEEDLES = ("trying to reconnect", "restored after", "is nearby at", "linking now",
           "is broken")


def outage() -> int:
    """--for of this test's lan-outage op."""
    ops = tomllib.loads((Path(__file__).parent / "test.toml").read_text())["ops"]
    words = next(shlex.split(o) for o in ops if o.startswith("lan-outage"))
    return int(words[words.index("--for") + 1])


async def look(n1: dict, peer: str) -> dict:
    """What node1 holds of peer: its nearby entry's nonce, and its links."""
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        entry = nearby.of(await nearby.statuses(c), peer)
        links = [{"network": l.network, "outbound": bool(l.outbound)}
                 for l in await c.nodes.links(experimental=True)
                 if str(l.remote_identity) == peer]
    return {"nonce": entry["nonce"] if entry else None, "links": links}


async def main():
    span = outage()
    nodes = load()["nodes"]
    n1, n2 = nodes["node1"], nodes["node2"]
    t0 = time.monotonic()
    timeline = []       # changes only: {"t" (s since the driver began), "nonce", "links"}

    async with LogTap(n1) as t1, LogTap(n2) as t2:
        first = await look(n1, n2["identity"])
        timeline.append({"t": 0.0, **first})
        back_at = None
        while True:
            now = await look(n1, n2["identity"])
            t = round(time.monotonic() - t0, 1)
            last = timeline[-1]
            if (now["nonce"], now["links"]) != (last["nonce"], last["links"]):
                timeline.append({"t": t, **now})
            if now["links"] and back_at is None and t > 1.0:
                back_at = t
            if back_at is not None and t >= back_at + AFTER:
                break
            if t >= span + MARGIN:
                break
            await asyncio.sleep(POLL)

        log = {"node1": [e for e in t1.find(*NEEDLES) if n2["identity"] in e["text"]],
               "node2": [e for e in t2.find(*NEEDLES) if n1["identity"] in e["text"]]}
        taps = {"node1": t1.state(), "node2": t2.state()}

    write_facts({"nearby_relink": {
        "outage": span, "watched": round(time.monotonic() - t0, 1),
        "first": first, "timeline": timeline, "back_at": back_at,
        "log": log, "taps": taps,
    }})
    print(f"driver: node2 off the LAN for {span}s; node1 "
          f"{'linked again at ' + str(back_at) + 's' if back_at else 'never linked again'}")


asyncio.run(main())
