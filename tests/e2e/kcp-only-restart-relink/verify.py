#!/usr/bin/env python3
"""Oracle: kcp-only siblings relink after a restart, drop the dead link, and fail a dead dial fast.

The runner restarts node2 between driver and oracle (test.toml `restart`).
Judged here, from the driver's fact `kcp_restart_relink` and from what the
nodes answer now:

- setup: before the restart, node1 and node2 held links to each other, all
  kcp, and tcp dial and listen stay off on both;
- criterion 3: the dial to a kcp address with nothing behind it answered an
  error, no link, within DEAD s. kcp's default `dial_timeout` is 5 s
  (mod/kcp/src/config.go); a kcp dial with no bound waits for the caller's
  budget (DEAD_BUDGET in script.py);
- criterion 1: node2 holds a kcp link to node1 within WAIT s, and node2's
  log shows it linked within WITHIN s of its link task starting
  (`starting to maintain link to <node1>` to `link to <node1> established`
  or `restored after N attempts`). The basic strategy dials kcp rows
  (mod/nodes/src/basic_link_strategy.go);
- criterion 2: node1 drops every link it held to node2's old process within
  DROP s of the oracle's start: a new link probes the older ones
  (mod/nodes/src/link_pool.go probeOlderLinks), and the kcp keepalive closes
  an idle dead link after `link_idle_ping` 15 s plus `link_ping_timeout`
  10 s (mod/kcp/src/config.go);
- a query from node1 to node2 is answered, with no `nodes.close_link`.

why node2's log for criterion 1: the runner waits for node2 to answer before
the oracle starts, and node2's link task starts at boot, so the oracle's own
clock sees only the end of the relink.
"""
import asyncio
import re
import time
from datetime import timedelta

import astral

from lib import linkview, nodefs
from lib.sessionio import load

WITHIN = 5.0
DEAD = 10.0
WAIT = 60.0
DROP = 15.0 + 10.0 + 10.0
POLL = 0.25
TCP_SETTINGS = ("/mod/tcp/settings/dial", "/mod/tcp/settings/listen")
ANSI = re.compile(r"\x1b\[[0-9;]*m")
STAMP = r"(\d\d):(\d\d):(\d\d)\.(\d{3})"
START = re.compile(rf"{STAMP} \[user\] starting to maintain link to (\S+)\s*$")
LINKED = re.compile(rf"{STAMP} \[user\] link to (\S+) (?:established|restored after \d+ attempts)\s*$")


def seconds(m: re.Match) -> float:
    h, mi, s, ms = (int(g) for g in m.groups()[:4])
    return timedelta(hours=h, minutes=mi, seconds=s, milliseconds=ms).total_seconds()


def relink_took(node2: dict, names: set):
    """node2's last link task start to node1 and the link that followed, in s.

    why names: node2 logs node1 by its alias when it holds one, else by its
    identity.
    """
    lines = ANSI.sub("", nodefs.log(node2)).splitlines()
    starts = [(i, m) for i, l in enumerate(lines)
              if (m := START.search(l)) and m.group(5) in names]
    if not starts:
        return None
    at, start = starts[-1]
    linked = next((m for l in lines[at + 1:]
                   if (m := LINKED.search(l)) and m.group(5) in names), None)
    if linked is None:
        return None
    # why modulo: the log stamps carry no date, and a relink may cross midnight
    return (seconds(linked) - seconds(start)) % 86400


async def watch(c1, c2, id1: str, id2: str, stale: set) -> dict:
    """Poll both ends until node2 holds a kcp link to node1 and node1 holds
    none of the stale ids, or WAIT s pass. Times in s since the start."""
    t0 = time.monotonic()
    seen = {"relinked": None, "dropped": None, "links1": [], "links2": []}
    while time.monotonic() - t0 < WAIT:
        t = round(time.monotonic() - t0, 2)
        seen["links1"] = linkview.to(await linkview.links(c1), id2)
        seen["links2"] = linkview.to(await linkview.links(c2), id1)
        if seen["relinked"] is None and any(r["network"] == "kcp" for r in seen["links2"]):
            seen["relinked"] = t
        if seen["dropped"] is None and not stale & {r["id"] for r in seen["links1"]}:
            seen["dropped"] = t
        if seen["relinked"] is not None and seen["dropped"] is not None:
            break
        await asyncio.sleep(POLL)
    return seen


async def main():
    doc = load()
    f = doc["facts"]["kcp_restart_relink"]
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    id1, id2 = n1["identity"], n2["identity"]
    print(f"oracle: driver facts {f}")

    before1, before2 = f["before"]["node1"], f["before"]["node2"]
    assert f["settled"] and before1 and before2, (
        f"the pair did not settle on kcp links before the restart: node1 {before1}, node2 {before2}")
    assert all(n == "kcp" for _, n in before1 + before2), (
        f"the pair held a non-kcp link before the restart: node1 {before1}, node2 {before2}")

    dead = f["dead"]
    assert "error" in dead, f"the dial to a dead kcp address produced a link: {dead}"
    assert dead["elapsed"] <= DEAD, (
        f"the dial to a dead kcp address failed after {dead['elapsed']} s, want within {DEAD} s "
        f"(the caller's budget was {dead['budget']} s): {dead['error']}")

    restarts = (n2.get("restarts") or 0) - f["node2_restarts_before"]
    assert restarts == 1, f"node2 was restarted {restarts} times between driver and oracle, not once"

    stale = {i for i, _ in before1}
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c1, \
            await astral.connect(n2["endpoint"], token=n2["token"]) as c2, \
            await astral.connect(n1["endpoint"], token=doc["facts"]["user_token"]) as cu:
        seen = await watch(c1, c2, id1, id2, stale)
        print(f"oracle: relinked {seen['relinked']} s, dropped {seen['dropped']} s; "
              f"node1 {seen['links1']}; node2 {seen['links2']}")

        for c, who in ((c1, "node1"), (c2, "node2")):
            for path in TCP_SETTINGS:
                value = await c.tree.get(path)
                assert value == False, f"{who}'s {path} reads {value!r}; the pair is not kcp-only"  # noqa: E712

        assert seen["relinked"] is not None, (
            f"node2 holds no kcp link to node1 {WAIT} s after the oracle started: {seen['links2']}")
        took = relink_took(n2, {id1, "node1"})
        assert took is not None, "node2's log holds no link task start followed by a link to node1"
        assert took <= WITHIN, (
            f"node2 linked to node1 {took:.2f} s after its link task started; want within {WITHIN} s")

        assert seen["dropped"] is not None and seen["dropped"] <= DROP, (
            f"node1 still lists a link to node2's old process {seen['dropped'] or WAIT} s after the "
            f"oracle started, want within {DROP} s: stale {sorted(stale)}, node1 {seen['links1']}")
        assert all(r["network"] == "kcp" for r in seen["links1"] + seen["links2"]), (
            f"a non-kcp link joined the pair: node1 {seen['links1']}, node2 {seen['links2']}")

        await cu.nodes.links(experimental=True, target=id2, timeout=10)

    print(f"oracle: dead kcp dial failed in {dead['elapsed']} s; node2 relinked over kcp "
          f"{took:.2f} s after its link task started; node1 dropped the old link "
          f"{seen['dropped']} s into the oracle; node1 queried node2")


asyncio.run(main())
