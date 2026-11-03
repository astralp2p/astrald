#!/usr/bin/env python3
"""Driver: bring a node claimed with the User's words into the swarm with user.introduce.

The driver acts and judges nothing. It claims `first` with generated words,
joins `member`, `sibling` and `banned` to it, and expels `banned` as the User.
It appoints a manager on the fresh `replacement`, claims it with the same
words as the manager, and records what `member`, `sibling` and `replacement`
list before the introduction. As the User, `replacement` then introduces
itself to `member`. The driver also asks for the op without a token, from
`sibling`'s node token, naming `replacement` itself, and once more with
`replacement`'s node token.

why `first` stays up: a driver cannot stop a node, and a node that dies under
a test fails it (lib/runner.py). `replacement` learns no member but `member`
from the driver, so the members reach it through the introduction alone.

why jsonops: astral-py has no binding for apphost.register_manager,
user.bootstrap, user.join or user.introduce, and the ops' JSON form is the
node's own.
"""
import asyncio
import sys
import time

import astral
from astral.errors import AstralError

from lib import jsonops
from lib.sessionio import load, write_facts

MANAGER = {"Type": "mod.apphost.register_manager_request", "Object": {"Name": "manager"}}
# why a deadline: a query that is never routed hangs rather than answering, and
# the client's default is 60 s.
DEADLINE = 30
# why a window: joins, bans and sibling links reach the other members
# asynchronously.
CONVERGE = 60.0
POLL = 1.0


async def converge(read, done):
    """Read until done holds or the window closes; answer the last read."""
    deadline = time.monotonic() + CONVERGE
    value = await read()
    while not done(value) and time.monotonic() < deadline:
        await asyncio.sleep(POLL)
        value = await read()
    return value


def connect(node: dict, token: str | None = "node"):
    """A session on node: its node token by default, another token, or none."""
    return astral.connect(node["endpoint"], token=node["token"] if token == "node" else token)


async def members(node: dict) -> set:
    async with await connect(node) as c:
        return {str(m.identity) for m in await c.user.swarm_status() if m.identity is not None}


async def expelled(node: dict) -> set:
    async with await connect(node) as c:
        return {str(e.expulsion.subject) for e in await c.user.list_expelled()
                if e.expulsion is not None and e.expulsion.subject is not None}


async def link(x: dict, y: dict) -> None:
    """Each node learns the other's endpoint, with its own token."""
    for a, b in ((x, y), (y, x)):
        async with await connect(a) as c:
            await c.nodes.add_endpoint(b["identity"], b["lan_endpoint"], experimental=True)


async def introduce(c, node: str) -> dict:
    """What user.introduce naming node answered, or how the query was refused."""
    try:
        docs = await jsonops.call(c, f"user.introduce?identity={node}&out=json", timeout=DEADLINE)
    except AstralError as e:
        return {"refused": True, "detail": f"{type(e).__name__}: {e}"}
    if docs and docs[0]["Type"] == "error_message":
        return {"refused": True, "detail": docs[0]["Object"]}
    return {"refused": False, "types": [d["Type"] for d in docs]}


async def build_swarm(first: dict, joiners: list) -> dict:
    """Claim first with generated words, join every joiner to it, and answer the claim."""
    async with await connect(first) as c:
        claim = await jsonops.value(c, "user.bootstrap?generate=true&out=json")
    for j in joiners:
        await link(first, j)
        async with await connect(j) as c:
            await jsonops.value(c, f"user.join?node={first['identity']}&out=json", timeout=DEADLINE)
    want = {first["identity"]} | {j["identity"] for j in joiners}
    if not want <= await converge(lambda: members(first), lambda m: want <= m):
        sys.exit("driver: the joined nodes never reached the first node's swarm")
    return claim


async def expel(first: dict, user_token: str, banned: dict, holders: list) -> None:
    """Expel banned as the User and wait until every holder holds the ban."""
    async with await connect(first, user_token) as c:
        await c.user.expel(banned["identity"])
    for h in holders:
        if banned["identity"] not in await converge(lambda: expelled(h), lambda e: banned["identity"] in e):
            sys.exit(f"driver: the ban of banned never reached {h['identity']}")


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


async def main():
    n = load()["nodes"]
    first, member, sibling, banned, replacement = (
        n[k] for k in ("first", "member", "sibling", "banned", "replacement"))

    claim = await build_swarm(first, [member, sibling, banned])
    await expel(first, claim["Token"], banned, [member, sibling])
    if sibling["identity"] not in await converge(lambda: members(member), lambda m: sibling["identity"] in m):
        sys.exit("driver: member never listed sibling")

    replaced = await claim_replacement(replacement, member, claim["Mnemonic"])
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

    write_facts({"introduce_node": {
        "user": claim["Identity"], "replacement_user": replaced["Identity"],
        "before": before, "introduced": introduced, "probes": probes}})
    print(f"driver: replacement claimed for {replaced['Identity']}; "
          f"introduce answered {introduced.get('types') or introduced.get('detail')}")


asyncio.run(main())
