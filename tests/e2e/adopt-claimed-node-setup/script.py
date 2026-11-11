#!/usr/bin/env python3
"""Driver: a second User, B, claims acn-b1 and adopts acn-b2 into a swarm of its own.

The setup of adopt-claimed-node, beside the first User's swarm (node1 and
node2 after adopt-node). B's key is made the way bootstrap-user-software-key
makes the first User's, on acn-b1; acn-b1 and acn-b2 learn each other's
endpoint, link over loopback, and B adopts acn-b2. The driver records B's
identity and token, and the id of the membership contract acn-b2 holds, for the oracle
of adopt-claimed-node to compare against.
"""
import asyncio

import astral
from astral.api.crypto import public_key_to_identity

from lib.sessionio import load, write_facts


async def main():
    doc = load()
    b1, b2 = doc["nodes"]["acn-b1"], doc["nodes"]["acn-b2"]

    async with await astral.connect(b1["endpoint"], token=b1["token"]) as c:
        entropy = await c.bip137sig.new_entropy(bits=256)
        mnemonic = await c.bip137sig.mnemonic(entropy)
        seed = await c.bip137sig.seed(mnemonic)
        privkey = await c.bip137sig.derive_key(seed, path="m/1791'/0'/0'/0/0")
        await c.objects.store(privkey)
        user_pub = await c.crypto.public_key(privkey)
        user_id = str(public_key_to_identity(user_pub))
        contract = await c.user.new_node_contract(user=user_id)
        signed = await c.auth.sign_contract(contract)
        await c.user.accept_contract(signed)
        user_token = (await c.apphost.create_token(user_id)).token
        # nodes.* is Tier-3 gated in astral-py: experimental=True per call.
        await c.nodes.add_endpoint(b2["identity"], b2["lan_endpoint"],
                                   experimental=True)

    async with await astral.connect(b2["endpoint"], token=b2["token"]) as c:
        await c.nodes.add_endpoint(b1["identity"], b1["lan_endpoint"],
                                   experimental=True)

    async with await astral.connect(b1["endpoint"], token=user_token) as c:
        await c.nodes.new_link(b2["identity"], endpoint=b2["lan_endpoint"],
                               strategies="basic", experimental=True,
                               timeout=60.0)
        await c.user.adopt(b2["identity"])

    async with await astral.connect(b2["endpoint"], token=b2["token"]) as c:
        contract_id = str((await c.user.info()).contract_id)

    write_facts({"acn_user_id": user_id, "acn_user_token": user_token,
                 "acn_contract_id": contract_id})
    print(f"driver: User B {user_id[:16]}… claimed acn-b1 and adopted acn-b2")


asyncio.run(main())
