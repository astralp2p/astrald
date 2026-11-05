#!/usr/bin/env python3
"""Driver: what a linked stranger and a swarm sibling may do to node1.

The driver acts and judges nothing. As the User on node1 it stores a bait
object. A third node, `stranger`, is claimed by a second User through a
token-less bootstrap and links to node1. Three askers then send every probe to
node1 over the link: the stranger with its node token, the stranger with its
own User's token, and the sibling node2 with its node token. The driver records
how each probe came back: allowed, refused, or reset.

why every probe names node1 as its target: the asker's node relays the query
over its link, so node1 sees a network caller, which is the case
astral-docs topics/node-exposure.md "Linked peers" describes.
why harmful writes aim at the stranger: user.adopt and user.expel name the
stranger, so a guard that broke would change only the stranger's standing,
never node2's, and the oracle still sees it.
why the stranger's links stay open: no test after this one in its world asserts
an exact link set or an exact provider set (read-swarm asks only that node2 is
among the providers), and the stranger holds no object of theirs. The oracle
needs the link for its own probes.
"""
import asyncio

import astral
from astral.errors import ProtocolError
from astral.object import Query

from lib.sessionio import load, write_facts

# why a deadline: a query that is never routed hangs rather than answering, and
# the client's default is 60 s. apphost-origin settled on 15 for the same reason.
DEADLINE = 15

BAIT = Query(query_string="swarm-authz-matrix bait")
# The oracle names these three as well, and checks node1 holds none of them.
INTRUDER = Query(query_string="swarm-authz-matrix intruder")
TREE_PATH = "/swarm-authz-matrix"
ALIAS = "hijack"
POLICY_OP = "swarm_authz_matrix.decide"

# note: a write that carries a body and is refused over a link does not read as
# "not permitted". node1 answers the refusal and closes the stream before it
# reads the body; the body the client already wrote then makes the relay reset
# the connection, and the refusal is lost. Which way it is lost is a race:
# astral-py reports ConnectionResetError ("Connection lost"), or a
# ProtocolError — "... destroyed its own error message" for crypto.sign_text,
# "the stream ended after 0 of 1 answers" for objects.store, "sent 1 object(s)
# and got 0 answer(s)" for tree.set. The same ops answer "not permitted" to a
# local caller. This driver records any such end as `reset`, and the oracle
# judges those probes by node1's state, never by their text.
# What a refusal names: the op's own refusal, apphost's, or node1 rejecting the
# query before any op ran.
REFUSAL_MARKS = ("not permitted", "origin not allowed", "query rejected",
                 "denied")


def probes(c, node1: str, bait: str, stranger: str) -> dict:
    """Each probe as a thunk, so a probe's coroutine exists only when it runs."""
    t = {"target": node1}
    return {
        # positive controls: what node-exposure.md says any linked peer reaches
        "apphost.whoami": lambda: c.apphost.whoami(**t),
        "dir.resolve": lambda: c.dir.resolve("localnode", **t),
        "shell.spec": lambda: c.shell.spec(**t),
        # reads a swarm member is granted
        "user.info": lambda: c.user.info(**t),
        "user.swarm_status": lambda: c.user.swarm_status(**t),
        "user.list_siblings": lambda: c.user.list_siblings(**t),
        "objects.read": lambda: c.objects.read(bait, **t),
        "objects.contains": lambda: c.objects.contains("local", bait, **t),
        "objects.load": lambda: c.objects.load(bait, repo="local", **t),
        "objects.repositories": lambda: c.objects.repositories(**t),
        "nodes.links": lambda: c.nodes.links(experimental=True, **t),
        # reads and writes no network caller is granted
        "tree.list": lambda: c.tree.list("/", **t),
        "apphost.list_tokens": lambda: c.apphost.list_tokens(**t),
        "objects.store": lambda: c.objects.store(INTRUDER, **t),
        "tree.set": lambda: c.tree.set(TREE_PATH, INTRUDER, **t),
        "dir.set_alias": lambda: c.dir.set_alias(stranger, ALIAS, **t),
        "apphost.create_token": lambda: c.apphost.create_token(stranger, **t),
        "apphost.hold_object": lambda: c.apphost.hold_object(bait, **t),
        "user.adopt": lambda: c.user.adopt(stranger, **t),
        "user.expel": lambda: c.user.expel(stranger, **t),
        "user.set_swarm_join_policy": lambda: c.user.set_swarm_join_policy(
            stranger, POLICY_OP, **t),
        "apphost.set_app_register_policy": lambda: c.apphost.set_app_register_policy(
            stranger, POLICY_OP, **t),
        "crypto.sign_text": lambda: c.crypto.sign_text("swarm-authz-matrix", **t),
        # last, so a broken guard leaves the reads above a bait to read
        "objects.delete": lambda: c.objects.delete("local", bait, **t),
    }


def answer_text(v) -> str:
    """A short, comparable form of what an allowed probe answered."""
    if isinstance(v, list):
        return f"{len(v)} item(s)"
    return str(v)[:120]


async def attempt(thunk) -> dict:
    """Run one probe and record its outcome class and how it read.

    why the deadline is wait_for and not the client's timeout: two setters take
    `timeout=` as their delegate's timeout, not the query's.
    """
    try:
        v = await asyncio.wait_for(thunk(), DEADLINE)
    except Exception as e:  # noqa: BLE001 — every failure is recorded, not raised
        detail = f"{type(e).__name__}: {e}"[:300]
        if any(m in detail.lower() for m in REFUSAL_MARKS):
            return {"outcome": "refused", "detail": detail}
        if isinstance(e, (ConnectionResetError, ProtocolError)):
            return {"outcome": "reset", "detail": detail}
        return {"outcome": "error", "detail": detail}
    return {"outcome": "allowed", "detail": "answered", "answer": answer_text(v)}


async def run_probes(endpoint: str, token: str, node1: str, bait: str,
                     stranger: str) -> dict:
    async with await astral.connect(endpoint, token=token) as c:
        return {name: await attempt(thunk)
                for name, thunk in probes(c, node1, bait, stranger).items()}


async def link_stranger(n1: dict, s: dict) -> None:
    """The stranger learns node1's address and links to it, as itself.

    why the stranger dials and node1 learns nothing: node1 should end this test
    holding no record of the stranger it did not have before.
    """
    async with await astral.connect(s["endpoint"], token=s["token"]) as c:
        await c.nodes.add_endpoint(n1["identity"], n1["lan_endpoint"],
                                   experimental=True)
        await c.nodes.new_link(n1["identity"], endpoint=n1["lan_endpoint"],
                               strategies="basic", experimental=True,
                               timeout=60.0)


async def main():
    doc = load()
    nodes, facts = doc["nodes"], doc["facts"]
    n1, n2, s = nodes["node1"], nodes["node2"], nodes["stranger"]

    async with await astral.connect(n1["endpoint"], token=facts["user_token"]) as u:
        (bait,) = await u.objects.store(BAIT, repo="local")
    bait = str(bait)

    # why a token-less bootstrap: it is how a second, unrelated User claims a
    # node, and its answer carries that User's token on the stranger.
    async with await astral.connect(s["endpoint"]) as anon:
        v_token = str((await anon.user.bootstrap(None)).token)

    await link_stranger(n1, s)

    args = (n1["identity"], bait, s["identity"])
    matrix = {
        "stranger_node": await run_probes(s["endpoint"], s["token"], *args),
        "stranger_user": await run_probes(s["endpoint"], v_token, *args),
        "sibling": await run_probes(n2["endpoint"], n2["token"], *args),
    }

    write_facts({"bait_id": bait, "matrix": matrix})
    for asker, row in matrix.items():
        allowed = sorted(p for p, r in row.items() if r["outcome"] == "allowed")
        other = sorted(f"{p}={r['outcome']}" for p, r in row.items()
                       if r["outcome"] not in ("allowed", "refused"))
        print(f"driver: {asker}: allowed {allowed}; not refused cleanly {other}")


asyncio.run(main())
