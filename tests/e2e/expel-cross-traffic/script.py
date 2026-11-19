#!/usr/bin/env python3
"""Driver: traffic across a ban, with apps and swarm follows live through it.

1. The Go driver (driver/) serves a pinger app on node1 and one on node3, a
   provider of a service on node1, and swarm follows of that service on node2
   and node3. It reports ready once both follows hold the provider's offering
   and node2 names node3 as the host of node3's app.
2. The User expels node3 on node1.
3. Once node2 holds the ban, the Go driver probes from node2: apphost.hosts
   of both apps, then a ping of each. node2's links to node3 are listed
   before and after the probe.
4. The flow holds until HOLD seconds after the expel, so a carry that kept
   retrying a former member would show.

Every node's log is tapped across the flow. The entries that tell a push, a
discovery, a relay or a link between node3 and the members are the
`cross_ban_log` fact; the probe is the `routing_probe` fact. The oracle
judges both.
"""
import asyncio
import json
import os
import time
from contextlib import AsyncExitStack

import astral

from lib import apps
from lib.nodelog import LogTap
from lib.sessionio import load, write_facts

READY = 180.0      # how long the Go driver may take to set up
CONVERGE = 30.0    # how long the ban may take to reach node2
# why a hold: a swarm follow that kept its carry to a former member retries
# it on a backoff from 1 s (mod/services/src/swarm.go carry), so the taps stay
# open long enough after the ban for a few retries to show.
HOLD = 12.0
# the entries that tell a push, a discovery, a relay or a link, and the
# moments the ban landed on each node
KEEP = ("services.discover", "rejecting pushed contract", "push relay contract",
        "refused relay", "removing sibling", "expelled", "-link with")


class Driver:
    """The Go driver: JSON lines on stdout; a "probe" line on stdin asks for
    one probe; closing stdin ends it."""

    def __init__(self, proc):
        self.proc = proc

    @classmethod
    async def start(cls):
        binary = apps.build_go("tests/e2e/expel-cross-traffic/driver",
                               name="expel-cross-traffic-driver")
        proc = await asyncio.create_subprocess_exec(
            str(binary), os.environ["ASTRAL_TESTS_SESSION"],
            stdin=asyncio.subprocess.PIPE, stdout=asyncio.subprocess.PIPE)
        return cls(proc)

    async def line(self, key: str, timeout: float) -> dict:
        deadline = time.monotonic() + timeout
        while True:
            raw = await asyncio.wait_for(self.proc.stdout.readline(),
                                         max(0.1, deadline - time.monotonic()))
            if not raw:
                code = await self.proc.wait()
                raise RuntimeError(f"Go driver exited {code} before {key!r}")
            doc = json.loads(raw)
            if key in doc:
                return doc[key]

    async def probe(self) -> dict:
        self.proc.stdin.write(b"probe\n")
        await self.proc.stdin.drain()
        return await self.line("probe", 90)

    async def stop(self) -> None:
        if self.proc.returncode is None and not self.proc.stdin.is_closing():
            self.proc.stdin.close()
        try:
            await asyncio.wait_for(self.proc.wait(), 30)
        except asyncio.TimeoutError:
            self.proc.kill()
            await self.proc.wait()


async def links_to(node: dict, peer: str) -> list:
    async with await astral.connect(node["endpoint"], token=node["token"]) as c:
        return sorted(str(l.id) for l in await c.nodes.links(experimental=True)
                      if str(l.remote_identity) == peer)


async def until_banned(node: dict, banned: str) -> bool:
    async with await astral.connect(node["endpoint"], token=node["token"]) as c:
        deadline = time.monotonic() + CONVERGE
        while time.monotonic() < deadline:
            for e in await c.user.list_expelled():
                if e.expulsion is not None and str(e.expulsion.subject) == banned:
                    return True
            await asyncio.sleep(0.5)
    return False


def kept(tap: LogTap, others: list) -> list:
    """The tap's entries that match KEEP and name one of others."""
    return [e for e in tap.find(*KEEP) if any(o in e["text"] for o in others)]


async def flow(doc: dict, driver: Driver) -> dict:
    n1, n2, n3 = (doc["nodes"][n] for n in ("node1", "node2", "node3"))
    id3 = n3["identity"]
    ready = await driver.line("ready", READY)

    expelled_at = time.monotonic()
    async with await astral.connect(n1["endpoint"],
                                    token=doc["facts"]["user_token"]) as c:
        signed = await c.user.expel(id3)
    expelled = str(signed.expulsion.subject)

    routing = {"ready": ready, "ban_on_node2": await until_banned(n2, id3)}
    routing["links_before"] = await links_to(n2, id3)
    routing["probe"] = await driver.probe()
    routing["links_after"] = await links_to(n2, id3)
    await asyncio.sleep(max(0.0, expelled_at + HOLD - time.monotonic()))
    return {"cross_traffic_expelled_id": expelled, "routing_probe": routing}


async def main():
    doc = load()
    nodes = doc["nodes"]
    ids = {n: nodes[n]["identity"] for n in ("node1", "node2", "node3")}
    driver = await Driver.start()
    try:
        async with AsyncExitStack() as stack:
            taps = {n: await stack.enter_async_context(LogTap(nodes[n])) for n in ids}
            facts = await flow(doc, driver)
            facts["cross_ban_log"] = {
                "node1": kept(taps["node1"], [ids["node3"]]),
                "node2": kept(taps["node2"], [ids["node3"]]),
                "node3": kept(taps["node3"], [ids["node1"], ids["node2"]]),
                "taps": {n: t.state() for n, t in taps.items()},
            }
    finally:
        await driver.stop()
    write_facts(facts)
    print(f"driver: routing probe {json.dumps(facts['routing_probe'])}")
    print("driver: kept log entries " + ", ".join(
        f"{n} {len(facts['cross_ban_log'][n])}" for n in ids))


asyncio.run(main())
