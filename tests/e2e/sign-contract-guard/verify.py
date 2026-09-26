#!/usr/bin/env python3
"""Oracle: auth.sign_contract signed the entitled contract and refused the rest.

The control first: a node that signs nothing would satisfy every refusal. Then
each local refusal must read as the guard's own, naming the party it refused
and never as a missing key ("sign as ..."), and the network refusal must read
as a rejection rather than a lost route.

Then the claim cleared from the tree. A stranger's claim must pass the guard
and fail only at signing, which shows the node reads as unclaimed and the claim
exception is open. The User's claim must still be refused by the guard, because
the User once claimed node1. node1 must answer as the User's node afterwards.

Last, what the app holds. Whether the forged sudo contract was refused and
whether the app nevertheless acts as the User are two claims, and the driver is
the wrong party for the second. The oracle asks node1 to sign a text under the
User's key as the app: a sudo contract from the User to the app, signed and
indexed, would make that answer.
"""
import asyncio

import astral
from astral.errors import AstralError
from astral.types import Identity

from lib.sessionio import load

FOREIGN = "cannot sign with another identity's key"
NODE_KEY = "cannot sign with the node's key"

EXPECTED = {
    "user names the app as subject": f"authorize subject: {FOREIGN}",
    "app forges user→app sudo": f"authorize issuer: {FOREIGN}",
    "app names the user as subject": f"authorize subject: {FOREIGN}",
    "node token asks the claim on a claimed node": f"authorize issuer: {FOREIGN}",
    "anonymous asks the node key": f"authorize issuer: {NODE_KEY}",
}

# What a refusal must not read like — each means the call never got a verdict.
NOT_A_REFUSAL = ("timeout", "timed out", "route_not_found", "routenotfound",
                 "nodeunavailable", "unknown target", "connection refused")


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    facts = doc["facts"]
    guard = facts["sign_guard"]

    control = guard["control"]
    assert not control["refused"] and control["both_signed"], (
        f"the User was refused its own contract on node1: {control['detail']} "
        "— the op signs for no one, so the refusals below prove nothing")

    probes = guard["probes"]
    assert set(probes) == set(EXPECTED) | {"the user over the link"}, (
        f"probes ran: {sorted(probes)}")

    for name, text in EXPECTED.items():
        r = probes[name]
        assert r["refused"], f"{name}: node1 signed a contract the caller may not sign"
        assert text in r["detail"], (
            f"{name}: refused with {r['detail']!r}, not the guard's {text!r}")
        assert "sign as" not in r["detail"], (
            f"{name}: {r['detail']!r} reads as a missing key, which setup "
            "clients retry")

    r = probes["the user over the link"]
    detail = r["detail"].lower()
    assert r["refused"], "node1 signed a contract for a query of network origin"
    assert not any(m in detail for m in NOT_A_REFUSAL), (
        f"the user over the link: failed without a verdict: {r['detail']}")
    assert "rejected with code 1" in detail, (
        f"the user over the link: {r['detail']} names no rejection")

    cleared = guard["cleared"]
    control = cleared["stranger's claim, claim cleared"]
    assert control["refused"] and "authorize" not in control["detail"] \
        and "sign as issuer: unsupported" in control["detail"], (
            f"the stranger's claim answered {control['detail']} with the claim "
            "cleared — the exception is closed, so the refusal below proves nothing")
    r = cleared["user's claim, claim cleared"]
    assert r["refused"], ("node1 signed the User's claim once the claim was "
                          "cleared from the tree")
    assert f"authorize issuer: {FOREIGN}" in r["detail"], (
        f"the User's claim, claim cleared: {r['detail']!r}")
    user = str(Identity.from_json(facts["user_id"]))
    assert cleared["restored"] == user, (
        f"node1 answers as {cleared['restored']}, not the User {user}")

    async with await astral.connect(n1["endpoint"], token=guard["app_token"]) as a:
        try:
            await a.crypto.sign_text("probe", key=facts["user_id"])
        except AstralError as e:
            assert FOREIGN in str(e), f"the app's text signature as the User: {e}"
        else:
            raise AssertionError("the app signs under the User's key — it acts "
                                 "as the User")

    print(f"oracle: node1 signed the User's own contract, refused "
          f"{len(EXPECTED)} forged parties by name and the network origin, "
          "refused the User's claim with the claim cleared from the tree, and "
          "the app still cannot act as the User")


asyncio.run(main())
