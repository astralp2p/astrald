#!/usr/bin/env python3
"""Driver: freeze node2 and time how fast node1's keepalive closes its links.

node1 and node2 are linked siblings, with tcp's and kcp's link_idle_ping and
link_ping_timeout at 2 s on both (test.toml). SIGSTOP keeps node2's sockets
open, so its kernel still acknowledges what node1 sends: only node1's
keepalive probe can tell the link is dead.

1. Taps node1's log and waits until node1's links to node2 hold still.
2. Opens a stream over the link: `log.listen` on node2, asked through node1
   as the User (`user_token`), whom node2 lets see its state.
3. Freezes node2 (SIGSTOP) and at once sends a query to node2
   (`user.info`), which no one answers.
4. Polls node1's links every 0.2 s until none leads to node2, and records
   when the stream and the query ended and how.
5. Resumes node2 (SIGCONT), always, and polls both ends until they are
   linked again.

The driver judges nothing. It records the `dead_peer_keepalive` fact, times
in seconds since the freeze.
"""
import asyncio
import time
from contextlib import AsyncExitStack

import astral

from lib import linkview, nodeproc
from lib.nodelog import LogTap
from lib.sessionio import load, write_facts

POLL = 0.2
GONE = 30.0      # node1 must drop the links well within this
QUERY = 90.0     # past the 60 s routing deadline, so a hang shows as one
RELINK = 60.0


async def links_to(c, peer: str) -> list:
    return sorted(linkview.ids(await linkview.links(c), peer))


async def drain(stream, t0: list) -> dict:
    """Read the stream to its end; when and how it ended, since the freeze, and its first bytes."""
    first = b""
    try:
        while chunk := await stream.read_bytes(65536):
            first = first or chunk[:200]
        how = "eof"
    except Exception as e:  # noqa: BLE001 — how the stream ended is the observation
        how = f"{type(e).__name__}: {e}"
    return {"ended_at": round(time.monotonic() - t0[0], 2) if t0 else None, "how": how,
            "first": first.decode(errors="replace")}


async def ask(c, peer: str, t0: float) -> dict:
    """A query to the frozen peer; how and when it ended, since the freeze."""
    try:
        got = await c.call("user.info", target=peer, timeout=QUERY)
        how = f"answered: {got!r}"
        ok = True
    except Exception as e:  # noqa: BLE001
        how, ok = f"{type(e).__name__}: {e}", False
    return {"ended_at": round(time.monotonic() - t0, 2), "how": how, "answered": ok}


async def wait_gone(c, peer: str, t0: float) -> float | None:
    while time.monotonic() - t0 < GONE:
        if not await links_to(c, peer):
            return round(time.monotonic() - t0, 2)
        await asyncio.sleep(POLL)
    return None


async def wait_relinked(c1, c2, id1: str, id2: str, t: float) -> float | None:
    while time.monotonic() - t < RELINK:
        try:
            if await links_to(c1, id2) and await links_to(c2, id1):
                return round(time.monotonic() - t, 2)
        except Exception:  # noqa: BLE001 — node2 may still be waking
            pass
        await asyncio.sleep(POLL)
    return None


async def main():
    doc = load()
    nodes = doc["nodes"]
    n1, n2 = nodes["node1"], nodes["node2"]
    id1, id2 = n1["identity"], n2["identity"]
    fact = {}
    async with AsyncExitStack() as stack:
        c1 = await stack.enter_async_context(
            await astral.connect(n1["endpoint"], token=n1["token"]))
        tap = await stack.enter_async_context(LogTap(n1))
        before, settled = await linkview.settle(lambda: links_to(c1, id2), timeout=60.0)
        fact.update(before=before, settled=settled)
        mark = len(tap.entries)

        cu = await stack.enter_async_context(
            await astral.connect(n1["endpoint"], token=doc["facts"]["user_token"]))
        stream = await stack.enter_async_context(cu.stream(
            "log.listen?out=json", target=id2, raw=True, persistent=True, timeout=None))
        t0: list = []
        reader = asyncio.create_task(drain(stream, t0))
        await asyncio.sleep(1.0)
        fact["stream_open_before_freeze"] = not reader.done()

        pid = nodeproc.freeze(n2["root"])
        t0.append(time.monotonic())
        try:
            query = asyncio.create_task(ask(c1, id2, t0[0]))
            fact["gone_at"] = await wait_gone(c1, id2, t0[0])
            fact["stream"] = await asyncio.wait_for(reader, GONE) if not reader.done() else reader.result()
            fact["query"] = await query
        finally:
            nodeproc.thaw(pid)
        t1 = time.monotonic()
        fact["closed"] = [e["text"] for e in tap.entries[mark:]
                          if "closed" in e["text"] and id2 in e["text"]]
        fact["tap"] = tap.state()

        c2 = await stack.enter_async_context(
            await astral.connect(n2["endpoint"], token=n2["token"]))
        fact["relinked_after"] = await wait_relinked(c1, c2, id1, id2, t1)

    write_facts({"dead_peer_keepalive": fact})
    print(f"driver: links {fact['before']} gone at {fact['gone_at']} s; stream "
          f"{fact['stream']}; query {fact['query']}; relinked after "
          f"{fact['relinked_after']} s")


asyncio.run(main())
