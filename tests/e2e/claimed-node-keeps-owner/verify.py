#!/usr/bin/env python3
"""Oracle: a claimed node keeps its owner when asked to take another.

After the driver, or an operator, asked node1 to become another User's node,
node1 must still report the User bootstrap-user-software-key made, as the
issuer of an active contract for node1, and answer that User's token as that
User. The oracle then asks once more itself, as node1, to claim the node for
the words of another User, and the node must refuse with `node already
claimed` and still report the first User.

why the oracle asks: whether a node can still be claimed is observable only by
claiming it. The question is the same one the driver and the operator put, so
it judges the node's refusal even where the driver is an operator that
reported nothing; and it is asked after the state is judged, so a node a
driver already lost is named for that first.
"""
import asyncio

import astral
from astral.errors import AstralError, RemoteError

from lib.sessionio import load

ALREADY_CLAIMED = "node already claimed"
# the BIP-39 test vector with entropy zero: valid, and not the words of any
# User a test makes; the operator's prompt gives the same words
RIVAL_WORDS = " ".join(["abandon"] * 11 + ["about"])
RESET_ATTEMPTS = 5


async def check_owner(n1: dict, user: str, user_token: str, when: str) -> None:
    # why node1's own token first: it reads the owner whoever the owner is,
    # where a token of the first User stops answering user.info once another
    # User owns the node.
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        info = await c.user.info()
        assert info.state == "claimed", f"{when}: state {info.state}"
        assert str(info.user_id) == user, (
            f"{when}: contract issuer {info.user_id}: the node changed owner "
            f"from {user}")
        assert str(info.node_id) == n1["identity"], (
            f"{when}: contract subject {info.node_id} != node {n1['identity']}")
        assert info.contract.fully_signed, f"{when}: the active contract is not fully signed"
    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        who = await c.apphost.whoami()
        assert str(who) == user, f"{when}: whoami {who} != the first User {user}"
        try:
            info = await c.user.info()
        except AstralError as e:
            raise AssertionError(f"{when}: the first User's token no longer works: {e}")
        assert str(info.user_id) == user, f"{when}: the User sees issuer {info.user_id}"


async def claim_attempt(n1: dict) -> str:
    """The node's answer to user.bootstrap with another User's words, as node1.

    why a call made again when reset: see the driver; a reset in place of the
    answer says nothing, and a refused call changes nothing.
    """
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        for _ in range(RESET_ATTEMPTS):
            try:
                await c.user.bootstrap(RIVAL_WORDS)
            except RemoteError as e:
                return e.message
            except ConnectionError:
                continue
            return "answered"
    return "reset"


async def main():
    doc = load()
    n1, facts = doc["nodes"]["node1"], doc["facts"]
    user, user_token = facts["user_id"], facts["user_token"]
    await check_owner(n1, user, user_token, "after the attempts")
    answer = await claim_attempt(n1)
    assert answer == ALREADY_CLAIMED, (
        f"user.bootstrap with another User's words on a claimed node: {answer!r}, "
        f"want {ALREADY_CLAIMED!r}")
    await check_owner(n1, user, user_token, "after the oracle's own attempt")
    print(f"oracle: node1 kept User {user[:16]}…, its contract and its token; "
          "a claim for other words was refused as node already claimed")


asyncio.run(main())
