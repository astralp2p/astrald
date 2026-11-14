#!/usr/bin/env python3
"""Oracle: of many claims at one instant, exactly one wins, and the node keeps
that User alone.

Per node of the roster, first from the node itself, with its own token:
- the active contract in the tree store (/mod/user/config/active_contract)
  names a contender as issuer and the node as subject;
- the node's log announces exactly one activated contract, for that issuer;
- user.info answers state claimed and the same contract, by ObjectID;
- apphost.list_tokens holds a token for no claiming User but a bootstrap
  winner, and for the issuer exactly when it claimed through user.bootstrap;
- a bootstrap the oracle sends with words of its own is refused as
  `node already claimed`, and the active contract is the same object after it.

Then from the facts: exactly one contender won, and it is the issuer; every
other one was refused with a claim refusal the user module answers, none hung
and none broke; both late claims were refused before the op read their body.
"""
import asyncio
import re

import astral
from astral.errors import RemoteError
from astral.objectid import object_id

from lib import nodefs
from lib.sessionio import load

ROSTER = ["race1", "race2", "race3", "race4"]
CONTENDERS = 12
ACTIVE = "/mod/user/config/active_contract"
ALREADY_CLAIMED = "node already claimed"
# The texts a claim that lost answers, in mod/user/src at 9f647ff1:
# - op_bootstrap.go, op_accept_contract.go: the op's own check, before it reads;
# - contracts.go errClaimed: claim() found the contract another claim set;
# - op_bootstrap.go errClaimRefused: signClaim, after the claim of another,
#   finds the claim rule of auth.sign_contract closed.
LOST = (
    ALREADY_CLAIMED,
    "the node has an active contract",
    "claim refused: the claim rule of auth.sign_contract refuses this user",
)
HELLO = "] hello, "
ANSI = re.compile(r"\x1b\[[0-9;]*m")
# Generated 24 words the oracle claims with; it never needs the User they name.
ORACLE_CLAIM = "user.bootstrap?generate=true"


def check_facts(name: str, r: dict) -> dict:
    """The race as the driver recorded it; returns the winning contender."""
    contenders = r["contenders"]
    assert len(contenders) == CONTENDERS, (
        f"{name}: {len(contenders)} contenders recorded, want {CONTENDERS}")
    users = {c["user"] for c in contenders}
    assert len(users) == CONTENDERS, f"{name}: contenders are not distinct Users"
    won = [c for c in contenders if c["outcome"] == "won"]
    assert len(won) == 1, (
        f"{name}: {len(won)} claims won, want exactly one: "
        + ", ".join(f"{c['user'][:16]}… via {c['op']}" for c in won))
    for c in contenders:
        if c is won[0]:
            continue
        assert c["outcome"] == "refused", (
            f"{name}: a losing claim via {c['op']} ended {c['outcome']!r} "
            f"after {c['elapsed']}s, not a refusal: {c['detail']}")
        assert c["detail"] in LOST, (
            f"{name}: a losing claim via {c['op']} was refused with "
            f"{c['detail']!r}, not a claim refusal {LOST}")
        assert not c["token"], f"{name}: a losing claim received a token"
    for c in r["late"]:
        assert c["outcome"] == "refused" and c["detail"] == ALREADY_CLAIMED, (
            f"{name}: a late claim via {c['op']} on the claimed node ended "
            f"{c['outcome']!r}: {c['detail']}; want {ALREADY_CLAIMED!r} before "
            "the op reads")
    return won[0]


async def active_contract(c):
    signed = await c.tree.get(ACTIVE)
    assert getattr(signed, "contract", None) is not None, (
        f"{ACTIVE} holds {signed!r}, not a signed contract")
    return signed


def fingerprint(identity: str) -> str:
    """How astrald logs an identity without an alias (astral-go
    astral/identity.go Fingerprint)."""
    return f"{identity[:8]}:{identity[-8:]}"


def check_activations(name: str, node: dict, issuer: str) -> None:
    """The node's log names one activated contract, the active one's issuer.

    why the log: each contract the active-contract store takes is announced
    once, by onActiveContractChanged (mod/user/src/contracts.go), as
    `hello, <user>!`. A second winner's contract is replaced in the store, so
    the log is the node's own record that it took more than one.
    """
    text = ANSI.sub("", nodefs.log(node))
    hellos = [line for line in text.splitlines() if HELLO in line]
    assert len(hellos) == 1, (
        f"{name}: the node activated {len(hellos)} contracts, want one: "
        + " | ".join(h.split(HELLO, 1)[1] for h in hellos))
    assert fingerprint(issuer) in hellos[0], (
        f"{name}: the node activated {hellos[0].split(HELLO, 1)[1]!r}, not the "
        f"active contract's issuer {fingerprint(issuer)}")


async def check_node(name: str, node: dict, r: dict) -> str:
    """The node's own state after the race; returns the issuer it holds."""
    by_user = {c["user"]: c for c in r["contenders"]}
    everyone = set(by_user) | {c["user"] for c in r["late"]}
    async with await astral.connect(node["endpoint"], token=node["token"]) as c:
        signed = await active_contract(c)
        issuer = str(signed.contract.issuer)
        assert str(signed.contract.subject) == node["identity"], (
            f"{name}: the active contract's subject is "
            f"{signed.contract.subject}, not the node {node['identity']}")
        assert issuer in by_user, (
            f"{name}: the active contract's issuer {issuer} is no contender")
        check_activations(name, node, issuer)

        info = await c.user.info()
        assert info.state == "claimed", f"{name}: user.info state {info.state!r}"
        assert str(info.user_id) == issuer and str(info.node_id) == node["identity"], (
            f"{name}: user.info names {info.user_id} on {info.node_id}; the "
            f"active contract names {issuer} on {node['identity']}")
        want_id = object_id(signed)
        assert info.contract_id == want_id and object_id(info.contract) == want_id, (
            f"{name}: user.info's contract {info.contract_id} is not the active "
            f"contract {want_id}")

        tokens = {str(t.identity) for t in await c.apphost.list_tokens()}
        holders = tokens & everyone
        want = {issuer} if by_user[issuer]["op"] == "user.bootstrap" else set()
        assert holders == want, (
            f"{name}: the node holds tokens for {sorted(h[:16] for h in holders)}"
            f" of the claiming Users; want {sorted(w[:16] for w in want)}")

        try:
            await c.call_with(ORACLE_CLAIM, eos=False)
            late = "answered"
        except RemoteError as e:
            late = e.message
        assert late == ALREADY_CLAIMED, (
            f"{name}: the oracle's own claim on the claimed node: {late!r}")
        after = await active_contract(c)
        assert object_id(after) == want_id, (
            f"{name}: the active contract changed from {want_id} to "
            f"{object_id(after)} after the oracle's claim")
    return issuer


async def main():
    doc = load()
    race = doc["facts"]["claim_race"]
    for name in ROSTER:
        issuer = await check_node(name, doc["nodes"][name], race[name])
        winner = check_facts(name, race[name])
        assert winner["user"] == issuer, (
            f"{name}: the node is claimed by {issuer[:16]}…, but the claim "
            f"that won answered {winner['user'][:16]}…")
        print(f"oracle: {name}: one of {CONTENDERS} claims won "
              f"({winner['user'][:16]}… via {winner['op']}), "
              f"{CONTENDERS - 1} refused, late claims refused")


asyncio.run(main())
