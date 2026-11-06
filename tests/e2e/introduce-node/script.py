#!/usr/bin/env python3
"""Driver: bring a node claimed with the User's words into the swarm with user.introduce.

The driver acts and judges nothing. It continues introduce-node-setup's
world, where `first`, the node that generated the words, is stopped. It
appoints a manager on the fresh `replacement`, claims it with the same words
as the manager, and records what `member`, `sibling` and `replacement` list
before the introduction. As the User, `replacement` then introduces itself
to `member`. The driver also asks for the op without a token, from
`sibling`'s node token, naming `replacement` itself, and once more with
`replacement`'s node token. Last, it asks user.info as the User across the
link both ways: from `replacement` of `member`, and from `member` of
`replacement`.

why jsonops: astral-py has no binding for apphost.register_manager,
user.bootstrap or user.introduce, and the ops' JSON form is the node's own.
"""
import asyncio
import time

import astral
from astral.errors import AstralError

from lib import jsonops
from lib.sessionio import load, write_facts

MANAGER = {"Type": "mod.apphost.register_manager_request", "Object": {"Name": "manager"}}
# why a deadline: a query that is never routed hangs rather than answering, and
# the client's default is 60 s.
DEADLINE = 30
# why a window: the sync that makes replacement and member siblings runs after
# the introduction answers, and RelayFor refuses the User's query until then.
CONVERGE = 60.0
POLL = 1.0


def connect(node: dict, token: str | None = "node"):
    """A session on node: its node token by default, another token, or none."""
    return astral.connect(node["endpoint"], token=node["token"] if token == "node" else token)


async def members(node: dict) -> set:
    async with await connect(node) as c:
        return {str(m.identity) for m in await c.user.swarm_status() if m.identity is not None}


async def introduce(c, node: str) -> dict:
    """What user.introduce naming node answered, or how the query was refused."""
    try:
        docs = await jsonops.call(c, f"user.introduce?identity={node}&out=json", timeout=DEADLINE)
    except AstralError as e:
        return {"refused": True, "detail": f"{type(e).__name__}: {e}"}
    if docs and docs[0]["Type"] == "error_message":
        return {"refused": True, "detail": docs[0]["Object"]}
    return {"refused": False, "types": [d["Type"] for d in docs]}


async def claim_replacement(replacement: dict, member: dict, words: str) -> dict:
    """Add member's endpoint on replacement, appoint a manager, and claim it with words."""
    # why the endpoint first: the E2E gate opens every op on a fresh node, and
    # its unclaimed list omits nodes.add_endpoint.
    async with await connect(replacement) as c:
        await c.nodes.add_endpoint(member["identity"], member["lan_endpoint"], experimental=True)
        manager = await jsonops.value(c, "apphost.register_manager?in=json&out=json", MANAGER)
    async with await connect(replacement, manager["Token"]) as m:
        return await jsonops.value(m, "user.bootstrap?in=json&out=json",
                                   {"Type": "string16", "Object": words})


async def info_as_user(c, target: str) -> dict:
    """user.info of target as the session's User, retried while the sync
    that makes the two nodes siblings runs; the last answer or refusal."""
    deadline = time.monotonic() + CONVERGE
    while True:
        try:
            i = await c.user.info(target=target, timeout=DEADLINE)
            sc = i.contract.contract if i.contract is not None else None
            return {"answered": True, "state": i.state,
                    "subject": str(sc.subject) if sc is not None else "",
                    "issuer": str(sc.issuer) if sc is not None else ""}
        except AstralError as e:
            last = {"answered": False, "detail": f"{type(e).__name__}: {e}"}
        if time.monotonic() >= deadline:
            return last
        await asyncio.sleep(POLL)


async def cross_info(member: dict, replacement: dict, user: str, user_token: str) -> dict:
    """user.info as the User across the link, from each node of the other."""
    # why minted: member joined, so the driver holds no User token for it; a
    # joined member's node token mints one (apphost.create_token).
    async with await connect(member) as c:
        member_user = (await c.apphost.create_token(user)).token
    async with await connect(replacement, user_token) as u:
        of_member = await info_as_user(u, member["identity"])
    async with await connect(member, member_user) as u:
        of_replacement = await info_as_user(u, replacement["identity"])
    return {"replacement_of_member": of_member, "member_of_replacement": of_replacement}


async def main():
    doc = load()
    n, swarm = doc["nodes"], doc["facts"]["introduce_swarm"]
    member, sibling, replacement = (n[k] for k in ("member", "sibling", "replacement"))

    replaced = await claim_replacement(replacement, member, swarm["words"])
    before = {k: sorted(await members(n[k])) for k in ("member", "sibling", "replacement")}

    probes = {}
    async with await connect(replacement, None) as c:                # anonymous
        probes["tokenless"] = await introduce(c, member["identity"])
    async with await connect(sibling) as c:
        probes["joined_node"] = await introduce(c, member["identity"])
    async with await connect(replacement, replaced["Token"]) as u:
        probes["self"] = await introduce(u, replacement["identity"])
        introduced = await introduce(u, member["identity"])
    # why the node token: a node user.bootstrap claimed holds AdminSwarm by
    # delegation, and a second introduction to the same node is answered too.
    async with await connect(replacement) as c:
        probes["again"] = await introduce(c, member["identity"])

    cross = await cross_info(member, replacement, swarm["user"], replaced["Token"])

    write_facts({"introduce_node": {
        "replacement_user": replaced["Identity"], "before": before,
        "introduced": introduced, "probes": probes, "cross_info": cross}})
    print(f"driver: replacement claimed for {replaced['Identity']}; "
          f"introduce answered {introduced.get('types') or introduced.get('detail')}")


asyncio.run(main())
