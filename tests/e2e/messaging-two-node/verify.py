#!/usr/bin/env python3
"""Oracle: mail crosses the link, and so do its receipt and its refusal.

Six judgements, and the order matters. node1 must have taken ben's and cal's
relay contracts, or neither send had a route. Each authority's record comes
next: the sender's side is asked on the sender's node and the recipient's side
on the recipient's, and an exchange proves nothing about reach unless both
were asked and admitted it, nor a refusal unless node2 was asked and refused.
Then the exchange, the refusal, and the query of a path node2 has no route
for, as the driver saw them.

Last, what each node holds. The oracle reads ann's outbox on node1 and ben's
inbox on node2 itself: the receipt and the refusal are claims about node1's
rows, and the driver is the wrong party to make them.
"""
import asyncio

import astral

from lib import mail
from lib.authority import RECEIVE, SEND, answers
from lib.sessionio import load

# What a delivery node2's authority refuses must read like on node1: the
# recipient's node rejects it with RejectNotAdmitted, and the code crosses the
# link and node1's relay path. A missing route here means the code was lost.
NOT_ADMITTED = "the recipient does not take messages from you"

# What a query node2 has no route for must read like on node1. node2 answers
# its missing route over the link with the generic reject code 1, which a
# refusal without a code of its own answers too, so node1's relay path reads
# code 1 as a missing route. A rejection here means node1 took node2's missing
# route for a refusal.
MISSING_ROUTE = "RouteNotFound"


def check_push(facts):
    for who in ("ben", "cal"):
        pushed = facts["pushed"][who]
        assert pushed[:1] == [{"Type": "bool", "Object": True}], (
            f"node1 answered {pushed!r} to {who}'s relay contract — node1 "
            f"never learned where {who} is hosted, so the send had no route")


def check_authorities(facts):
    a, b, c = facts["ann"], facts["ben"], facts["cal"]
    for node, kind, actor, other, who, want in (
            ("node1", SEND, a, b, "ann sending to ben", True),
            ("node2", RECEIVE, b, a, "ben receiving from ann", True),
            ("node2", SEND, b, a, "ben sending to ann", True),
            ("node1", RECEIVE, a, b, "ann receiving from ben", True),
            ("node1", SEND, a, c, "ann sending to cal", True),
            ("node2", RECEIVE, c, a, "cal receiving from ann", False)):
        q = facts["questions"][node]
        got = answers(q, kind, actor, other)
        assert got == [want], (
            f"{node}'s authority answered {got!r} for {who}, not one "
            f"{'yes' if want else 'no'} — the question was put on the wrong "
            f"node, named the wrong actor, or never put; {node} was asked "
            f"{q!r}")


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


def check_turned_away(facts):
    t = facts["turned_away"]
    assert t["refused"] and t["kind"] == "OpError" and \
        t["detail"] == f"delivery failed: {NOT_ADMITTED}", (
        f"ann's send to cal on node1 ended {t!r}, not delivery failed: "
        f"{NOT_ADMITTED} — node2's refusal reached ann as something else")


def check_unknown_path(facts):
    u = facts["unknown"]
    assert u["kind"] == MISSING_ROUTE, (
        f"ann's query of {facts['unknown_path']} on ben through node1 ended "
        f"{u!r}, not {MISSING_ROUTE} — node2 has no route for the path, and "
        f"node1's relay path answered something else")


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

    to_cal = [r for r in (await rows(nodes["node1"]["endpoint"],
                                     facts["ann_token"], "outbox")).values()
              if r["Recipient"].lower() == facts["cal"]]
    assert len(to_cal) == 1 and to_cal[0].get("FailedAt") and \
        not to_cal[0].get("LandedAt") and to_cal[0].get("Err") == NOT_ADMITTED, (
        f"ann's rows to cal on node1 read {to_cal!r}, not one failed row "
        f"holding {NOT_ADMITTED!r}")


def main():
    doc = load()
    facts = doc["facts"]
    check_push(facts)
    check_authorities(facts)
    check_exchange(facts)
    check_turned_away(facts)
    check_unknown_path(facts)
    asyncio.run(check_records(doc["nodes"], facts))
    print("oracle: ann on node1 and ben on node2 exchanged mail across the "
          "link, each node asked its own participant's side, node2's "
          "receipt stamped ann's row on node1, and node2's refusal of cal's "
          "side reached ann and her row with its own words, and a path "
          "node2 has no route for reached ann as a missing route")


main()
