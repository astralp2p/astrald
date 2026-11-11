#!/usr/bin/env python3
"""Oracle: an unclaimed node's manager answers the swarm invite. The approved
node joins the User's swarm and keeps its manager; the declined node stays
unclaimed.

The facts hold what each manager was asked and how each adoption ended; the
oracle reads each node's state itself.
"""
import asyncio

import astral

from lib.sessionio import load

DECLINED = "invitation declined"


async def info(n: dict):
    # why the node's token: the invitees run the shipped gate, which lets a
    # token-less session read user.info only, and the node's own token reads it
    # in every state.
    async with await astral.connect(n["endpoint"], token=n["token"]) as c:
        return await c.user.info()


async def roster(n: dict) -> set:
    async with await astral.connect(n["endpoint"], token=n["token"]) as c:
        return {str(m.identity) for m in await c.user.swarm_status() if m.identity}


def check_asked(name: str, c: dict, inviter: str, user: str, subject: str):
    # why first: a manager that cannot bind and attach a handler on the
    # unclaimed node answers nothing, and the node declines for lack of one.
    assert not c["serve_error"], (
        f"{name}: the manager could not serve on the unclaimed node: {c['serve_error']}")
    # why exactly one question: the decision is the node's policy, asked once
    # per invitation, of the manager the appointment named.
    assert len(c["asked"]) == 1, f"{name}: the manager was asked {len(c['asked'])} times: {c['asked']}"
    a = c["asked"][0]
    assert a == {"inviter": inviter, "issuer": user, "subject": subject}, (
        f"{name}: the manager was asked {a}; want the inviting node {inviter}, "
        f"the User {user}, and the invited node {subject}")
    assert c["state_before"] == "unclaimed" and c["manager_before"] == c["manager"], (
        f"{name}: before the invitation the node read {c['state_before']} naming "
        f"{c['manager_before']!r}; want unclaimed naming its manager {c['manager']}")


async def main():
    doc = load()
    f = doc["facts"]["manager_answers_invite"]
    nodes = doc["nodes"]
    inviter, ok, no = nodes["mai-inviter"], nodes["mai-approve"], nodes["mai-decline"]
    user, yes_case, no_case = f["user"], f["cases"]["approve"], f["cases"]["decline"]

    assert yes_case["manager"] != no_case["manager"], "both nodes appointed the same manager"

    check_asked("approve", yes_case, inviter["identity"], user, ok["identity"])
    assert yes_case["adopt_error"] == "" and yes_case["adopted_subject"] == ok["identity"], (
        f"approve: the adoption answered {yes_case['adopt_error']!r} for "
        f"{yes_case['adopted_subject']!r}; want a contract for {ok['identity']}")
    i = await info(ok)
    assert i.state == "claimed" and str(i.user_id) == user, (
        f"approve: the node reads {i.state} under {i.user_id}; want claimed under {user}")
    assert str(i.manager) == yes_case["manager"], (
        f"approve: the claimed node names manager {i.manager}; want {yes_case['manager']}")

    check_asked("decline", no_case, inviter["identity"], user, no["identity"])
    assert DECLINED in no_case["adopt_error"], (
        f"decline: the adoption answered {no_case['adopt_error']!r}; want {DECLINED!r}")
    assert not no_case["adopted_subject"], f"decline: a contract came back for {no_case['adopted_subject']}"
    i = await info(no)
    assert i.state == "unclaimed" and i.contract is None, (
        f"decline: the node reads {i.state} with contract {i.contract}; want unclaimed with none")
    assert str(i.manager) == no_case["manager"], (
        f"decline: the node names manager {i.manager}; want {no_case['manager']}")

    # why the inviter's own roster: the contract the approved node signed is in
    # the User's swarm, and the declined node is not a member.
    members = await roster(inviter)
    assert ok["identity"] in members, f"approve: the inviter's swarm {sorted(members)} lacks {ok['identity']}"
    assert no["identity"] not in members, f"decline: the inviter's swarm holds {no['identity']}"

    print("oracle: the approving manager brought its node into the swarm and stayed manager; "
          "the declining manager left its node unclaimed; each was asked once, by the inviting node")


asyncio.run(main())
