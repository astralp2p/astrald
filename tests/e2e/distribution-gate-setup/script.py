#!/usr/bin/env python3
"""Driver: set up a node that runs the shipped default gate, the way
node-setup.md describes, and probe the gate at each state.

The driver acts and judges nothing. node1 starts under the apphost gate and
policies a released astrald ships (test.toml clears the harness's). From a
session without a token the driver reads user.info, appoints the manager with
sys.register_manager, and reads user.info again. As the manager it previews the
User and claims the node with the same pinned words. At each state it records
what the gate answered the callers it must keep out: a session without a token,
one with the node's token, and the manager.

why jsonops: astral-py has no binding for sys.register_manager, user.bootstrap,
user.derive_identity or user.join, and the ops' JSON form is the node's own.
"""
import asyncio
import sys

import astral
from astral.errors import AstralError

from lib import jsonops
from lib.sessionio import load, write_facts

# why these words: the BIP-39 vector of 24 words, so they are valid and the User
# they derive is pinned in the oracle.
WORDS = " ".join(["abandon"] * 23 + ["art"])
REGISTER_MANAGER = {"Type": "mod.sys.register_manager_request",
                    "Object": {"Name": "gate-setup", "GrantPermits": [],
                               "ContractPermits": [], "Policies": []}}
# why a deadline: a query that is never routed hangs rather than answering, and
# the client's default is 60 s.
DEADLINE = 30


async def attempt(coro) -> dict:
    """Run one probe; report whether it was refused, and how it read."""
    try:
        answer = await coro
    except (AstralError, jsonops.OpError) as e:
        return {"refused": True, "detail": f"{type(e).__name__}: {e}"}
    return {"refused": False, "detail": "answered", "answer": answer}


def op(c, qs: str, body: dict | None = None):
    return jsonops.value(c, qs, body, timeout=DEADLINE)


async def state(c) -> str:
    return (await op(c, "user.info?out=json"))["State"]


def words(text: str = WORDS) -> dict:
    return {"Type": "string16", "Object": text}


# The ops the gate keeps from each caller in each state: "<state>/<who>". Each
# is denied before the op reads anything; an op the caller may call is left out,
# since calling it would change the node (the node and the manager may claim an
# unclaimed one).
SETUP = ("user.bootstrap", "user.derive_identity", "user.join")
KEPT = {
    "fresh/tokenless": SETUP + ("apphost.register", "apphost.list_tokens"),
    "fresh/node": SETUP + ("apphost.register", "apphost.list_tokens"),
    "unclaimed/tokenless": SETUP + ("sys.register_manager", "apphost.list_tokens"),
    "unclaimed/node": ("sys.register_manager", "apphost.list_tokens"),
    "unclaimed/manager": ("sys.register_manager", "apphost.list_tokens"),
}


async def probes(c, node: dict, case: str, out: dict) -> None:
    """Ask `c` for the ops the gate keeps from `case`; record how each was
    answered under "<case>/<op>"."""
    asks = {
        "user.bootstrap": ("user.bootstrap?in=json&out=json", words()),
        "user.derive_identity": ("user.derive_identity?in=json&out=json", words()),
        "user.join": (f"user.join?node={node['identity']}&out=json", None),
        "sys.register_manager": ("sys.register_manager?in=json&out=json",
                                 REGISTER_MANAGER),
        "apphost.register": ("apphost.register?in=json&out=json",
                             {"Type": "mod.apphost.register_request", "Object": {}}),
        "apphost.list_tokens": ("apphost.list_tokens?out=json", None),
    }
    for name in KEPT[case]:
        qs, body = asks[name]
        out[f"{case}/{name}"] = (r := await attempt(op(c, qs, body)))
        r.pop("answer", None)


async def main():
    n = load()["nodes"]["node1"]
    states, found = {}, {}

    async def anonymous():
        return await astral.connect(n["endpoint"])

    async def node():
        return await astral.connect(n["endpoint"], token=n["token"])

    async with await anonymous() as c:
        states["before"] = await state(c)
    async with await anonymous() as c:
        await probes(c, n, "fresh/tokenless", found)
    async with await node() as c:
        await probes(c, n, "fresh/node", found)

    async with await anonymous() as c:
        manager = await op(c, "sys.register_manager?in=json&out=json", REGISTER_MANAGER)
        states["appointed"] = await state(c)
    async with await anonymous() as c:
        await probes(c, n, "unclaimed/tokenless", found)
    async with await node() as c:
        await probes(c, n, "unclaimed/node", found)
    async with await astral.connect(n["endpoint"], token=manager["Token"]) as m:
        await probes(m, n, "unclaimed/manager", found)
        try:
            preview = await op(m, "user.derive_identity?in=json&out=json", words())
            claim = await op(m, "user.bootstrap?in=json&out=json", words())
        except jsonops.OpError as e:
            # why: a probe the gate admitted may have claimed the node first
            admitted = [k for k, v in found.items() if not v["refused"]]
            sys.exit(f"driver: the manager's claim answered {e}; probes the gate "
                     f"admitted: {admitted or 'none'}")
    if preview["Identity"] != claim["Identity"]:
        sys.exit(f"driver: the preview named {preview['Identity']}, the claim "
                 f"{claim['Identity']}")

    async with await astral.connect(n["endpoint"], token=claim["Token"]) as u:
        states["claimed"] = await state(u)

    write_facts({"gate_manager_id": manager["Identity"],
                 "gate_user_id": claim["Identity"],
                 "gate_user_token": claim["Token"],
                 "gate_states": states, "gate_probes": found})
    for k, v in found.items():
        print(f"driver: {k}: {v['detail']}")
    print(f"driver: manager {manager['Identity']}; {states}")


asyncio.run(main())
