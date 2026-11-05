#!/usr/bin/env python3
"""Driver: each of the five admission policies refuses, and its control admits.

The driver acts and judges nothing. On `admitter`, whose config rejects every
swarm join and every app registration, it claims the node without a token,
then asks it for a registration and a membership by config and through
delegates that cannot decide; it adopts `invitee`, whose invite policy rejects,
and `applicant`, whose policy accepts. On `acceptor` it claims step by step
under a contract accept delegate that cannot decide, then without one, and
asks for a grant under the reject_all and accept_all grant request modes.
Every answer goes into the `admission` fact; the oracle judges.

why the controls beside every refusal: a refusal and a broken op leave the
caller holding the same nothing. A register answered on `applicant`, an
adoption answered for `applicant`, a contract accepted once the delegate is
cleared and a grant answered under accept_all show the op works in this world,
so the refusal is the policy's.

why delegates that cannot decide: a delegate that answers the wrong object,
and one that is never reached, are the two faults a delegated policy meets
without any app served on the host. Each must read as a refusal, never as an
admission.
"""
import asyncio
import time

import astral
from astral.api.crypto import public_key_to_identity
from astral.errors import AstralError, RemoteError

from lib import jsonops
from lib.sessionio import load, write_facts

# why the secp256k1 generator: a valid public key no node in any world holds,
# so a delegate named by it resolves and is never reached.
STRANGER = "0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"
# why two actions: the node records a permit it already holds as the same
# grant, so a declined request for the action the control asks would leave
# nothing to tell apart. The declined requests ask DENIED, the control SEE.
SEE = "mod.auth.see_objects_action"
DENIED = "mod.user.see_swarm_action"
# why an answer that is not a decision: every node answers user.info, so the
# delegate is reached and replies, and the reply is an Info, not the policy's
# decision type.
WRONG_OP = "user.info"
# why bounds this small: every refusal here was answered in under 0.03 s live,
# and a delegate is asked at most DELEGATE_TIMEOUT; a client's default is 60 s,
# and request_grant's 610 s.
DELEGATE_TIMEOUT = "3s"
DEADLINE = 15
GRANT_MODE = "apphost.set_grant_request_policy?mode={}&out=json"


async def attempt(coro) -> dict:
    """Run one step; report whether it was refused, as what, and how long it
    took. A RemoteError keeps the node's own text, unprefixed."""
    t0 = time.monotonic()
    try:
        await coro
    except (AstralError, jsonops.OpError) as e:
        text = e.message if isinstance(e, RemoteError) else str(e)
        return {"refused": True, "error": type(e).__name__, "text": text,
                "seconds": round(time.monotonic() - t0, 3)}
    return {"refused": False, "error": "", "text": "answered",
            "seconds": round(time.monotonic() - t0, 3)}


def connect(node: dict, token: str | None = "node"):
    return astral.connect(node["endpoint"],
                          token=node["token"] if token == "node" else token)


async def link(a: dict, b: dict) -> None:
    """Each node learns the other's endpoint, with its own token."""
    for x, y in ((a, b), (b, a)):
        async with await connect(x) as c:
            await c.nodes.add_endpoint(y["identity"], y["lan_endpoint"],
                                       experimental=True)


async def state(node: dict) -> str:
    async with await connect(node) as c:
        return (await c.user.info()).state


async def join(applicant: dict, admitter: dict) -> dict:
    async with await connect(applicant) as c:
        return await attempt(c.user.join(admitter["identity"], timeout=DEADLINE))


async def register_tokenless(node: dict) -> dict:
    async with await connect(node, None) as c:
        return await attempt(c.apphost.register(timeout=DEADLINE))


async def admitter_flow(admitter, applicant, invitee, refusals, controls, facts):
    """The app register, swarm join and swarm invite policies."""
    async with await connect(admitter, None) as c:
        claimed = await c.user.bootstrap(None, timeout=DEADLINE)
    facts["user"] = str(claimed.identity)
    facts["user_token"] = claimed.token

    refusals["register_config"] = await register_tokenless(admitter)
    controls["register_applicant"] = await register_tokenless(applicant)

    refusals["join_config"] = await join(applicant, admitter)

    async with await connect(admitter, claimed.token) as u:
        controls["set_join_wrong_object"] = await attempt(u.user.set_swarm_join_policy(
            invitee["identity"], WRONG_OP, timeout=DELEGATE_TIMEOUT))
    refusals["join_delegate_wrong_object"] = await join(applicant, admitter)

    async with await connect(admitter, claimed.token) as u:
        controls["set_join_unreachable"] = await attempt(u.user.set_swarm_join_policy(
            STRANGER, "x.decide", timeout=DELEGATE_TIMEOUT))
    refusals["join_delegate_unreachable"] = await join(applicant, admitter)
    facts["applicant_after_joins"] = await state(applicant)

    async with await connect(admitter, claimed.token) as u:
        refusals["delegate_self"] = await attempt(u.user.set_swarm_join_policy(
            admitter["identity"], WRONG_OP, timeout=DELEGATE_TIMEOUT))
        controls["clear_join_delegate"] = await attempt(u.user.set_swarm_join_policy())

        refusals["adopt_invitee"] = await attempt(
            u.user.adopt(invitee["identity"], timeout=DEADLINE))
        # why adopt succeeds where join was refused: adoption asks the
        # invited node's invite policy, never this node's join policy.
        controls["adopt_applicant"] = await attempt(
            u.user.adopt(applicant["identity"], timeout=DEADLINE))

        controls["set_register_delegate"] = await attempt(u.apphost.set_app_register_policy(
            invitee["identity"], WRONG_OP, timeout=DELEGATE_TIMEOUT))
    refusals["register_delegate"] = await register_tokenless(admitter)
    async with await connect(admitter, claimed.token) as u:
        controls["clear_register_delegate"] = await attempt(
            u.apphost.set_app_register_policy())
    facts["invitee_after_adopt"] = await state(invitee)


async def claim_contract(c):
    """A User generated on the node, and the claim contract both parties signed."""
    entropy = await c.bip137sig.new_entropy(bits=256)
    seed = await c.bip137sig.seed(await c.bip137sig.mnemonic(entropy))
    key = await c.bip137sig.derive_key(seed, path="m/1791'/0'/0'/0/0")
    await c.objects.store(key)
    uid = str(public_key_to_identity(await c.crypto.public_key(key)))
    signed = await c.auth.sign_contract(await c.user.new_node_contract(user=uid))
    return uid, signed


async def acceptor_flow(acceptor, invitee, refusals, controls, facts):
    """The contract accept and grant request policies."""
    async with await connect(acceptor) as c:
        controls["set_contract_delegate"] = await attempt(c.user.set_contract_accept_policy(
            invitee["identity"], WRONG_OP, timeout=DELEGATE_TIMEOUT))
        uid, signed = await claim_contract(c)
        refusals["accept_contract_delegate"] = await attempt(
            c.user.accept_contract(signed, timeout=DEADLINE))
        facts["acceptor_after_decline"] = (await c.user.info()).state
        controls["clear_contract_delegate"] = await attempt(c.user.set_contract_accept_policy())
        controls["accept_contract_cleared"] = await attempt(
            c.user.accept_contract(signed, timeout=DEADLINE))
        user_token = (await c.apphost.create_token(uid)).token
    facts["acceptor_user"] = uid

    async with await connect(acceptor, user_token) as u:
        app = await u.apphost.register(name="admission-policies", timeout=DEADLINE)
        controls["mode_reject_all"] = await attempt(
            jsonops.value(u, GRANT_MODE.format("reject_all"), timeout=DEADLINE))
    facts["app"], facts["app_token"] = str(app.identity), app.token

    async with await connect(acceptor, app.token) as a:
        refusals["grant_reject_all"] = await attempt(
            a.apphost.request_grant([DENIED], timeout=DEADLINE))
    # why actions and not a count: the registration itself grants the app
    # serve_apps and use_blueprints, so only the requested action says whether
    # the declined request wrote anything.
    async with await connect(acceptor, user_token) as u:
        facts["grants_after_reject"] = [g.permit.action for g in
                                        await u.apphost.list_grants(app.identity)]
        controls["mode_accept_all"] = await attempt(
            jsonops.value(u, GRANT_MODE.format("accept_all"), timeout=DEADLINE))
    async with await connect(acceptor, app.token) as a:
        controls["grant_accept_all"] = await attempt(
            a.apphost.request_grant([SEE], timeout=DEADLINE))
    # why reject_all again: the oracle asks DENIED again under it.
    async with await connect(acceptor, user_token) as u:
        controls["mode_reject_all_again"] = await attempt(
            jsonops.value(u, GRANT_MODE.format("reject_all"), timeout=DEADLINE))


async def main():
    nodes = load()["nodes"]
    admitter, applicant = nodes["admitter"], nodes["applicant"]
    invitee, acceptor = nodes["invitee"], nodes["acceptor"]

    # why these pairs: admitter adopts applicant and invitee and asks invitee
    # as a delegate; acceptor asks invitee as a delegate.
    for a, b in ((admitter, applicant), (admitter, invitee), (acceptor, invitee)):
        await link(a, b)

    refusals, controls, facts = {}, {}, {}
    await admitter_flow(admitter, applicant, invitee, refusals, controls, facts)
    await acceptor_flow(acceptor, invitee, refusals, controls, facts)

    facts["refusals"], facts["controls"] = refusals, controls
    write_facts({"admission": facts})
    for name, r in {**refusals, **controls}.items():
        print(f"driver: {name}: {r['text']} ({r['seconds']} s)")


asyncio.run(main())
