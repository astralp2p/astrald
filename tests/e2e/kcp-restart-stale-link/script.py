#!/usr/bin/env python3
"""Driver: watch node1's kcp links to node2 while node2's astrald is killed and starts again.

kcp-only-relink left the siblings joined by kcp alone: tcp between them is
dropped for good. restart-astrald kills node2's astrald --in seconds after it
ran, and systemd starts it again. The driver polls every POLL s:

- before node2 goes down: node1's links to node2, the last list before node2
  stops answering being the stale links, all to node2's old process;
- whether node2's apphost answers: when it stops (`down`) and answers again
  (`up`);
- after `down`: node1's links to node2, until node1 lists none of the stale
  links and a new one (`stale_gone_at`, `new_at`), or WAIT s pass.

why a timer and polls: a VM world restarts a node only through an op, which
runs before the driver, so the driver has to be watching when the node goes
down and comes back.

The driver acts and judges nothing; it never closes a link. It records the
`kcp_restart_stale` fact, times in seconds since the driver began, on the
host's clock.
"""
import asyncio
import shlex
import time
import tomllib
from pathlib import Path

import astral

from lib import linkview
from lib.sessionio import load, write_facts

POLL = 0.5
CONNECT = 2.0
# why: past the restart, a restart that takes long, and a link that does not drop
MARGIN = 90.0
# why: the oracle's 35 s bound, and room to see a link that drops late
WAIT = 60.0


def restart_in() -> int:
    """--in of this test's restart-astrald op."""
    ops = tomllib.loads((Path(__file__).parent / "test.toml").read_text())["ops"]
    words = next(shlex.split(o) for o in ops if o.startswith("restart-astrald"))
    return int(words[words.index("--in") + 1])


async def answers(node: dict) -> bool:
    try:
        async def ask():
            async with await astral.connect(node["endpoint"], token=node["token"]) as c:
                await c.call("apphost.whoami", timeout=CONNECT)
        await asyncio.wait_for(ask(), CONNECT)
        return True
    except Exception:  # noqa: BLE001 — a node that does not answer is the observation
        return False


async def links_to(c1, peer: str):
    """node1's links to peer as rows, or None when the poll fails."""
    try:
        return linkview.to(await asyncio.wait_for(linkview.links(c1), CONNECT), peer)
    except Exception:  # noqa: BLE001 — a missed poll is not an observation
        return None


def note_links(rows: list, now: float, seen: dict) -> None:
    """Records when node1 first listed none of the stale links, and its first new link."""
    stale = {r["id"] for r in seen["stale"]}
    ids = {r["id"] for r in rows}
    seen["links1"] = rows
    if ids & stale:
        seen["stale_gone_at"] = None
    elif seen["stale_gone_at"] is None:
        seen["stale_gone_at"] = now
    if seen["new_at"] is None and ids - stale:
        seen["new_at"] = now
        seen["new_links"] = [r for r in rows if r["id"] not in stale]


async def watch(c1, n2: dict, id2: str, t0: float, deadline: float) -> dict:
    """The stale links, down, up, and when node1 dropped the stale links, in s since t0."""
    seen = {"stale": [], "down": None, "up": None, "stale_gone_at": None,
            "new_at": None, "new_links": [], "links1": []}
    clock = lambda: round(time.monotonic() - t0, 2)  # noqa: E731
    while seen["down"] is None and clock() < deadline:
        rows = await links_to(c1, id2)
        if not await answers(n2):
            seen["down"] = clock()
            break
        if rows is not None:
            seen["stale"] = rows
        await asyncio.sleep(POLL)
    if seen["down"] is None:
        return seen
    end = min(deadline, seen["down"] + WAIT)
    while clock() < end:
        now = clock()
        if seen["up"] is None and await answers(n2):
            seen["up"] = now
        rows = await links_to(c1, id2)
        if rows is not None:
            note_links(rows, now, seen)
        if seen["up"] is not None and seen["stale_gone_at"] is not None and seen["new_at"] is not None:
            break
        await asyncio.sleep(POLL)
    return seen


async def main():
    nodes = load()["nodes"]
    n1, n2 = nodes["node1"], nodes["node2"]
    t0 = time.monotonic()
    span = restart_in()
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c1:
        seen = await watch(c1, n2, n2["identity"], t0, span + MARGIN)
    write_facts({"kcp_restart_stale": {"restart_in": span, **seen,
                                       "watched": round(time.monotonic() - t0, 2)}})
    print(f"driver: node2 down at {seen['down']} s, up at {seen['up']} s; node1 dropped "
          f"its stale links {seen['stale']} at {seen['stale_gone_at']} s; new link at "
          f"{seen['new_at']} s {seen['new_links']}; node1 now {seen['links1']}")


asyncio.run(main())
