#!/usr/bin/env python3
"""Oracle (criterion 2.5): when two Privileged nodes answer the same request
differently, every node ends with the same status.

- node2 signed the Decline, and node1 the offer when it was not refused;
- within 60 s, each of node1, node2 and node3 stores the Decline (a merge is
  a union) and lists the request with one status: `declined`, or `fulfilled`
  when node3, the filer, co-signed node1's offer before the Decline reached
  it. A Decline only closes its request and takes back no contract both
  parties signed (astral-docs protocols/user/types/mod.user.request_status.md,
  PR 4 decision Q1).
"""
import asyncio
import time

import astral

from lib import swarmstate as ss
from lib.sessionio import load

NAMES = ["node1", "node2", "node3"]
CONVERGE = 60.0


def decliners(node: dict, req_id: str) -> list:
    """The issuers of the Declines of req_id node stores."""
    out = []
    for e in ss.entries(node).values():
        body = e["Object"].get("Decline") if isinstance(e["Object"], dict) else None
        if body and body.get("RequestID") == req_id:
            out.append(body["Issuer"])
    return out


async def view(nodes: dict, req_id: str) -> dict:
    out = {}
    for n in NAMES:
        async with await astral.connect(nodes[n]["endpoint"], token=nodes[n]["token"]) as c:
            out[n] = {"status": await ss.status_of(c, req_id), "decliners": decliners(nodes[n], req_id)}
    return out


def settled(v: dict, f: dict) -> bool:
    statuses = {s["status"] for s in v.values()}
    return len(statuses) == 1 and statuses <= {"declined", "fulfilled"} \
        and all(f["decline_issuer"] in s["decliners"] for s in v.values())


async def main():
    doc = load()
    nodes, f = doc["nodes"], doc["facts"]["conflicting_answers"]
    assert f["decline_issuer"] == nodes["node2"]["identity"], "node2 did not sign the Decline"
    assert f["offer_issuer"] in (None, nodes["node1"]["identity"]), "node1 did not sign the offer"

    deadline = time.monotonic() + CONVERGE
    v = await view(nodes, f["request_id"])
    while not settled(v, f) and time.monotonic() < deadline:
        await asyncio.sleep(2)
        v = await view(nodes, f["request_id"])
    assert settled(v, f), f"the nodes do not agree on declined or fulfilled within {CONVERGE:.0f} s: {v}"
    status = v["node1"]["status"]
    assert status == "declined" or f["offer_issuer"] is not None, "fulfilled with no offer"
    print(f"oracle: node1, node2 and node3 each hold the Decline and list the request {status}")


asyncio.run(main())
