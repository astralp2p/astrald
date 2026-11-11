#!/usr/bin/env python3
"""Oracle: the same words and passphrase yield one User on every path.

user.derive_identity, the bip137sig.seed -> derive_key -> crypto.public_key
chain and user.bootstrap, each given the words and the passphrase, must all
name the pinned WITH_PASSPHRASE; the same words with no passphrase must name
the pinned WITHOUT_PASSPHRASE, a different identity, on both read paths. The
claimed node1 must answer, to the token the claim returned, as the pinned User
with an active contract node1 is the subject of.
"""
import asyncio

import astral

from lib.sessionio import load

# Both identities are BIP-39 + BIP-32 m/1791'/0'/0'/0/0 over the words below,
# computed outside astrald (PBKDF2-HMAC-SHA512 with salt "mnemonic" plus the
# passphrase, then BIP-32 and a secp256k1 point multiplication, in plain
# Python). The empty-passphrase identity is the one bootstrap-user-op and
# import-user-software-key pin.
WORDS = ("horse soldier imitate stool square buyer verb party enjoy "
         "result jazz rabbit trigger file benefit cloth term change")
PASSPHRASE = "astral-passphrase"
WITH_PASSPHRASE = (
    "02f1d2c29346ca6d1394ed15e725f8e8449c3f75fa77d8b484a50ae4b000637ce2")
WITHOUT_PASSPHRASE = (
    "02564ba75b0fc5869e12c0c89e56a9c4fbec9d23ed01e6d240aac80532fcb10af1")


def check_facts(facts: dict) -> None:
    assert WITH_PASSPHRASE != WITHOUT_PASSPHRASE
    for name, want in (("derive_with", WITH_PASSPHRASE),
                       ("seed_with", WITH_PASSPHRASE),
                       ("user_id", WITH_PASSPHRASE),
                       ("derive_without", WITHOUT_PASSPHRASE),
                       ("seed_without", WITHOUT_PASSPHRASE)):
        got = facts.get(name)
        assert got == want, (
            f"{name} is {got}, the words with "
            f"{'the' if want == WITH_PASSPHRASE else 'no'} passphrase "
            f"derive {want}")
    assert facts.get("user_token"), "the claim returned no token"


async def check_node(n1: dict, facts: dict) -> None:
    async with await astral.connect(n1["endpoint"],
                                    token=facts["user_token"]) as c:
        who = await c.apphost.whoami()
        assert str(who) == WITH_PASSPHRASE, f"whoami {who}"
        info = await c.user.info()
        assert str(info.user_id) == WITH_PASSPHRASE, (
            f"contract issuer {info.user_id}")
        assert str(info.node_id) == n1["identity"], (
            f"contract subject {info.node_id} != node {n1['identity']}")


async def main():
    doc = load()
    check_facts(doc["facts"])
    await check_node(doc["nodes"]["node1"], doc["facts"])
    print("oracle: derive_identity, bip137sig.seed and user.bootstrap agree on "
          f"{WITH_PASSPHRASE[:16]}… with the passphrase, and the same words "
          f"without it derive {WITHOUT_PASSPHRASE[:16]}…")


asyncio.run(main())
