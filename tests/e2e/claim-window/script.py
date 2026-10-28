#!/usr/bin/env python3
"""Driver: on an unclaimed node, auth.sign_contract admits the claim contract only.

A session with node1's token arrives as node1. The signer rule refuses it both
parties of the user→node contract, and the claim exception admits that one
contract while node1 is unclaimed. The driver asks for the claim contract and
for five near misses, and claims nothing: every issuer is an identity whose key
node1 does not hold, node1 itself, or an app node1 minted.

why an issuer whose key node1 does not hold: the guard and the signing that
follows are separate steps. Admitted, such a contract fails at signing
("sign as issuer: no signing scheme available for key …"); refused, it fails at
the guard naming the party. The control reads the first, the probes the second,
and node1 stays unclaimed for the tests after this one.

why the driver checks the claim state first: the window exists only on an
unclaimed node, and the runner's states name no such state. A selection that
pulls in a two-node prereq runs this test after the claim, where every probe
would read as a broken guard. The driver fails instead, so the run reports the
order and not astrald.

why node1's token: the step-by-step setup ops run with the node's token
(.ai/system/topics/node-setup.md).
"""
import asyncio
import sys

import astral
from astral.api.auth import Contract, Permit
from astral.errors import AstralError

from lib.sessionio import load, write_facts

SUDO = "mod.auth.sudo_action"
# why the generator point: a valid identity whose private key (1) no node stores.
STRANGER = "0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"


async def attempt(coro) -> dict:
    """Run one probe; report whether it was refused, and how it read."""
    try:
        await coro
    except AstralError as e:
        return {"refused": True, "detail": f"{type(e).__name__}: {e}"}
    return {"refused": False, "detail": "answered"}


def with_sudo(c: Contract) -> Contract:
    return Contract(issuer=c.issuer, subject=c.subject,
                    permits=list(c.permits) + [Permit(action=SUDO, constraints=None, delegation=0)],
                    expires_at=c.expires_at)


async def main():
    n1 = load()["nodes"]["node1"]
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as node:
        # note: user.info answers "no active contract" on a node with no active
        # contract.
        before = await attempt(node.user.info())
        if not (before["refused"] and "no active contract" in before["detail"]):
            sys.exit(f"driver: user.info answered {before['detail']!r}, so node1 "
                     "is not unclaimed and the claim window is closed — run "
                     "claim-window alone or in main.suite order, before "
                     "bootstrap-user-software-key")

        app = await node.apphost.register()
        claim = await node.user.new_node_contract(user=STRANGER)
        probes = {
            "claim, stranger issuer": await attempt(node.auth.sign_contract(claim)),
            "claim issued by the node": await attempt(node.auth.sign_contract(
                await node.user.new_node_contract(user=n1["identity"]))),
            "claim issued by an app": await attempt(node.auth.sign_contract(
                await node.user.new_node_contract(user=str(app.identity)))),
            "claim carrying a sudo permit": await attempt(
                node.auth.sign_contract(with_sudo(claim))),
            "claim valid for a century": await attempt(node.auth.sign_contract(
                await node.user.new_node_contract(user=STRANGER, duration="876000h"))),
        }

    async with await astral.connect(n1["endpoint"], token=str(app.token)) as a:
        probes["claim asked by an app as itself"] = await attempt(a.auth.sign_contract(claim))

    async with await astral.connect(n1["endpoint"], token=n1["token"]) as node:
        after = await attempt(node.user.info())

    write_facts({"claim_window": {"probes": probes, "unclaimed_after": after}})
    print("driver: " + ", ".join(f"{k}: {v['detail'][:60]}" for k, v in probes.items()))


asyncio.run(main())
