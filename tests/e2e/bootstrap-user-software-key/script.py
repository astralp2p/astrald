#!/usr/bin/env python3
"""Driver: create a software User key on node1 and activate its node contract.

Deterministic port of the node-setup playbook (netsim drove this via an AI
agent; here it is the exact op chain).
"""
import asyncio

import astral
from astral.api.crypto import public_key_to_identity

from lib.sessionio import load, write_facts


async def main():
    n1 = load()["nodes"]["node1"]
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        # A. software User key: entropy -> mnemonic -> seed -> private key
        entropy = await c.bip137sig.new_entropy(bits=256)
        mnemonic = await c.bip137sig.mnemonic(entropy)
        seed = await c.bip137sig.seed(mnemonic)
        privkey = await c.bip137sig.derive_key(seed, path="m/44'/0'/0'/0/0")

        # B. persist the key (crypto indexes it as a signer) + derive identity
        await c.objects.store(privkey)
        user_pub = await c.crypto.public_key(privkey)
        user_id = str(public_key_to_identity(user_pub))

        # C. build, sign, and activate the node contract
        contract = await c.user.new_node_contract(user=user_id)
        signed = await c.auth.sign_contract(contract)
        await c.user.accept_contract(signed)

        # D. mint an apphost token so later steps act AS the User
        access = await c.apphost.create_token(user_id)

    write_facts({"user_id": user_id, "user_token": access.token})
    print(f"driver: user {user_id[:16]}… bootstrapped, contract active")


asyncio.run(main())
