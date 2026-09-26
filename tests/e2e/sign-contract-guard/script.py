#!/usr/bin/env python3
"""Driver: auth.sign_contract signs only as parties the caller may act as.

The op signs a contract as its issuer and as its subject with keys node1
holds. Each party passes the signer rule crypto.sign_hash applies: the
caller's own key, never the node's key as the node itself, or a key the caller
may sudo to. A query from a peer is rejected before any of that.

The driver acts and judges nothing. On node1 it registers an app for the
User and asks for seven contracts: the User's own (the control), one naming
the app as subject for the User, two the app asks in the User's name as
issuer and as subject, the claim contract asked by node1's own token on the
claimed node, and one in node1's own name asked by an anonymous session. Over
the link it asks node1 for the User's own contract as the User, from node2.

Last, as node1's token, it clears node1's claim from the tree, asks for a
stranger's claim (the control) and the User's claim, and puts the claim back.

why the claim is put back: expel-node runs on this two-nodes state next. The
driver restores the tree value and the nearby mode that clearing it changes,
and waits until node1 answers as the User's node again.
"""
import asyncio

import astral
from astral.api.auth import Contract, Permit
from astral.errors import AstralError
from astral.object import Nil
from astral.types import Identity, Time

from lib.sessionio import load, write_facts

# why a deadline: a query that is never routed hangs rather than answering, and
# the client's default is 60 s. apphost-origin settled on 15 for the same reason.
DEADLINE = 15
SEE = "mod.user.see_swarm_action"
SUDO = "mod.auth.sudo_action"
ACTIVE = "/mod/user/config/active_contract"
MODE = "/mod/nearby/mode"
# why the generator point: a valid identity whose private key (1) no node stores.
STRANGER = "0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"


def contract(issuer, subject, action) -> Contract:
    return Contract(issuer=issuer, subject=subject,
                    permits=[Permit(action=action, constraints=None, delegation=0)],
                    expires_at=Time(Time.now() + 3600 * 10**9))


async def attempt(coro) -> dict:
    """Run one probe; report whether it was refused, and how it read."""
    try:
        signed = await coro
    except AstralError as e:
        return {"refused": True, "detail": f"{type(e).__name__}: {e}"}
    return {"refused": False, "detail": "signed",
            "both_signed": signed.issuer_sig is not None and signed.subject_sig is not None}


async def claimed(c) -> bool:
    """Whether node1 answers user.info; it rejects with code 2 when unclaimed."""
    try:
        await c.user.info()
    except AstralError as e:
        if "code 2" in str(e):
            return False
        raise
    return True


async def settle(c, want: bool) -> None:
    for _ in range(50):
        if await claimed(c) == want:
            return
        await asyncio.sleep(0.1)
    raise SystemExit(f"driver: node1 claimed={not want} 5 s after the tree write")


async def settle_mode(c, path: str, before) -> None:
    """Wait until the nearby mode moves off its value before the clear.

    why: clearing the claim sets the nearby mode from a goroutine, which must
    not land after the driver puts the old mode back. A mode that was already
    the one clearing sets never moves, so the wait ends after 1 s regardless.
    """
    for _ in range(10):
        if repr(await c.tree.get(path)) != repr(before):
            return
        await asyncio.sleep(0.1)


async def bound_path(c, path: str) -> str:
    """The path a module's value is bound at: `path`, or `path` without /mod.

    why: astral-go tree.Query (api/tree/node.go, the ErrAlreadyExists case)
    stays at the parent when another module creates the same node first, so a
    module that loses that race at startup binds /user/config and not
    /mod/user/config.
    """
    for candidate in (path, path.removeprefix("/mod")):
        try:
            await c.tree.get(candidate)
        except AstralError as e:
            if "not found" not in str(e):
                raise
            continue
        return candidate
    raise SystemExit(f"driver: node1 binds {path} neither under /mod nor at the root")


async def cleared_claim(n1, claim) -> dict:
    """Clear node1's claim from the tree, ask for two claims, put it back."""
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as n:
        active_path = await bound_path(n, ACTIVE)
        mode_path = await bound_path(n, MODE)
        active, mode = await n.tree.get(active_path), await n.tree.get(mode_path)
        await n.tree.set(active_path, Nil())
        try:
            await settle(n, False)
            await settle_mode(n, mode_path, mode)
            stranger = await n.user.new_node_contract(user=STRANGER)
            probes = {
                "stranger's claim, claim cleared": await attempt(n.auth.sign_contract(stranger)),
                "user's claim, claim cleared": await attempt(n.auth.sign_contract(claim)),
            }
        finally:
            await n.tree.set(active_path, active)
            await settle(n, True)
            await n.tree.set(mode_path, mode)
        probes["restored"] = str((await n.user.info()).contract.contract.issuer)
    return probes


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    facts = doc["facts"]
    user = Identity.from_json(facts["user_id"])
    node1 = Identity.from_json(n1["identity"])

    probes = {}
    async with await astral.connect(n1["endpoint"], token=facts["user_token"]) as u:
        app = await u.apphost.register()
        claim = await u.user.new_node_contract(user=str(user))
        control = await attempt(u.auth.sign_contract(contract(user, user, SEE)))
        probes["user names the app as subject"] = await attempt(
            u.auth.sign_contract(contract(user, app.identity, SEE)))

    async with await astral.connect(n1["endpoint"], token=str(app.token)) as a:
        probes["app forges user→app sudo"] = await attempt(
            a.auth.sign_contract(contract(user, app.identity, SUDO)))
        probes["app names the user as subject"] = await attempt(
            a.auth.sign_contract(contract(app.identity, user, SUDO)))

    async with await astral.connect(n1["endpoint"], token=n1["token"]) as n:
        probes["node token asks the claim on a claimed node"] = await attempt(
            n.auth.sign_contract(claim))

    async with await astral.connect(n1["endpoint"]) as anon:
        probes["anonymous asks the node key"] = await attempt(
            anon.auth.sign_contract(contract(node1, app.identity, SUDO)))

    # why the User over the link: the User signs this very contract on node1
    # (the control), so the origin is the only difference.
    # why node2 mints its own User token: user_token authenticates on node1 only.
    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c2:
        sibling_user = await c2.apphost.create_token(facts["user_id"])
    async with await astral.connect(n2["endpoint"], token=sibling_user.token) as c2:
        probes["the user over the link"] = await attempt(c2.auth.sign_contract(
            contract(user, user, SEE), target=n1["identity"], timeout=DEADLINE))

    cleared = await cleared_claim(n1, claim)

    write_facts({"sign_guard": {"control": control, "probes": probes,
                                "cleared": cleared, "app_token": str(app.token)}})
    shape = ", ".join(f"{k}: {'refused' if v['refused'] else 'SIGNED'}"
                      for k, v in probes.items())
    print(f"driver: control {control['detail']}; {shape}; claim cleared: "
          + ", ".join(f"{k}: {v['detail'][:60]}" for k, v in cleared.items()
                      if k != "restored"))


asyncio.run(main())
