#!/usr/bin/env python3
"""Driver: a swarm sibling asks node1 to register, host a handler and discover
the swarm, as the User.

apphost.register and apphost.register_handler refuse a query off a link, and
services.discover refuses reach=swarm arriving over one. The User identity
passes every authorization check these ops make, and mod/user authorizes a swarm
node to relay as the User, so node2 can send node1 a query naming the User as
its caller. The origin refusal is what is under test.

The driver acts and judges nothing. It mints a User token on node2, sends the
three ops to node1 through that session, and records what came back, plus what
the same three ops do for the User locally on node1 in the same run, because
"refused" and "broken" leave a caller holding the same nothing. It also counts
the tokens node1 holds before and after the probes: a registration that was
refused must leave no identity behind.

why the sibling mints its own token: `user_token` in the session facts is
node1's apphost token and authenticates nowhere else. node2's own token
authenticates as node2, which holds AdminManageApps on node2 as this node, and
may mint a User token there (apphost-origin does the same).
"""
import asyncio

import astral
from astral.api.services import REACH_SWARM
from astral.errors import AstralError

from lib.sessionio import load, write_facts

# why a deadline: a query that is never routed hangs rather than answering, and
# the client's default is 60 s. apphost-origin settled on 15 for the same reason.
DEADLINE = 15

# why a name no node advertises: the discovery only has to be admitted. Its
# answer is empty, so the control does not depend on any provider.
SERVICE = "oag-nothing-advertises-this"

# why a dead endpoint: the handler is registered to be refused or acknowledged,
# never called. A query for the User would dial it, and none is sent.
ENDPOINT = "tcp:127.0.0.1:9"
HANDLER_TOKEN = 0x0A6F1ED5


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
    target = n1["identity"]

    # The control: the User reaches the three ops locally on node1.
    async with await astral.connect(n1["endpoint"],
                                    token=facts["user_token"]) as c1:
        registered = await c1.apphost.register(timeout=DEADLINE)

        # why bind first: closing the bind stream removes the handler, so the
        # control leaves no registration behind.
        bind = await c1.apphost.bind(HANDLER_TOKEN)
        try:
            await c1.apphost.register_handler(ENDPOINT, HANDLER_TOKEN,
                                              timeout=DEADLINE)
            handler_acked = True
        finally:
            await bind.aclose()

        events = await c1.services.discover([SERVICE], reach=REACH_SWARM,
                                            timeout=DEADLINE)
        initial = [e for e in events if hasattr(e, "complete")]
        control_discover = {
            "answered": True,
            "offerings": len([e for e in events if e not in initial]),
            "ended": bool(initial),
        }

        tokens_before = len(await c1.apphost.list_tokens())

    # A User session on the sibling, minted by node2 as itself.
    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c2:
        sibling_user = await c2.apphost.create_token(user_id)

    # The probes: the same three ops on node1, reached over the link as the User.
    async with await astral.connect(n2["endpoint"],
                                    token=sibling_user.token) as c2:
        refusals = {
            "apphost.register": await attempt(
                c2.apphost.register(target=target, timeout=DEADLINE)),
            "apphost.register_handler": await attempt(
                c2.apphost.register_handler(ENDPOINT, HANDLER_TOKEN + 1,
                                            target=target, timeout=DEADLINE)),
            "services.discover": await attempt(
                c2.services.discover([SERVICE], reach=REACH_SWARM,
                                     target=target, timeout=DEADLINE)),
        }

    async with await astral.connect(n1["endpoint"],
                                    token=facts["user_token"]) as c1:
        tokens_after = len(await c1.apphost.list_tokens())

    write_facts({
        "control_registered": str(registered.identity),
        "control_handler_acked": handler_acked,
        "control_discover": control_discover,
        "refusals": refusals,
        "tokens_before": tokens_before,
        "tokens_after": tokens_after,
    })

    shape = ", ".join(f"{op} {'refused' if r['refused'] else 'ANSWERED'}"
                      for op, r in refusals.items())
    print(f"driver: node1 answered the User locally; over the link as the "
          f"User — {shape}")


asyncio.run(main())
