#!/usr/bin/env python3
"""Driver: make node1 a User node from an EXISTING mnemonic.

The alternative path to bootstrap-user-software-key: same contract flow, but
the key comes from the fixed BIP-39 mnemonic below instead of fresh entropy.
The mnemonic is the one the netsim import-user-software-key prompt carries
verbatim, so both drivers derive the same User.

This test saves no state — a second producer of one-node would silently
shadow bootstrap's, so it is a terminal alternative path (migration plan,
open decision 3).
"""
import asyncio

import astral
from astral.api.crypto import public_key_to_identity

from lib.sessionio import load, write_facts

MNEMONIC = ("horse soldier imitate stool square buyer verb party enjoy "
            "result jazz rabbit trigger file benefit cloth term change")
PATH = "m/44'/0'/0'/0/0"


async def main():
    n1 = load()["nodes"]["node1"]
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        # A. the existing key: mnemonic -> seed -> private key. No new_entropy.
        seed = await c.bip137sig.seed(MNEMONIC)
        privkey = await c.bip137sig.derive_key(seed, path=PATH)

        # B. persist the key (crypto indexes it as a signer) + derive identity
        await c.objects.store(privkey)
        user_pub = await c.crypto.public_key(privkey)
        user_id = str(public_key_to_identity(user_pub))

        # C. build, sign, and activate the node contract
        contract = await c.user.new_node_contract(user=user_id)
        signed = await c.auth.sign_contract(contract)
        await c.user.accept_contract(signed)

        # D. mint an apphost token so the oracle can act AS the User
        access = await c.apphost.create_token(user_id)

    write_facts({"import_user_id": user_id, "import_user_token": access.token})
    print(f"driver: user {user_id[:16]}… imported from the mnemonic, "
          "contract active")


asyncio.run(main())
