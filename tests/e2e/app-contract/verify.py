#!/usr/bin/env python3
"""Oracle: no contract, no reach — and a contract grants it from either side.

The apps are gone by the time this runs, so the verdict comes from what each
act left in node1's repository. Every (act, asker) pair sends its own payload,
and the id of each is a content hash this oracle computes for itself, so "the
app was reached" is a question about bytes rather than about an error message.

That is the trap this test exists to avoid. An unroutable query and a query the
app never answered look identical from the caller's side, and a test judging
act 1 by the shape of its error would pass on a node where nothing worked at
all. Act 1 is judged by objects that must be **absent** while acts 2 and 3
leave theirs present — the same apps, the same handler, the same queries, one
difference.

The host and peer halves are reported separately because they fail separately:
a contract that makes an app addressable on its own node and never travels is a
different defect from one that grants nothing at all.
"""
import asyncio

import astral
from astral.objectid import object_id_of_bytes

from lib.sessionio import load

# (fact key, must the app have been reached)
EXPECTED = (
    ("no-contract.host", False),
    ("no-contract.peer", False),
    ("installed.host", True),
    ("installed.peer", True),
    ("signed.host", True),
    ("signed.peer", True),
)


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    facts = doc["facts"]
    payloads = facts["payloads"]

    async with await astral.connect(n1["endpoint"],
                                    token=facts["user_token"]) as c:
        held = {}
        for key, _ in EXPECTED:
            oid = str(object_id_of_bytes(payloads[key].encode()))
            held[key] = (oid, await c.objects.contains("local", oid))

    faults = []
    for key, want in EXPECTED:
        oid, present = held[key]
        answered = facts[key]
        if present != want:
            faults.append(
                f"{key}: node1 " + ("holds" if present else "does not hold")
                + f" {oid[:16]}…, and should" + ("" if want else " not"))
        elif want and answered != oid:
            faults.append(
                f"{key}: the app answered {answered!r}, not the {oid[:16]}… "
                "its payload hashes to")
        elif not want and answered is not None:
            faults.append(
                f"{key}: an answer ({answered!r}) came back from an app no "
                "contract names")

    assert not faults, "; ".join(faults)

    print("oracle: an app no contract names is unreachable from its own node "
          "and from a sibling; apphost.install_app and new_app_contract + "
          "sign_app_contract each make it answer from both")


asyncio.run(main())
