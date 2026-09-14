#!/usr/bin/env python3
"""Driver: a swarm sibling asks node1 to mint a token, as the User.

The token ops answer to `mod.auth.admin_manage_apps_action`, which the User
identity holds. mod/user also authorizes `RelayForAction` for a swarm node
relaying on behalf of the User, so node2 can send a relay query naming the User
as its caller. node1 admits the relay and the op sees the User — an identity
that satisfies the authorization check. The origin refusal is what is under
test.

The driver acts and judges nothing. It mints a User token on node2, asks node1
for a token and for its token list through that session, and records what came
back — plus what the same two ops do for the User locally on node1, because
"refused" and "broken" leave a caller holding the same nothing.

why the sibling mints its own token: `user_token` in the session facts is
node1's apphost token and authenticates nowhere else. node2's own token
authenticates as node2, which holds AdminManageApps on node2 as this node, and
may mint a User token there.
"""
import asyncio

import astral
from astral.errors import AstralError

from lib.sessionio import load, write_facts

# why a deadline: a query that is never routed hangs rather than answering, and
# the client's default is 60 s. app-query settled on 15 for the same reason.
DEADLINE = 15


async def attempt(coro) -> dict:
    """Run one probe; report whether it was refused, and how it read."""
    try:
        await coro
    except AstralError as e:
        return {"refused": True, "detail": f"{type(e).__name__}: {e}"}
    return {"refused": False, "detail": "answered"}


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    facts = doc["facts"]
    user_id = facts["user_id"]

    # The control: the User reaches both ops locally on node1. Without it the
    # refusals below would be satisfied by a node whose apphost never answered.
    async with await astral.connect(n1["endpoint"],
                                    token=facts["user_token"]) as c1:
        minted = await c1.apphost.create_token(user_id)
        control_tokens = await c1.apphost.list_tokens()

    # A User session on the sibling, minted by node2 as itself.
    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c2:
        sibling_user = await c2.apphost.create_token(user_id)

    # The probes: the same two ops on node1, reached over the link as the User.
    # why node2's identity as the mint target: it is distinctive, already known
    # here, and lets the oracle ask node1 whether anything was minted.
    async with await astral.connect(n2["endpoint"],
                                    token=sibling_user.token) as c2:
        refusals = {
            "apphost.create_token": await attempt(
                c2.apphost.create_token(n2["identity"],
                                        target=n1["identity"],
                                        timeout=DEADLINE)),
            "apphost.list_tokens": await attempt(
                c2.apphost.list_tokens(target=n1["identity"],
                                       timeout=DEADLINE)),
        }

    write_facts({
        "control_minted": bool(str(minted.token)),
        "control_list_count": len(control_tokens),
        "refusals": refusals,
        "probe_identity": n2["identity"],
    })

    shape = ", ".join(f"{op} {'refused' if r['refused'] else 'ANSWERED'}"
                      for op, r in refusals.items())
    print(f"driver: node1 answered the User locally ({len(control_tokens)} "
          f"tokens); over the link as the User — {shape}")


asyncio.run(main())
