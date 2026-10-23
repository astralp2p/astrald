#!/usr/bin/env python3
"""Driver: set up a node, and join one to it, from sessions without a token.

The driver acts and judges nothing. On claimer it first probes the
step-by-step setup ops, then previews the User with
user.derive_identity -generate, claims the node with user.bootstrap and the
same words, and probes what a token-less session reaches afterwards. On joiner
it joins claimer's swarm with user.join, then asks to join again without a
token and with joiner's own.

why jsonops: astral-py has no binding for user.derive_identity, user.bootstrap
or user.join, and the ops' JSON form is the node's own.
"""
import asyncio

import astral
from astral.errors import AstralError

from lib import jsonops
from lib.sessionio import load, write_facts

ADMIN = "mod.auth.admin_manage_apps_action"
# The step-by-step setup ops; they run with the node's token.
STEP_BY_STEP = ("bip137sig.new_entropy", "bip137sig.seed", "bip137sig.derive_key",
                "crypto.public_key", "objects.store", "user.new_node_contract",
                "auth.sign_contract", "user.accept_contract")
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


async def probe_unclaimed(c) -> dict:
    """What a token-less session reaches on the unclaimed node, besides setup."""
    probes = {op: await attempt(jsonops.value(c, f"{op}?out=json")) for op in STEP_BY_STEP}
    probes["user.info"] = await attempt(c.user.info())
    return probes


async def claim(c) -> dict:
    """Preview the User and claim the node with the same words."""
    preview = await jsonops.value(c, "user.derive_identity?generate=true&out=json")
    words = {"Type": "string16", "Object": preview["Mnemonic"]}
    claimed = await jsonops.value(c, "user.bootstrap?in=json&out=json", words)
    return {"preview": preview["Identity"], "words": len(preview["Mnemonic"].split()),
            "claimed": claimed["Identity"], "token": claimed["Token"],
            "mnemonic_in_claim": claimed["Mnemonic"]}


async def probe_claimed(c) -> dict:
    """What a token-less session reaches on the claimed node."""
    return {
        "user.info": await attempt(c.user.info()),
        "apphost.register": await attempt(c.apphost.register()),
        "user.derive_identity": await attempt(
            jsonops.value(c, "user.derive_identity?generate=true&out=json")),
        "shell.spec": await attempt(c.shell.spec()),
        "apphost.list_tokens": await attempt(c.apphost.list_tokens()),
    }


async def register_admin(c, endpoint: str) -> dict:
    """Register an app asking for admin rights; list tokens and ask whoami with
    no caller, as the app."""
    app = await jsonops.value(c, f"apphost.register?grant_permits={ADMIN}&out=json")
    async with await astral.connect(endpoint, token=app["Token"]) as a:
        tokens = await attempt(a.apphost.list_tokens())
        who = await a.apphost.whoami(caller=None)
    return {"app": app["Identity"], "list_tokens": tokens["detail"], "whoami": str(who)}


async def link(n1: dict, n2: dict) -> None:
    """Each node learns the other's endpoint, with its own token."""
    for a, b in ((n1, n2), (n2, n1)):
        async with await astral.connect(a["endpoint"], token=a["token"]) as c:
            await c.nodes.add_endpoint(b["identity"], b["lan_endpoint"], experimental=True)


async def join(joiner: dict, claimer: dict) -> dict:
    async with await astral.connect(joiner["endpoint"]) as c:      # anonymous
        before = await attempt(c.user.info())
        joined = await attempt(jsonops.value(
            c, f"user.join?node={claimer['identity']}&out=json", timeout=DEADLINE))
        after = await attempt(c.user.info())
        again_anon = await attempt(jsonops.value(
            c, f"user.join?node={claimer['identity']}&out=json", timeout=DEADLINE))
    # why the node token: the claimed list refuses user.join to a token-less
    # session, so only a session with a token reaches the op's own refusal.
    async with await astral.connect(joiner["endpoint"], token=joiner["token"]) as c:
        again = await attempt(jsonops.value(
            c, f"user.join?node={claimer['identity']}&out=json", timeout=DEADLINE))
    contract = joined.pop("answer", None) or {}
    inner = contract.get("Contract") or contract
    info = after.pop("answer", None)
    return {"before": before, "joined": joined, "again": again, "again_anon": again_anon,
            "issuer": inner.get("Issuer", ""), "subject": inner.get("Subject", ""),
            "user_after": str(info.contract.contract.issuer) if info else "",
            "after": after}


async def main():
    nodes = load()["nodes"]
    claimer, joiner = nodes["claimer"], nodes["joiner"]

    async with await astral.connect(claimer["endpoint"]) as c:     # anonymous
        unclaimed = await probe_unclaimed(c)
        claimed = await claim(c)
        probes = await probe_claimed(c)
        admin = await register_admin(c, claimer["endpoint"])
    async with await astral.connect(claimer["endpoint"], token=claimed["token"]) as u:
        claimed["token_whoami"] = str(await u.apphost.whoami())

    await link(claimer, joiner)
    joined = await join(joiner, claimer)

    for r in list(probes.values()) + list(unclaimed.values()):
        r.pop("answer", None)
    write_facts({"token_less_setup": {"unclaimed": unclaimed, "claim": claimed, "probes": probes,
                                      "admin": admin, "join": joined}})
    print(f"driver: claimed {claimed['claimed']}; joiner joined: "
          f"{joined['joined']['detail']}; second join: {joined['again']['detail']}")


asyncio.run(main())
