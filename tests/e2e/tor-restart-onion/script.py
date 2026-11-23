#!/usr/bin/env python3
"""Driver: watch node2's onion while Tor stops and starts under a running astrald, then link to it over Tor.

enable-tor gave both nodes an onion; restart-tor stops Tor on node2 --in s
after it ran and starts it again --down s later. astrald runs throughout.

1. Taps node2's log for the whole flow, and every SAMPLE s reads the
   endpoints node2 resolves for itself: whether a tor row is there, and its
   address. The first time the tap holds node2's `is down: tor control
   connection closed` line, the driver notes the time it saw it.
2. Waits for the first sample after Tor is due back (the down line plus
   --down) that holds a tor row again.
3. As the User, node1 dials `nodes.new_link(node2, endpoint=tor:<onion>)`
   every RETRY s until node1 lists a tor link to node2, at most LINK_WAIT s
   from the row's return; then asks node2 for `user.info` and reads both
   ends' links.

why the down line marks the stop: the onion service goes down with Tor's
control connection, and astrald logs the loss as it withdraws the endpoint
(mod/tor/src/server.go accept). Seen on the host's clock it is the time
Tor stopped, late by at most one tap read; Tor starts --down s after the
stop, since restart-tor schedules both timers in one call.

why a new_link attempt is retried and the link table read: a tor dial may
answer before the link is up while the strategy keeps trying (tor-link's
script.py, mod/nodes/src/loader.go), so the link table is the answer.

The driver acts and judges nothing. It records the `tor_restart_onion` fact,
times in seconds since the driver began, on the host's clock.
"""
import asyncio
import shlex
import time
import tomllib
from contextlib import suppress
from pathlib import Path

import astral

from lib.nodelog import LogTap
from lib.sessionio import load, write_facts

SAMPLE = 1.0
DOWN_LINE = "is down: tor control connection closed"
NEEDLES = ("onion service", "server error")
# why: --in plus the op's verifier and the session open
DOWN_WAIT = 120.0
# why: past the oracle's 75 s, so a late return is measured rather than missed
BACK_WAIT = 120.0
LINK_WAIT = 240.0
RETRY = 10.0
DIAL = 60.0
QUERY = 30.0
CALL = 5.0


def op_args(test_dir: Path) -> dict:
    """The --in and --down of the restart-tor op on test_dir's manifest."""
    ops = tomllib.loads((test_dir / "test.toml").read_text())["ops"]
    words = next(shlex.split(o) for o in ops if o.split()[0] == "restart-tor")
    out = {"in": 30, "down": 0}
    for flag, value in zip(words, words[1:]):
        if flag in ("--in", "--down"):
            out[flag[2:]] = int(value)
    return out


async def onion(c) -> str | None:
    """The tor endpoint node2 resolves for itself, or None."""
    eps = await asyncio.wait_for(c.nodes.resolve_endpoints("localnode", experimental=True), CALL)
    return next((f"tor:{e.endpoint.address()}" for e in eps
                 if e.endpoint.network() == "tor"), None)


async def sample(c, tap: LogTap, clock, seen: dict) -> None:
    """Records node2's tor row every SAMPLE s, and when the tap first held the down line."""
    while True:
        t = clock()
        try:
            seen["samples"].append({"t": t, "onion": await onion(c)})
        except Exception as e:  # noqa: BLE001 — a missed sample is not an observation
            seen["samples"].append({"t": t, "error": f"{type(e).__name__}: {e}"})
        if seen["down_seen"] is None and tap.find(DOWN_LINE):
            seen["down_seen"] = clock()
        await asyncio.sleep(SAMPLE)


async def wait_for(check, deadline: float, clock):
    """check()'s first truthy answer, polled every SAMPLE s until deadline; else None."""
    while clock() < deadline:
        got = check()
        if got:
            return got
        await asyncio.sleep(SAMPLE)
    return None


def returned(seen: dict, due: float):
    """The first sample at or after due that holds a tor row."""
    return next((s for s in seen["samples"] if s["t"] >= due and s.get("onion")), None)


async def tor_links(c, peer: str) -> list:
    return [str(i.id) for i in await c.nodes.links(experimental=True)
            if str(i.remote_identity) == peer and str(i.network) == "tor"]


async def listed(c, peer: str, until: float, clock) -> list:
    """c's node's tor links to peer, polled every SAMPLE s until one shows or until passes."""
    while True:
        ids = await tor_links(c, peer)
        if ids or clock() >= until:
            return ids
        await asyncio.sleep(SAMPLE)


async def dial(c, peer: str, endpoint: str, clock) -> dict:
    """new_link to endpoint every RETRY s until c's node lists a tor link to peer, or LINK_WAIT."""
    start, attempts = clock(), []
    ids = await tor_links(c, peer)
    while not ids and clock() < start + LINK_WAIT:
        t = clock()
        try:
            await c.nodes.new_link(peer, endpoint=endpoint, experimental=True, timeout=DIAL)
            attempts.append({"t": t, "ended_at": clock(), "ok": True})
        except Exception as e:  # noqa: BLE001 — how each attempt failed is the observation
            attempts.append({"t": t, "ended_at": clock(), "ok": False,
                             "how": f"{type(e).__name__}: {e}"})
        ids = await listed(c, peer, min(clock() + RETRY, start + LINK_WAIT), clock)
    return {"at": clock() if ids else None, "ids": ids, "attempts": attempts}


async def info(c, peer: str, clock) -> dict:
    t = clock()
    try:
        await c.call("user.info", target=peer, timeout=QUERY)
        nets = sorted({str(i.network) for i in await c.nodes.links(experimental=True)
                       if str(i.remote_identity) == peer})
        return {"sent_at": t, "ended_at": clock(), "ok": True, "networks": nets}
    except Exception as e:  # noqa: BLE001 — how the query failed is the observation
        return {"sent_at": t, "ended_at": clock(), "ok": False, "how": f"{type(e).__name__}: {e}"}


async def link_over_tor(n1: dict, n2: dict, user_token: str, endpoint: str, clock) -> dict:
    """node1, as the User, links to node2 at endpoint and asks it for user.info."""
    async with await astral.connect(n1["endpoint"], token=user_token) as cu, \
            await astral.connect(n2["endpoint"], token=n2["token"]) as c2:
        link = await dial(cu, n2["identity"], endpoint, clock)
        if link["at"] is not None:
            link["info"] = await info(cu, n2["identity"], clock)
            link["node2_ids"] = await tor_links(c2, n1["identity"])
    return link


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    op = op_args(Path(__file__).parent)
    t0 = time.monotonic()
    clock = lambda: round(time.monotonic() - t0, 2)  # noqa: E731
    seen = {"op": op, "samples": [], "down_seen": None, "back": None, "link": None}

    async with LogTap(n2) as tap, \
            await astral.connect(n2["endpoint"], token=n2["token"]) as c2:
        sampler = asyncio.create_task(sample(c2, tap, clock, seen))
        try:
            if await wait_for(lambda: seen["down_seen"], DOWN_WAIT, clock) is not None:
                due = seen["down_seen"] + op["down"]
                seen["back"] = await wait_for(lambda: returned(seen, due),
                                              due + BACK_WAIT, clock)
            if seen["back"] is not None:
                seen["link"] = await link_over_tor(n1, n2, doc["facts"]["user_token"],
                                                   seen["back"]["onion"], clock)
        finally:
            sampler.cancel()
            with suppress(asyncio.CancelledError):
                await sampler
        seen["log"] = tap.find(*NEEDLES)
        seen["tap"] = tap.state()

    write_facts({"tor_restart_onion": seen})
    link = seen["link"] or {}
    print(f"driver: Tor down seen at {seen['down_seen']} s; onion back {seen['back']}; "
          f"tor link at {link.get('at')} s, user.info {link.get('info')}")


asyncio.run(main())
