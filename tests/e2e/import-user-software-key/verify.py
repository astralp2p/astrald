#!/usr/bin/env python3
"""Oracle: the node is a User node, under the User the mnemonic derives.

Ported from the netsim import-user-software-key verify.sh, plus the check that
verify.sh could only make when the caller supplied ASTRAL_USER_ID: the derived
identity must equal EXPECTED_USER_ID. That equality is the whole point of this
test — it proves the EXISTING key was used and not a fresh one.
"""
import asyncio

import astral

from lib.sessionio import load

# The identity BIP-39 + BIP-32 m/44'/0'/0'/0/0 derives from the driver's
# mnemonic. Deterministic, so a pin here is a real oracle and not a snapshot.
EXPECTED_USER_ID = (
    "0205135bf7de5efbefcee6a2b9e149f53e3f21cb3a469271bb6222a47812aece73")


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    user_id = doc["facts"]["import_user_id"]
    user_token = doc["facts"]["import_user_token"]
    assert user_id and user_token, "import facts missing"
    assert user_id == EXPECTED_USER_ID, (
        f"derived User {user_id} != {EXPECTED_USER_ID} expected from the "
        "mnemonic — a fresh key was generated instead of the existing one")

    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        who = await c.apphost.whoami()
        assert str(who) == user_id, f"whoami {who} != user {user_id}"
        info = await c.user.info()          # raises if no active contract
        assert str(info.user_id) == user_id, (
            f"contract issuer {info.user_id} != user {user_id}")
        assert str(info.node_id) == n1["identity"], (
            f"contract subject {info.node_id} != node {n1['identity']}")
    print(f"oracle: imported User {user_id[:16]}… active, contract "
          "issuer/subject correct")


asyncio.run(main())
