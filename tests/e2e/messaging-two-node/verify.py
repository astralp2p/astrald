#!/usr/bin/env python3
"""Oracle: mail crosses the link, and so does its receipt.

Four judgements, and the order matters. node1 must have taken ben's relay
contract, or the exchange never had a route. Each authority's record comes
next: the sender's side is asked on the sender's node and the recipient's side
on the recipient's, and an exchange proves nothing about reach unless both
were asked and admitted it. Then the exchange as the driver saw it.

Last, what each node holds. The oracle reads ann's outbox on node1 and ben's
inbox on node2 itself: the receipt is a claim about node1's row, and the driver
is the wrong party to make it.
"""
import asyncio

import astral

from lib import mail
from lib.authority import RECEIVE, SEND, answers
from lib.sessionio import load


def check_push(facts):
    assert facts["pushed"][:1] == [{"Type": "bool", "Object": True}], (
        f"node1 answered {facts['pushed']!r} to ben's relay contract — node1 "
        "never learned where ben is hosted, so the exchange had no route")


def check_authorities(facts):
    a, b = facts["ann"], facts["ben"]
    for node, kind, actor, other, who in (
            ("node1", SEND, a, b, "ann sending to ben"),
            ("node2", RECEIVE, b, a, "ben receiving from ann"),
            ("node2", SEND, b, a, "ben sending to ann"),
            ("node1", RECEIVE, a, b, "ann receiving from ben")):
        q = facts["questions"][node]
        got = answers(q, kind, actor, other)
        assert got == [True], (
            f"{node}'s authority answered {got!r} for {who}, not one yes — the "
            "question was put on the wrong node, named the wrong actor, or "
            f"never put; {node} was asked {q!r}")


def check_exchange(facts):
    x = facts["exchange"]
    waited = [m["ID"] for m in x["waited"]["Messages"]]
    assert waited == [x["asked"]], (
        f"ben's wait on node2 answered {x['waited']!r}, not ann's message")
    heard = x["heard"]["Messages"][0]
    assert heard["Content"] == facts["ask"] and \
        heard["Envelope"]["Sender"].lower() == facts["ann"], (
        f"ben read {heard!r}, not ann's question")
    got = x["got"]["Messages"][0]
    assert got["Content"] == facts["answer"] and \
        got["Envelope"]["ParentID"] == x["asked"], (
        f"ann read {got!r}, not ben's answer to {x['asked']}")


async def rows(endpoint: str, token: str, list_name: str) -> dict:
    async with await astral.connect(endpoint, token=token) as c:
        return {r["ID"]: r for r in await mail.list_messages(c, list=list_name)}


async def check_records(nodes, facts):
    x = facts["exchange"]
    sent = (await rows(nodes["node1"]["endpoint"], facts["ann_token"],
                       "outbox"))[x["asked"]]
    assert sent["LandedAt"] and sent["FetchedAt"], (
        f"ann's row on node1 reads {sent!r} — the message did not land on "
        "node2, or node2's receipt never reached node1")

    held = (await rows(nodes["node2"]["endpoint"], facts["ben_token"],
                       "inbox"))[x["asked"]]
    assert held["ReceiptDueAt"] and held["ReceiptStoredAt"], (
        f"ben's row on node2 reads {held!r} — node2 owes a receipt it never "
        "delivered")

    reply = (await rows(nodes["node1"]["endpoint"], facts["ann_token"],
                        "inbox"))[x["answered"]]
    assert reply["ParentID"] == x["asked"] and \
        reply["Sender"].lower() == facts["ben"], (
        f"ann's inbox on node1 holds {reply!r}, not ben's answer")


def main():
    doc = load()
    facts = doc["facts"]
    check_push(facts)
    check_authorities(facts)
    check_exchange(facts)
    asyncio.run(check_records(doc["nodes"], facts))
    print("oracle: ann on node1 and ben on node2 exchanged mail across the "
          "link, each node asked its own participant's side, and node2's "
          "receipt stamped ann's row on node1")


main()
