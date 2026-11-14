#!/usr/bin/env python3
"""Oracle: of Users racing to adopt one fresh node, exactly one wins.

For every round the driver recorded, the oracle judges the answers and then
reads the final state itself, collecting every failure before it exits:

1. Answers: exactly one `user.adopt` was answered with a signed contract.
   Every other one was refused as `node already claimed`, or as `session
   closed` when the prize closes the session before the adopter has written
   the contract (see adopt-refusals); a timeout, a transport failure or any
   other text is not a clean refusal.
2. One User on the prize: its `user.info`, read with its own token, is
   `claimed`, names the winner's User, and its `contract_id` is the contract
   the winner was answered. The contract the prize serves to the winner's node
   (`user.contract`) is that same object.
3. The swarms agree: within CONVERGE seconds the winner's
   `user.swarm_status` and the prize's are both exactly the winner's swarm:
   the winner's node and every prize the winner won. At no poll, and for
   WINDOW seconds after, does any loser's `user.swarm_status` or
   `user.list_siblings` list the prize.

why the final state and not only the answers: before the fix two adopters
could both be answered, and the prize kept whichever contract it set last, so
an adopter can hold a node that holds another User.
"""
import asyncio
import contextlib
import re
import time

import astral
from astral.objectid import object_id

from lib import nodefs
from lib.sessionio import load

REFUSALS = ("node already claimed", "session closed")
CONVERGE = 30.0
WINDOW = 5.0
POLL = 1.0
DEADLINE = 15.0
ANSI = re.compile(r"\x1b\[[0-9;]*m")
# the prize's log line for each contract it set active
ACTIVATED = "[user] hello, "


def judge_answers(prize: str, answers: dict) -> tuple[str | None, list]:
    """The winning adopter, and what is wrong with the answers."""
    won = [a for a, ans in answers.items() if ans["outcome"] == "answered"]
    bad = []
    if len(won) != 1:
        ids = {a: answers[a]["message"] for a in won}
        bad.append(f"{prize}: {len(won)} adoptions won, not exactly 1: {ids}")
    for a, ans in answers.items():
        if a in won:
            continue
        if ans["outcome"] != "refused" or ans["message"] not in REFUSALS:
            bad.append(f"{prize} <- {a}: want a refusal "
                       f"{' or '.join(REFUSALS)!r}, got {ans['outcome']} "
                       f"{ans['message']!r}")
    return (won[0] if len(won) == 1 else None), bad


async def members(c) -> set:
    return {str(m.identity) for m in await c.user.swarm_status()
            if m.identity is not None}


async def siblings(c) -> set:
    return {str(i) for i in await c.user.list_siblings()}


async def judge_prize(nodes, adopters, rnd, winner) -> list:
    """The prize names one User, the winner's, under the winner's contract."""
    prize = nodes[rnd["prize"]]
    want = rnd["answers"][winner]["message"] if winner else None
    bad = []
    async with await astral.connect(prize["endpoint"], token=prize["token"]) as c:
        info = await asyncio.wait_for(c.user.info(), DEADLINE)
    held = str(info.contract_id) if info.contract_id else None
    if info.state != "claimed":
        bad.append(f"{rnd['prize']}: state {info.state!r}, not 'claimed'")
    if winner is None:
        return bad + [f"{rnd['prize']}: holds contract {held} of User "
                      f"{info.user_id}"]
    if str(info.user_id) != adopters[winner]["user_id"]:
        bad.append(f"{rnd['prize']}: user.info names User {info.user_id}, "
                   f"not the winner {winner}'s {adopters[winner]['user_id']}")
    if held != want:
        bad.append(f"{rnd['prize']}: user.info contract_id {held}, not the "
                   f"contract {winner} was answered, {want}")
    w = nodes[winner]
    async with await astral.connect(w["endpoint"], token=w["token"]) as c:
        served = await asyncio.wait_for(
            c.user.contract(target=prize["identity"]), DEADLINE)
    if str(object_id(served)) != want:
        bad.append(f"{rnd['prize']}: serves contract {object_id(served)} to "
                   f"{winner}, not {want}")
    return bad


async def judge_swarms(nodes, adopters, rnd, winner, swarm: set) -> list:
    """The winner's swarm and the prize's converge on `swarm`; no loser ever
    lists the prize."""
    prize = nodes[rnd["prize"]]
    pid = prize["identity"]
    losers = [a for a in rnd["answers"] if a != winner]
    # why: with no single winner every adopter is judged as a loser
    role = "loser" if winner else "adopter"
    async with contextlib.AsyncExitStack() as stack:
        async def conn(node, token):
            return await stack.enter_async_context(
                await astral.connect(node["endpoint"], token=token))
        cs = {a: await conn(nodes[a], adopters[a]["user_token"])
              for a in rnd["answers"]}
        pc = await conn(prize, prize["token"])

        async def leaks() -> list:
            out = []
            for a in losers:
                if pid in await members(cs[a]) or pid in await siblings(cs[a]):
                    out.append(f"{rnd['prize']}: {role} {a}'s swarm lists it")
            return out

        def positive(wm: set, pm: set) -> list:
            if winner is None:
                return []
            out = []
            if wm != swarm:
                out.append(f"{rnd['prize']}: winner {winner}'s swarm_status is "
                           f"{sorted(wm)}, not exactly {sorted(swarm)}")
            if pm != swarm:
                out.append(f"{rnd['prize']}: its swarm_status is {sorted(pm)}, "
                           f"not exactly {sorted(swarm)}")
            return out

        deadline = time.monotonic() + CONVERGE
        while True:
            leaked = await leaks()
            if leaked:
                return leaked
            wm = await members(cs[winner]) if winner else set()
            missing = positive(wm, await members(pc))
            if not missing or time.monotonic() > deadline:
                break
            await asyncio.sleep(POLL)
        if missing:
            return [f"after {CONVERGE:.0f}s: {m}" for m in missing]
        end = time.monotonic() + WINDOW
        while time.monotonic() < end:
            await asyncio.sleep(POLL)
            leaked = await leaks()
            if leaked:
                return leaked
    return []


def evidence(node: dict) -> list:
    """The prize's log lines for each contract it set active; diagnostics only."""
    try:
        text = ANSI.sub("", nodefs.log(node))
    except (nodefs.NodeFSError, OSError) as e:
        return [f"(log unreadable: {e})"]
    return [ln.strip() for ln in text.splitlines() if ACTIVATED in ln]


async def main():
    doc = load()
    nodes, facts = doc["nodes"], doc["facts"]
    adopters, rounds = facts["adopters"], facts["rounds"]
    users = {a["user_id"] for a in adopters.values()}
    assert len(users) == len(adopters), "two adopters share a User"
    assert rounds, "the driver ran no round"

    judged = [(rnd, *judge_answers(rnd["prize"], rnd["answers"]))
              for rnd in rounds]
    # why: a User that won several races holds every prize it won, so its
    # swarm is its own node and those prizes
    swarms = {a: {nodes[a]["identity"]} for a in adopters}
    for rnd, winner, _ in judged:
        if winner:
            swarms[winner].add(nodes[rnd["prize"]]["identity"])

    failures = []
    for rnd, winner, bad in judged:
        bad += await judge_prize(nodes, adopters, rnd, winner)
        bad += await judge_swarms(nodes, adopters, rnd, winner,
                                  swarms.get(winner, set()))
        if bad:
            log = evidence(nodes[rnd["prize"]])
            bad.append(f"{rnd['prize']} log, each activation: {log}")
        failures += bad
        print(f"oracle: {rnd['prize']}: winner {winner}, "
              f"{'red' if bad else 'green'}")

    if failures:
        raise SystemExit("oracle: " + "; ".join(failures))
    print(f"oracle: {len(rounds)} races, each won by exactly one User; losers "
          "refused cleanly and list no prize")


asyncio.run(main())
