#!/usr/bin/env python3
"""Driver: build a User's swarm on `first`, the node that generates the words.

The driver acts and judges nothing. It claims `first` with generated words,
joins `member`, `sibling` and `banned` to it, and expels `banned` as the User.
The world stops `first` after the driver (test.toml `stop`).

why the words are a fact: introduce-node claims a replacement with them once
`first` is gone, and the words are the only thing of the User that outlives
`first`. They are a throwaway User's, generated for this world.

why jsonops: astral-py has no binding for user.bootstrap or user.join, and
the ops' JSON form is the node's own.
"""
import asyncio
import sys
import time

import astral

from lib import jsonops
from lib.sessionio import load, write_facts

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
    """A session on node: its node token by default, or another token."""
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


async def main():
    n = load()["nodes"]
    first, member, sibling, banned = (n[k] for k in ("first", "member", "sibling", "banned"))

    claim = await build_swarm(first, [member, sibling, banned])
    await expel(first, claim["Token"], banned, [member, sibling])
    if sibling["identity"] not in await converge(lambda: members(member), lambda m: sibling["identity"] in m):
        sys.exit("driver: member never listed sibling")

    write_facts({"introduce_swarm": {
        "user": claim["Identity"], "words": claim["Mnemonic"], "first": first["identity"]}})
    print(f"driver: {claim['Identity']} claimed on first; member, sibling and banned "
          "joined, banned expelled")


asyncio.run(main())
