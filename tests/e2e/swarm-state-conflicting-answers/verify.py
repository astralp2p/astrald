#!/usr/bin/env python3
"""Oracle (criterion 2.5): when two Privileged nodes answer the same request
differently, every node ends with the Decline.

- node1 signed the Approval and node2 the Decline;
- within 60 s, each of node1, node2 and node3 stores both answers (a merge is
  a union) and lists the request as declined: a Decline beats an Approval
  (astral-docs protocols/user/types/mod.user.request_status.md).
"""
import asyncio
import time

import astral

from lib import swarmstate as ss
from lib.sessionio import load

NAMES = ["node1", "node2", "node3"]
CONVERGE = 60.0


def answers(node: dict, req_id: str) -> dict:
    """{answer type: [issuers]} of the answers to req_id node stores."""
    out = {}
    for e in ss.entries(node).values():
        for kind in ("Approval", "Decline"):
            body = e["Object"].get(kind) if isinstance(e["Object"], dict) else None
            if body and body.get("RequestID") == req_id:
                out.setdefault(kind, []).append(body["Issuer"])
    return out


async def view(nodes: dict, req_id: str) -> dict:
    out = {}
    for n in NAMES:
        async with await astral.connect(nodes[n]["endpoint"], token=nodes[n]["token"]) as c:
            out[n] = {"status": await ss.status_of(c, req_id), "answers": answers(nodes[n], req_id)}
    return out


def settled(v: dict, f: dict) -> bool:
    return all(s["status"] == "declined"
               and f["approval_issuer"] in s["answers"].get("Approval", [])
               and f["decline_issuer"] in s["answers"].get("Decline", [])
               for s in v.values())


async def main():
    doc = load()
    nodes, f = doc["nodes"], doc["facts"]["conflicting_answers"]
    assert f["approval_issuer"] == nodes["node1"]["identity"], "node1 did not sign the Approval"
    assert f["decline_issuer"] == nodes["node2"]["identity"], "node2 did not sign the Decline"

    deadline = time.monotonic() + CONVERGE
    v = await view(nodes, f["request_id"])
    while not settled(v, f) and time.monotonic() < deadline:
        await asyncio.sleep(2)
        v = await view(nodes, f["request_id"])
    assert settled(v, f), f"not every node ends with the Decline within {CONVERGE:.0f} s: {v}"
    print("oracle: node1, node2 and node3 each hold both answers and list the request declined")


asyncio.run(main())
