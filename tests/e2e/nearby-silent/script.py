#!/usr/bin/env python3
"""Driver: make the reflector Silent, then give it every reason nearby has to send.

The driver acts and judges nothing. capture-ether started a capture of the
reflector's ether traffic before this driver ran. With the reflector's log
recorded, the driver stores Silent as the reflector's nearby mode, then asks
the reflector itself to broadcast its status and to scan, and asks node1 and
node2 to scan, each of which a non-silent node answers with a status. It
waits past one full status interval after the switch, has node1 and node2 scan
again, and records what node1, node2 and the reflector list of each other
after each step. Last it reads the capture's record from the reflector's tree.

why Silent is stored through the tree: nearby has no op for its mode, and the
tree is where nearby keeps and follows it (mod/nearby/src/deps.go modePath);
a change there is a mode change, on which nearby sends at once in any other
mode (mod/nearby/src/send.go runPeriodic).

why the reflector's own nearby.broadcast and nearby.scan: they reach the two
send paths a Silent node keeps (Broadcast and Scan), which its periodic loop,
an address change and a scan reply also go through.
"""
import asyncio
import json
import shlex
import time
import tomllib
from pathlib import Path

import astral
from astral.errors import AstralError

from lib import jsonops, nearby
from lib.nodelog import LogTap
from lib.sessionio import load, write_facts

# why: past the reflector's next status, at most 95 s after the switch
# (mod/nearby/src/send.go maxBroadcastInterval)
PERIODIC = 100.0
# why: a node answers a scan within the 2 s reply window
# (mod/nearby/src/scan_reply.go replyWindow)
REPLY = 5.0
SETTLE = 3.0
POLL = 2.0


def capture_args() -> tuple:
    """(--for, --name) of this test's capture-ether op."""
    ops = tomllib.loads((Path(__file__).parent / "test.toml").read_text())["ops"]
    words = next(shlex.split(o) for o in ops if o.startswith("capture-ether"))
    return int(words[words.index("--for") + 1]), words[words.index("--name") + 1]


def connect(node: dict):
    return astral.connect(node["endpoint"], token=node["token"])


async def view(nodes: dict) -> dict:
    """The proof nonce each node holds of each other node, None for no entry."""
    out = {}
    for k, node in nodes.items():
        async with await connect(node) as c:
            listing = await nearby.statuses(c)
        out[k] = {other: (nearby.of(listing, n["identity"]) or {}).get("nonce")
                  for other, n in nodes.items() if other != k}
    return out


async def scans(nodes: dict, who: tuple) -> dict:
    out = {}
    for k in who:
        async with await connect(nodes[k]) as c:
            out[k] = await nearby.scan(c)
    return out


async def record(reflector: dict, name: str, deadline: float) -> dict:
    """The capture's record from the reflector's tree, once it is there."""
    path = f"/e2e/capture/{name}"
    last = None
    while time.monotonic() < deadline:
        try:
            async with await connect(reflector) as c:
                docs = await jsonops.stream(c, f"tree.get?out=json&path={path}")
            if docs and isinstance(docs[0].get("Object"), str):
                return json.loads(docs[0]["Object"])
            last = f"tree.get answered {docs}"
        except (jsonops.OpError, AstralError) as e:
            last = f"{type(e).__name__}: {e}"
        await asyncio.sleep(POLL)
    return {"error": f"no record at {path}: {last}"}


async def main():
    t_start = time.monotonic()
    span, name = capture_args()
    nodes = {k: v for k, v in load()["nodes"].items() if k in ("node1", "node2", "reflector")}
    r = nodes["reflector"]
    views = {}

    async with LogTap(r) as tap:
        views["before"] = await view(nodes)
        async with await connect(r) as c:
            switch = await nearby.set_mode(c, nearby.SILENT)
        t_switch = time.monotonic()
        await asyncio.sleep(SETTLE)
        views["after_switch"] = await view(nodes)

        async with await connect(r) as c:
            own = {"broadcast": await nearby.broadcast(c), "scan": await nearby.scan(c)}
        asked = {"round1": await scans(nodes, ("node1", "node2"))}
        await asyncio.sleep(REPLY)
        views["round1"] = await view(nodes)

        await asyncio.sleep(max(0.0, t_switch + PERIODIC - time.monotonic()))
        asked["round2"] = await scans(nodes, ("node1", "node2"))
        await asyncio.sleep(REPLY)
        views["round2"] = await view(nodes)

        async with await connect(r) as c:
            stored = await nearby.stored_mode(c)
        # why from the driver's start: the capture began just before it, and
        # needs a moment after its window to write the record
        capture = await record(r, name, t_start + span + 60.0)
        mode_lines = tap.find("mode ")

    write_facts({"nearby_silent": {
        "switch": switch, "stored": stored, "own": own, "asked": asked,
        "views": views, "mode_log": mode_lines, "tap": tap.state(),
        "capture": capture, "span": span,
    }})
    print(f"driver: reflector set Silent ({switch}); capture "
          f"{'recorded ' + str(capture.get('sent_total')) + ' sent' if 'error' not in capture else capture['error']}")


asyncio.run(main())
