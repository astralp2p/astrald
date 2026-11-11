#!/usr/bin/env python3
"""Driver: derive one User from the same words and a passphrase, three ways.

On a fresh node1: user.derive_identity with the words, with and without the
passphrase; bip137sig.seed, bip137sig.derive_key and crypto.public_key with and
without it; then user.bootstrap with the words and the passphrase, which claims
node1. The driver judges nothing.

why jsonops: astral-py has no binding for user.derive_identity or
user.bootstrap, and the ops' JSON form is the node's own. The passphrase is a
query argument of all three ops.
"""
import asyncio

import astral
from astral.api.crypto import public_key_to_identity

from lib import jsonops
from lib.sessionio import load, write_facts

WORDS = ("horse soldier imitate stool square buyer verb party enjoy "
         "result jazz rabbit trigger file benefit cloth term change")
PASSPHRASE = "astral-passphrase"
PATH = "m/1791'/0'/0'/0/0"


async def words_op(c, op: str, passphrase: str) -> dict:
    """user.derive_identity or user.bootstrap on WORDS: the answer object."""
    arg = f"&passphrase={passphrase}" if passphrase else ""
    body = {"Type": "string16", "Object": WORDS}
    return await jsonops.value(c, f"{op}?in=json&out=json{arg}", body)


async def seed_identity(c, passphrase: str) -> str:
    seed = await c.bip137sig.seed(WORDS, passphrase=passphrase)
    key = await c.bip137sig.derive_key(seed, path=PATH)
    return str(public_key_to_identity(await c.crypto.public_key(key)))


async def main():
    n1 = load()["nodes"]["node1"]
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        facts = {
            "derive_with": (await words_op(c, "user.derive_identity", PASSPHRASE))["Identity"],
            "derive_without": (await words_op(c, "user.derive_identity", ""))["Identity"],
            "seed_with": await seed_identity(c, PASSPHRASE),
            "seed_without": await seed_identity(c, ""),
        }
        claim = await words_op(c, "user.bootstrap", PASSPHRASE)
        facts["user_id"] = claim["Identity"]
        facts["user_token"] = claim["Token"]
    write_facts(facts)
    print("driver: " + " ".join(f"{k}={v[:16]}…" for k, v in facts.items()
                                if k != "user_token"))


asyncio.run(main())
