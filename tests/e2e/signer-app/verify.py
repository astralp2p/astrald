#!/usr/bin/env python3
"""Oracle: every step held, and the claim outlives the app that signed it.

The app is gone by the time this runs, so the oracle judges what it left behind:
signer1 answers as the User's node, and nothing signs under the User's key.
"""
import asyncio

import astral
from astral.errors import AstralError

from lib.sessionio import load

STEPS = [
    "bound",
    "claim_signed",
    "claim_issuer_is_bip137",
    "claim_subject_is_asn1",
    "claim_accepted",
    "text_signed_and_verified",
    "app_saw_the_text",
    "foreign_caller_refused",
    "foreign_caller_never_reached_the_app",
    "no_permit_refused",
    "node_key_refused",
    "claimed_key_refused",
    "empty_set_refused",
    "local_key_signed_locally",
    "local_key_never_reached_the_app",
    "decline_reached_the_caller",
    "decline_asked_once",
    "forgery_refused",
    "forgery_ended_the_binding",
    "rebound_after_forgery",
    "closed_binding_holds_nothing",
]


async def main():
    doc = load()
    facts = doc["facts"].get("signer_app", {})
    missing = [s for s in STEPS if facts.get(s) is not True]
    assert not missing, f"steps that did not hold: {missing}"

    user_id = doc["facts"]["signer_app_user"]
    n = doc["nodes"]["signer1"]
    async with await astral.connect(n["endpoint"], token=n["token"]) as c:
        info = await c.user.info()
        assert str(info.user_id) == user_id, (
            f"contract issuer {info.user_id} != user {user_id}")
        assert str(info.node_id) == n["identity"], (
            f"contract subject {info.node_id} != node {n['identity']}")

        access = await c.apphost.create_token(user_id)

    async with await astral.connect(n["endpoint"], token=access.token) as c:
        try:
            await c.crypto.sign_text("the app is gone")
        except AstralError as e:
            print(f"oracle: signing after the app left answers: {e}")
        else:
            raise AssertionError("something signed under a key nothing should hold")

    print("oracle: the claim stands and the key is gone")


asyncio.run(main())
