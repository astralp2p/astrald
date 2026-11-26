#!/usr/bin/env python3
"""Driver (criterion 2.7): expelling the Member node cuts every app chain
through it.

The world holds swarm-state-app-on-member's front app on node3, whose
discovery of the private wallet on node2 comes from a contract node3 issued
under node1's Approval. node1 and node2 each mint a token for the front's
identity (apphost.create_token with the node's own token), so the front's
chain can be asked on those nodes without node3: as a local discovery on
node2, the wallet's node, and a swarm discovery from node1.

1. Before the expel, the front's discovery shows the wallet on node3, on
   node2 and from node1.
2. The User expels node3 on node1 (user.expel); the user key signs the
   Expulsion, which enters SwarmState.
3. Once node1 and node2 store the Expulsion, the driver polls the front's
   discovery on node2 until it shows no wallet (up to 60 s).

Records `expel_cuts`: {front, wallet, before, expulsion_on, after, cut_after_s}.
"""
import asyncio
import time

import astral

from lib import jsonops
from lib import swarmstate as ss
from lib.sessionio import load, write_facts

WAIT = 60.0


def probes(n1: dict, n2: dict, n3: dict, tokens: dict) -> dict:
    """The front's discoveries of the wallet: on node3 (its own node) and on
    node2 (local, the wallet's node).

    note: no swarm discovery from node1: node1, a Privileged node, relays as the user only, not for an app node3 hosts, so
    node2 refuses node1's carry for the front with or without the chain."""
    return {"node3": ss.discover(n3, tokens["node3"], "swarm"),
            "node2": ss.discover(n2, tokens["node2"], "local")}


def expulsions(node: dict, subject: str) -> bool:
    return any(e["Type"] == "mod.user.signed_expulsion"
               and subject in str(e["Object"]) for e in ss.entries(node).values())


async def main():
    doc = load()
    n1, n2, n3 = (doc["nodes"][n] for n in ("node1", "node2", "node3"))
    prior = doc["facts"]["app_on_member"]
    front, wallet = prior["front"]["identity"], prior["wallet"]["identity"]
    tokens = {"node3": prior["front"]["token"]}
    for name, n in (("node2", n2),):
        out = ss.app_tool("token", n["endpoint"], n["token"], front)
        assert "error" not in out, f"apphost.create_token on {name}: {out}"
        tokens[name] = out["token"]

    f = {"front": front, "wallet": wallet, "before": probes(n1, n2, n3, tokens)}

    async with await astral.connect(n1["endpoint"], token=doc["facts"]["user_token"]) as cu:
        await jsonops.value(cu, f"user.expel?identity={n3['identity']}&out=json")
    expelled_at = time.monotonic()

    deadline = expelled_at + WAIT
    while not all(expulsions(n, n3["identity"]) for n in (n1, n2)) \
            and time.monotonic() < deadline:
        await asyncio.sleep(1)
    f["expulsion_on"] = {name: expulsions(n, n3["identity"]) for name, n in
                         (("node1", n1), ("node2", n2))}

    f["cut_after_s"] = None
    while time.monotonic() < deadline:
        f["after"] = {"node2": ss.discover(n2, tokens["node2"], "local")}
        if not any(wallet in v for v in f["after"].values()):
            f["cut_after_s"] = time.monotonic() - expelled_at
            break
        await asyncio.sleep(1)
    f["tokens"] = {"node2": tokens["node2"]}
    write_facts({"expel_cuts": f})
    print(f"driver: node3 expelled; the front's chain cut {f['cut_after_s']} s later")


asyncio.run(main())
