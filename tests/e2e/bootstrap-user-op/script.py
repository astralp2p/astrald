#!/usr/bin/env python3
"""Driver: claim a node in one user.bootstrap call, from given or generated words.

node1 takes the words import-user-software-key derives from, so its User is
known in advance; a second call probes the refusal on a claimed node. node2
generates its words, and the driver derives an identity from the words node2
returned, through node2's own bip137sig ops.

why jsonops: astral-py has no user.bootstrap binding, and the op's JSON form
is the node's own.
"""
import asyncio

import astral
from astral.api.crypto import public_key_to_identity
from astral.errors import AstralError

from lib import jsonops
from lib.sessionio import load, write_facts

WORDS = ("horse soldier imitate stool square buyer verb party enjoy "
         "result jazz rabbit trigger file benefit cloth term change")
PATH = "m/1791'/0'/0'/0/0"


async def bootstrap(client, qs: str, body: dict | None) -> dict:
    """One user.bootstrap call: its answer, or how it was refused."""
    try:
        docs = await jsonops.call(client, qs, body)
    except AstralError as e:
        return {"refused": True, "detail": f"{type(e).__name__}: {e}"}
    return {"refused": False, "answer": docs}


def identity_in(result: dict) -> str:
    if result["refused"] or not result["answer"]:
        return ""
    return result["answer"][0]["Object"].get("Identity", "")


async def given_words(n1: dict) -> dict:
    words = {"Type": "string16", "Object": WORDS}
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        first = await bootstrap(c, "user.bootstrap?in=json&out=json", words)
        second = await bootstrap(c, "user.bootstrap?in=json&out=json", words)
        # why the claim's own token: user.bootstrap returns the User's
        # credential to the claiming app.
        token = first["answer"][0]["Object"].get("Token", "") if identity_in(first) else ""
    return {"first": first, "second": second, "user_token": token}


async def generated_words(n2: dict) -> dict:
    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        result = await bootstrap(c, "user.bootstrap?generate=true&out=json", None)
        derived = ""
        if identity_in(result):
            seed = await c.bip137sig.seed(result["answer"][0]["Object"]["Mnemonic"])
            key = await c.bip137sig.derive_key(seed, path=PATH)
            derived = str(public_key_to_identity(await c.crypto.public_key(key)))
    return {"generated": result, "derived": derived}


async def main():
    nodes = load()["nodes"]
    facts = await given_words(nodes["node1"])
    facts.update(await generated_words(nodes["node2"]))
    write_facts(facts)
    print(f"driver: first={facts['first']} second={facts['second']} "
          f"generated identity={identity_in(facts['generated'])} "
          f"derived={facts['derived']}")


asyncio.run(main())
