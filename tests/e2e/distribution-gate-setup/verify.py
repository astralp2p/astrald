#!/usr/bin/env python3
"""Oracle: a node under the shipped default gate was set up the way node-setup.md
describes, and the gate holds its denials before the claim and after it.

The facts say what the setup did: the states user.info read, the manager, the
User and its token, and what the gate answered the callers it keeps out. The
oracle asks the claimed node itself for the rest, so it judges an operator's
run as it judges the script's.
"""
import asyncio
import re

import astral
from astral.errors import AstralError

from lib import jsonops
from lib.sessionio import load

# The identity BIP-39 + BIP-32 m/1791'/0'/0'/0/0 derives from the 24 words
# "abandon" x 23 and "art", computed outside astrald (PBKDF2-HMAC-SHA512 and
# BIP-32 in plain Python). Deterministic, so a pin here is a real oracle.
PINNED_USER = (
    "0397193d7e662bee6bf4b9efcf28878e47fe90397acd497785001b5ee8005f93ad")
# why generate: the op then reads no words, and a refusal that closes the
# connection with the words unread resets it (bootstrap-user-op)
CLAIM = "user.bootstrap?generate=true&out=json"
GENERATE = "user.derive_identity?generate=true&out=json"

# What setup owes whoever drove it: the denials of a session without a token
# that node-setup.md tells an operator to expect.
REQUIRED = ("fresh/tokenless/user.bootstrap", "unclaimed/tokenless/user.bootstrap")
# A recorded denial the script wrote: the op it names, then apphost's answer.
DENIED = re.compile(r"Denied: ([a-z_.]+)\?[^ ]*: apphost error: denied")

NOT_FRESH = "node is not fresh"
ALREADY_CLAIMED = "node already claimed"


async def attempt(coro) -> dict:
    try:
        answer = await coro
    except (AstralError, jsonops.OpError) as e:
        return {"refused": True, "detail": f"{type(e).__name__}: {e}"}
    return {"refused": False, "detail": "answered", "answer": answer}


def denied(r: dict) -> bool:
    return r["refused"] and r["detail"].startswith("Denied") \
        and r["detail"].endswith("apphost error: denied")


def check_recorded(facts: dict) -> None:
    probes = facts["gate_probes"]
    assert isinstance(probes, dict), f"gate_probes is {probes!r}, not an object"
    for key in REQUIRED:
        assert key in probes, f"the setup recorded no answer for {key}: {sorted(probes)}"
    for key, r in probes.items():
        text = r["detail"] if isinstance(r, dict) else str(r)  # a script's, or an operator's
        op = key.rsplit("/", 1)[1]
        assert "denied" in text.lower(), (
            f"{key}: the gate answered {text!r} — want denied")
        m = DENIED.fullmatch(text)
        if text.startswith("Denied"):
            assert m and m.group(1) == op, (
                f"{key}: answered {text!r} — want 'Denied: {op}…: apphost error: denied'")


async def main():
    doc = load()
    n1, facts = doc["nodes"]["node1"], doc["facts"]
    manager, user = facts["gate_manager_id"], facts["gate_user_id"]

    assert user == PINNED_USER, (
        f"the claim made {user} the User; the pinned words derive {PINNED_USER}")
    assert manager and manager not in (user, n1["identity"]), (
        f"the manager {manager} is not an identity of its own")
    check_recorded(facts)
    assert facts["gate_states"] == {"before": "fresh", "appointed": "unclaimed",
                                    "claimed": "claimed"}, (
        f"user.info read {facts['gate_states']} — want fresh, then unclaimed, "
        "then claimed")

    ep = n1["endpoint"]
    async with await astral.connect(ep, token=facts["gate_user_token"]) as u:
        who = await attempt(u.apphost.whoami())
        assert not who["refused"], (
            f"apphost.whoami with the User's token on the claimed node: "
            f"{who['detail']} — the gate does not open to the claimed list")
        who = who["answer"]
        info = await u.user.info()
        again = await attempt(jsonops.value(u, CLAIM))
        after = await u.user.info()
    assert str(who) == user, f"the claim's token authenticates as {who}, not the User {user}"
    assert info.state == "claimed" and str(info.manager) == manager, (
        f"user.info: state {info.state}, manager {info.manager}; want claimed under {manager}")
    assert str(info.user_id) == user and str(info.node_id) == n1["identity"], (
        f"contract {info.user_id} -> {info.node_id}; want {user} -> {n1['identity']}")
    assert again["refused"] and ALREADY_CLAIMED in again["detail"], (
        f"a second claim with the User's token: {again['detail']}")
    assert str(after.user_id) == user, f"a second claim changed the User to {after.user_id}"

    out = {}
    async with await astral.connect(ep) as c:              # no token
        out["info"] = await attempt(c.user.info())
        out["register_manager"] = await attempt(jsonops.value(
            c, "sys.register_manager?in=json&out=json",
            {"Type": "mod.sys.register_manager_request", "Object": {"Name": "late"}}))
        out["register"] = await attempt(c.apphost.register())
        out["spec"] = await attempt(jsonops.call(c, ".spec?out=json"))
        for op in ("user.bootstrap", "user.derive_identity", "user.join",
                   "apphost.list_tokens", "shell.spec"):
            out[op] = await attempt(jsonops.value(c, {
                "user.bootstrap": CLAIM, "user.derive_identity": GENERATE,
                "user.join": f"user.join?node={n1['identity']}&out=json"}.get(op, op)))
    async with await astral.connect(ep, token=n1["token"]) as c:    # the node's own
        out["node list_tokens"] = await attempt(c.apphost.list_tokens())
        out["node bootstrap"] = await attempt(jsonops.value(c, CLAIM))
    i = out["info"]
    assert not i["refused"] and i["answer"].state == "claimed", (
        f"user.info without a token on the claimed node: {i['detail']}")
    r = out["register_manager"]
    assert r["refused"] and NOT_FRESH in r["detail"], (
        f"sys.register_manager on the claimed node: {r['detail']} — want {NOT_FRESH!r}")
    r = out["register"]
    assert r["refused"] and "registration declined" in r["detail"], (
        f"apphost.register without a token on the claimed node: {r['detail']} — "
        "want the app register policy's refusal")
    assert not out["spec"]["refused"], f".spec without a token: {out['spec']['detail']}"
    for op in ("user.bootstrap", "user.derive_identity", "user.join",
               "apphost.list_tokens", "shell.spec"):
        r = out[op]
        assert denied(r) and r["detail"].startswith(f"Denied: {op}"), (
            f"{op} without a token on the claimed node: {r['detail']} — want denied")
    r = out["node list_tokens"]
    assert not r["refused"], (
        f"apphost.list_tokens with the node's token on the claimed node: {r['detail']} — "
        "the gate still holds the claimed node to the unclaimed list")
    r = out["node bootstrap"]
    assert r["refused"] and ALREADY_CLAIMED in r["detail"], (
        f"user.bootstrap with the node's token on the claimed node: {r['detail']}")

    print(f"oracle: manager {manager[:8]}… appointed and User {user[:8]}… claimed "
          "under the pinned words; fresh → unclaimed → claimed; "
          f"{len(facts['gate_probes'])} recorded denials hold; the claimed node "
          "refuses a second claim and a second manager, and keeps tokenless callers "
          "to .spec, user.info and app registration")


asyncio.run(main())
