#!/usr/bin/env python3
"""Driver: a participant hosted on node1 mails one hosted on node2, and is
turned away by another.

node1's admin mints ann with messaging.create_identity, and node2's admin
mints ben and cal. ann writes to ben, ben reads it, and the receipt travels
back to node1; ben answers, and ann reads the answer. Then ann writes to cal,
and last asks ben for a path nothing on node2 serves. Each node serves its own
external authority, and each admits only its own participant's side of the
exchange: node1 lets ann send to ben and cal and receive from ben, node2 lets
ben receive from ann and send to her. node2 lets cal receive from nobody.

why node2 introduces ben's and cal's relay contracts to node1: node1 finds the
node that hosts an identity through a relay contract that identity issued
(mod/apphost/src/query_preprocessor.go), and nothing hands node1 either one.
node2 pushes them, and node1 indexes a relay contract pushed by the sibling it
names as subject (mod/user/src/object_receiver.go). The way back needs no step:
node1 pushes ann's relay contract as the caller proof of its first relayed
delivery.

why cal is a third participant and not ben refused later: the oracle reads one
answer per question from each authority, and a refusal of ben would put a
second answer to a question the exchange already asked.

why the unknown path goes to ben: ben is reached only through his relay
contract, so node1 asks it through the same relay path as the send to cal, and
node2 has no route for it.

The driver acts and judges nothing. It records what every op answered and
every question each authority was asked.
"""
import asyncio

import astral
from astral.errors import AstralError

from lib import jsonops, mail
from lib.authority import RECEIVE, SEND, Authority
from lib.sessionio import load, write_facts

ASK = "ann-asks-across-0xC0FFEE"
ANSWER = "ben-answers-across-0xBEEF"
TURNED_AWAY = "ann-writes-to-cal-0xDEAD"
UNKNOWN_PATH = "e2e.no_such_path"
RELAY = "mod.nodes.relay_for_action"
WAIT = "10s"

# why a deadline on the stamps: a receipt is sent after the read has answered,
# on a goroutine nothing waits for, so the sender's row is stamped a moment
# after the reader holds the body.
STAMP_DEADLINE = 10.0


async def introduce(admin, participant: dict, node: str) -> list:
    """The push of a participant's relay contract to `node`, as `node`
    answered it."""
    relay = [c for c in await mail.contracts_of(admin, participant["Identity"])
             if mail.permits(c) == [(RELAY, 0, None)]]
    return await jsonops.call(admin, "objects.push?in=json&out=json",
                              relay[0], eos=True, target=node)


async def mint(n1: dict, n2: dict) -> tuple:
    """ann on node1, ben and cal on node2, and node2's push of each one's
    relay contract to node1, answered by node1."""
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as a1:
        ann = await mail.create_identity(a1, "two-ann")
    async with await astral.connect(n2["endpoint"], token=n2["token"]) as a2:
        ben = await mail.create_identity(a2, "two-ben")
        cal = await mail.create_identity(a2, "two-cal")
        pushed = {"ben": await introduce(a2, ben, n1["identity"]),
                  "cal": await introduce(a2, cal, n1["identity"])}
    return ann, ben, cal, pushed


async def stamped(client, list_name: str, id: str, field: str) -> dict:
    """The caller's envelope for one message, once `field` is set on it, or
    as it stands at the deadline."""
    deadline = asyncio.get_running_loop().time() + STAMP_DEADLINE
    while True:
        rows = await mail.list_messages(client, list=list_name)
        row = next((r for r in rows if r["ID"] == id), {})
        if row.get(field) or asyncio.get_running_loop().time() > deadline:
            return row
        await asyncio.sleep(0.2)


async def exchange(n1: dict, n2: dict, ann: dict, ben: dict) -> dict:
    async with await astral.connect(n1["endpoint"], token=ann["Token"]) as ca, \
            await astral.connect(n2["endpoint"], token=ben["Token"]) as cb:
        parked = asyncio.create_task(mail.wait(cb, WAIT))
        await asyncio.sleep(0.5)
        asked = await mail.send(ca, ben["Identity"], ASK)
        waited = await parked
        heard = await mail.read(cb, ("inbox", asked))
        fetched = await stamped(ca, "outbox", asked, "FetchedAt")
        receipted = await stamped(cb, "inbox", asked, "ReceiptStoredAt")

        answered = await mail.send(cb, ann["Identity"], ANSWER, parent=asked)
        back = await mail.wait(ca, WAIT)
        got = await mail.read(ca, ("inbox", answered))
    return {"asked": asked, "answered": answered, "waited": waited,
            "heard": heard, "fetched": fetched, "receipted": receipted,
            "back": back, "got": got}


async def turn_away(n1: dict, ann: dict, cal: dict) -> dict:
    """How ann's send to cal ended. node2's authority refuses cal's side."""
    async with await astral.connect(n1["endpoint"], token=ann["Token"]) as ca:
        try:
            sent = await mail.send(ca, cal["Identity"], TURNED_AWAY)
        except (AstralError, jsonops.OpError, OSError) as e:
            return {"refused": True, "kind": type(e).__name__,
                    "detail": str(e)}
    return {"refused": False, "kind": "", "detail": sent}


async def ask_unknown(n1: dict, ann: dict, ben: dict) -> dict:
    """How ann's query of a path nothing on node2 serves ended, addressed to
    ben through node1."""
    async with await astral.connect(n1["endpoint"], token=ann["Token"]) as ca:
        try:
            async with ca.stream(UNKNOWN_PATH, target=ben["Identity"],
                                 raw=True):
                return {"kind": "accepted", "code": None, "detail": ""}
        except AstralError as e:
            return {"kind": type(e).__name__,
                    "code": getattr(e, "code", None), "detail": str(e)}


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]

    ann, ben, cal, pushed = await mint(n1, n2)
    a, b, c = (p["Identity"].lower() for p in (ann, ben, cal))
    facts = {"ann": a, "ben": b, "cal": c, "ann_token": ann["Token"],
             "ben_token": ben["Token"], "pushed": pushed, "ask": ASK,
             "answer": ANSWER, "unknown_path": UNKNOWN_PATH}

    first = Authority(n1["authority_url"],
                      {(SEND, a, b), (RECEIVE, a, b), (SEND, a, c)})
    second = Authority(n2["authority_url"], {(RECEIVE, b, a), (SEND, b, a)})
    try:
        facts["exchange"] = await exchange(n1, n2, ann, ben)
        facts["turned_away"] = await turn_away(n1, ann, cal)
        facts["unknown"] = await ask_unknown(n1, ann, ben)
    finally:
        first.close()
        second.close()
    facts["questions"] = {"node1": first.questions, "node2": second.questions}
    write_facts(facts)

    x = facts["exchange"]
    print(f"driver: ann on node1 asked {x['asked']}; ben on node2 answered "
          f"{x['answered']}; ann's send to cal ended "
          f"{facts['turned_away']['detail']!r}; ann's query of "
          f"{UNKNOWN_PATH} on ben ended {facts['unknown']['kind']}; node1 "
          f"was asked {len(first.questions)} and node2 "
          f"{len(second.questions)} questions")


asyncio.run(main())
