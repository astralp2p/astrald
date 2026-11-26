#!/usr/bin/env python3
"""Oracle: each admission policy refused with its documented text, admitted
nothing it refused, and the controls beside the refusals were answered.

Two kinds of judgement. The driver's record first: every refusal reads as the
policy's own refusal text, and every control was answered, since a refusal
beside a broken op proves nothing. Then what the nodes hold now, asked by the
oracle itself with each node's own token: who claimed whom, the swarm's roster,
the stored delegates. A refusal that still admitted would otherwise read as a
pass. Where a refusal is cheap and changes nothing when it holds, the
oracle asks it again itself.
"""
import asyncio

import astral
from astral.errors import AstralError, RemoteError
from astral.object import Nil

from lib import jsonops
from lib.sessionio import load

# The refusal texts node-policies.md's table names for each policy, and the
# setter's refusal of the node as its own delegate
# (mod/user/src/op_set_swarm_policy.go).
REGISTRATION = "registration declined"
REQUEST = "request declined"
INVITATION = "invitation declined"
CONTRACT = "contract declined"
SELF_DELEGATE = "the node cannot be its own delegate"

REFUSALS = {
    "register_config": REGISTRATION,
    "join_config": REQUEST,
    "join_delegate_wrong_object": REQUEST,
    "join_delegate_unreachable": REQUEST,
    "delegate_self": SELF_DELEGATE,
    "adopt_invitee": INVITATION,
    "register_delegate": REGISTRATION,
    "accept_contract_delegate": CONTRACT,
    "accept_contract_config": CONTRACT,
}
CONTROLS = ("register_applicant", "set_join_wrong_object", "set_join_unreachable",
            "clear_join_delegate", "adopt_applicant", "set_register_delegate",
            "clear_register_delegate", "set_contract_delegate",
            "clear_contract_delegate", "accept_contract_cleared")
DEADLINE = 15


async def refusal(coro) -> str:
    """The node's own text for a refusal; AssertionError if it was answered."""
    try:
        await coro
    except RemoteError as e:
        return e.message
    except (AstralError, jsonops.OpError) as e:
        return f"{type(e).__name__}: {e}"
    raise AssertionError("answered")


def connect(node: dict, token: str | None = "node"):
    return astral.connect(node["endpoint"],
                          token=node["token"] if token == "node" else token)


def check_record(f: dict) -> None:
    for case, want in REFUSALS.items():
        r = f["refusals"][case]
        assert r["refused"], f"{case}: astrald admitted it ({r['text']}); want {want!r}"
        assert r["error"] == "RemoteError" and r["text"] == want, (
            f"{case}: refused with {r['error']} {r['text']!r}; want the node's {want!r} — "
            "a refusal that is not the policy's says nothing about the policy")
    for case in CONTROLS:
        r = f["controls"][case]
        assert not r["refused"], (
            f"control {case} was refused: {r['error']} {r['text']!r} — the op does not "
            "work here, so the refusals beside it prove nothing")
    assert f["applicant_after_joins"] == "fresh", (
        f"applicant read {f['applicant_after_joins']} after three refused joins")
    assert f["acceptor_after_decline"] == "fresh", (
        f"acceptor read {f['acceptor_after_decline']} after its contract was declined")
    assert f["invitee_after_contract"] == "fresh", (
        f"invitee read {f['invitee_after_contract']} after its config declined a contract")


async def check_admitter(nodes: dict, f: dict) -> str:
    """The swarm admitter built, and what its policies refuse now."""
    admitter, applicant, invitee = nodes["admitter"], nodes["applicant"], nodes["invitee"]
    async with await connect(admitter) as c:
        info = await c.user.info()
        user = str(info.user_id)
        assert info.state == "claimed" and user == f["user"], (
            f"admitter reads {info.state} for {user}; want claimed for {f['user']}")
        roster = {str(m.identity) for m in await c.user.swarm_status()}
        want = {admitter["identity"], applicant["identity"]}
        assert roster == want, (
            f"admitter's swarm is {sorted(roster)}; want admitter and applicant alone "
            f"{sorted(want)} — a refused node is a member, or the adopted one is not")
        for path in ("/mod/user/policy/swarm_join", "/mod/apphost/policy/app_register"):
            stored = await c.tree.get(path)
            assert isinstance(stored, Nil), f"{path} holds {stored!r} after the clear"
        tokens = len(await c.apphost.list_tokens())

    # why again, by the oracle: the cleared delegate leaves the config's
    # reject_all deciding, and a declined registration must mint no token.
    async with await connect(admitter, None) as c:
        text = await refusal(c.apphost.register(timeout=DEADLINE))
    assert text == REGISTRATION, f"a token-less register on admitter: {text!r}"
    async with await connect(admitter) as c:
        after = len(await c.apphost.list_tokens())
    assert after == tokens, f"a declined registration left {after - tokens} new token(s)"

    async with await connect(applicant) as c:
        info = await c.user.info()
    assert info.state == "claimed" and str(info.user_id) == user, (
        f"applicant reads {info.state} for {info.user_id}; want claimed by {user}")

    async with await connect(invitee) as c:
        assert (await c.user.info()).state == "fresh", "invitee was claimed though it declined"
        text = await refusal(c.user.join(admitter["identity"], timeout=DEADLINE))
        assert text == REQUEST, f"invitee's join to admitter: {text!r}"
        assert (await c.user.info()).state == "fresh", "invitee was claimed by a refused join"
    return user


async def check_acceptor(nodes: dict, f: dict, admitter_user: str) -> None:
    """The contract acceptor accepted."""
    acceptor = nodes["acceptor"]
    async with await connect(acceptor) as c:
        info = await c.user.info()
        assert info.state == "claimed" and str(info.user_id) == f["acceptor_user"], (
            f"acceptor reads {info.state} for {info.user_id}; want claimed by "
            f"{f['acceptor_user']}")
        assert f["acceptor_user"] != admitter_user, "acceptor's User is admitter's"
        path = "/mod/user/policy/contract_accept"
        stored = await c.tree.get(path)
        assert isinstance(stored, Nil), f"{path} holds {stored!r} after the clear"


async def main():
    doc = load()
    nodes, f = doc["nodes"], doc["facts"]["admission"]
    check_record(f)
    user = await check_admitter(nodes, f)
    await check_acceptor(nodes, f, user)
    print(f"oracle: {len(REFUSALS)} refusals read as their policy's, {len(CONTROLS)} "
          "controls answered; admitter's swarm holds applicant alone, invitee stays "
          "fresh, and acceptor claimed once its delegate was cleared")


asyncio.run(main())
