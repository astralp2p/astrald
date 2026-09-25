#!/usr/bin/env python3
"""Driver: a participant hosted on node1 mails one hosted on node2.

node1's admin mints ann with messaging.create_identity, and node2's admin
mints ben. ann writes to ben, ben reads it, and the receipt travels back to
node1; ben answers, and ann reads the answer. Each node serves its own external
authority, and each admits only its own participant's side of the exchange:
node1 lets ann send to ben and receive from him, node2 lets ben receive from
ann and send to her.

why node2 introduces ben's relay contract to node1: node1 finds the node that
hosts an identity through a relay contract that identity issued
(mod/apphost/src/query_preprocessor.go), and nothing hands node1 ben's. node2
pushes it, and node1 indexes a relay contract pushed by the sibling it names as
subject (mod/user/src/object_receiver.go). The way back needs no step: node1
pushes ann's relay contract as the caller proof of its first relayed delivery.

The driver acts and judges nothing. It records what every op answered and
every question each authority was asked.
"""
import asyncio

import astral

from lib import jsonops, mail
from lib.authority import RECEIVE, SEND, Authority
from lib.sessionio import load, write_facts

ASK = "ann-asks-across-0xC0FFEE"
ANSWER = "ben-answers-across-0xBEEF"
RELAY = "mod.nodes.relay_for_action"
WAIT = "10s"

# why a deadline on the stamps: a receipt is sent after the read has answered,
# on a goroutine nothing waits for, so the sender's row is stamped a moment
# after the reader holds the body.
STAMP_DEADLINE = 10.0


async def mint(n1: dict, n2: dict) -> tuple:
    """ann on node1, ben on node2, and node2's push of ben's relay contract to
    node1, answered by node1."""
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as a1:
        ann = await mail.create_identity(a1, "two-ann")
    async with await astral.connect(n2["endpoint"], token=n2["token"]) as a2:
        ben = await mail.create_identity(a2, "two-ben")
        relay = [c for c in await mail.contracts_of(a2, ben["Identity"])
                 if mail.permits(c) == [(RELAY, 0, None)]]
        pushed = await jsonops.call(a2, "objects.push?in=json&out=json",
                                    relay[0], eos=True, target=n1["identity"])
    return ann, ben, pushed


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


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]

    ann, ben, pushed = await mint(n1, n2)
    a, b = ann["Identity"].lower(), ben["Identity"].lower()
    facts = {"ann": a, "ben": b, "ann_token": ann["Token"],
             "ben_token": ben["Token"], "pushed": pushed, "ask": ASK,
             "answer": ANSWER}

    first = Authority(n1["authority_url"], {(SEND, a, b), (RECEIVE, a, b)})
    second = Authority(n2["authority_url"], {(RECEIVE, b, a), (SEND, b, a)})
    try:
        facts["exchange"] = await exchange(n1, n2, ann, ben)
    finally:
        first.close()
        second.close()
    facts["questions"] = {"node1": first.questions, "node2": second.questions}
    write_facts(facts)

    x = facts["exchange"]
    print(f"driver: ann on node1 asked {x['asked']}; ben on node2 answered "
          f"{x['answered']}; node1 was asked {len(first.questions)} and node2 "
          f"{len(second.questions)} questions")


asyncio.run(main())
