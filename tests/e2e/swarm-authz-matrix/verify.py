#!/usr/bin/env python3
"""Oracle: a linked stranger reaches only the open ops on node1, a sibling only
the swarm's reads, and node1 holds nothing either of them tried to write.

Three judgements. First the contract: for each asker, the set of probes node1
answered is exactly the set astral-docs topics/node-exposure.md and
mod/user/src/authorizers.go grant it, and every other probe reads as a refusal,
never as a timeout or a lost route.

Then node1's state, measured by the oracle as the User and never taken from the
driver. Every write a probe attempted is absent: the bait is still there and
still the bait, the roster and the expulsion list are as adopt-node left them,
the alias, the tree path and the intruder object do not exist, no token names
the stranger, and no policy delegate names it. This is the only verdict for the
three body-carrying writes that come back as a reset (see RESET_OK).

Last, three probes of the oracle's own, so a driver that probed nothing, or
probed the wrong node, does not pass: the stranger is refused the bait, the
sibling reads it, and node1 sees the stranger as the stranger.
"""
import asyncio

import astral
from astral.errors import AstralError
from astral.object import Query
from astral.objectid import object_id_of, object_id_of_bytes

from lib.sessionio import load

DEADLINE = 15

# The values the driver's probes wrote; the oracle names them itself.
INTRUDER = Query(query_string="swarm-authz-matrix intruder")
TREE_PATH = "/swarm-authz-matrix"
ALIAS = "hijack"
POLICY_OP = "swarm_authz_matrix.decide"
POLICIES = ("/mod/user/policy/swarm_join", "/mod/apphost/policy/app_register")

CONTROLS = {"apphost.whoami", "dir.resolve", "shell.spec"}
SWARM_READS = {"user.info", "user.swarm_status", "user.list_siblings",
               "objects.read", "objects.contains", "objects.load",
               "objects.repositories", "nodes.links"}
GUARDED = {"tree.list", "apphost.list_tokens", "objects.store", "tree.set",
          "dir.set_alias", "apphost.create_token", "apphost.hold_object",
          "user.adopt", "user.expel", "user.set_swarm_join_policy",
          "apphost.set_app_register_policy", "crypto.sign_text",
          "objects.delete"}
PROBES = CONTROLS | SWARM_READS | GUARDED

# The contract under test: what node1 answers each asker over the link.
# why the stranger's own User is granted nothing, the controls included: node1
# admits a relay only from a swarm node relaying for node1's own User
# (AuthorizeRelayFor), so it rejects the query before any op sees it.
# why the sibling gets the swarm's reads and no write: SeeSwarm, SeeObjects and
# AdminNetwork grant the local swarm; StoreObjects, AdminObjects, AdminSwarm
# and the apphost and policy setters do not (mod/user/src/authorizers.go).
ALLOWED = {
    "stranger_node": CONTROLS,
    "stranger_user": set(),
    "sibling": CONTROLS | SWARM_READS,
}

# note: these three carry a body, and a refusal over a link loses its text:
# node1 refuses before it reads the body, the relay resets the stream on the
# body already sent, and the caller reads a reset or an end with no answer —
# ConnectionResetError, or a ProtocolError whose wording depends on the race
# and the op. That reads like a fault, not a refusal, so the oracle accepts a
# reset for these three only, never reads its text, and judges them by node1's
# state below: no intruder object, no tree value, and crypto.sign_text changes
# nothing a caller could keep but the signature no asker got.
RESET_OK = {"objects.store", "tree.set", "crypto.sign_text"}
RESET_TYPES = ("ConnectionResetError:", "ProtocolError:")

# What a refusal reads like, per asker: the op's own refusal, or node1
# rejecting the query before any op ran.
REFUSAL = {
    "stranger_node": ("not permitted", "origin not allowed: network",
                      "query rejected with code 1"),
    "stranger_user": ("query rejected with code 1",),
    "sibling": ("not permitted", "origin not allowed: network"),
}
# What a refusal must not read like — each means the call never got a verdict.
NOT_A_REFUSAL = ("timeout", "timed out", "route_not_found", "routenotfound",
                 "nodeunavailable", "unknown target", "connection refused")


def check_matrix(matrix: dict, ids: dict) -> None:
    assert set(matrix) == set(ALLOWED), f"askers probed: {sorted(matrix)}"
    for asker, row in matrix.items():
        assert set(row) == PROBES, (
            f"{asker}: probes ran {sorted(row)}, missing "
            f"{sorted(PROBES - set(row))}")
        allowed = {p for p, r in row.items() if r["outcome"] == "allowed"}
        assert allowed == ALLOWED[asker], (
            f"{asker}: node1 answered {sorted(allowed)}; the contract grants "
            f"{sorted(ALLOWED[asker])} — granted beyond it "
            f"{sorted(allowed - ALLOWED[asker])}, refused within it "
            f"{sorted(ALLOWED[asker] - allowed)}")
        for probe, r in row.items():
            if r["outcome"] == "allowed":
                continue
            detail = r["detail"].lower()
            assert not any(m in detail for m in NOT_A_REFUSAL), (
                f"{asker} {probe}: failed without a verdict: {r['detail']}")
            if r["outcome"] == "reset":
                assert probe in RESET_OK, (
                    f"{asker} {probe}: reset the connection, and only "
                    f"{sorted(RESET_OK)} may — its refusal is lost: {r['detail']}")
                assert r["detail"].startswith(RESET_TYPES), (
                    f"{asker} {probe}: recorded reset, reads {r['detail']}")
                continue
            assert r["outcome"] == "refused", (
                f"{asker} {probe}: {r['outcome']}: {r['detail']}")
            assert any(m in detail for m in REFUSAL[asker]), (
                f"{asker} {probe}: {r['detail']} is not a refusal "
                f"{REFUSAL[asker]}")

    # why the answers: a control that answered for another node, or a whoami
    # that names the wrong caller, would make every refusal above say nothing
    # about node1's view of this asker.
    for asker, me in (("stranger_node", ids["stranger"]), ("sibling", ids["node2"])):
        row = matrix[asker]
        assert row["apphost.whoami"]["answer"] == me, (
            f"{asker}: node1 sees it as {row['apphost.whoami']['answer']}, not {me}")
        assert row["dir.resolve"]["answer"] == ids["node1"], (
            f"{asker}: localnode resolved to {row['dir.resolve']['answer']}, "
            f"not node1 — the probes reached another node")


async def absent(coro, mark: str) -> str:
    """The error a lookup of something that must not exist answered."""
    try:
        v = await coro
    except AstralError as e:
        assert mark in str(e), f"lookup failed for another reason: {e}"
        return str(e)
    raise AssertionError(f"exists: {v!r}")


async def bound_path(u, path: str):
    """The value at `path` or at `path` without /mod; `None` when neither binds.

    why: astral-go tree.Query stays at the parent when another module creates
    the same node first, so a module that loses that startup race binds
    /user/... rather than /mod/user/... (sign-contract-guard's driver).
    """
    for candidate in (path, path.removeprefix("/mod")):
        try:
            return await u.tree.get(candidate)
        except AstralError as e:
            if "not found" not in str(e):
                raise
    return None


async def check_state(u, bait: str, ids: dict) -> bytes:
    held = await u.objects.read(bait, repo="local")
    assert str(object_id_of_bytes(bytes(held))) == bait, (
        f"node1's copy of the bait {bait[:20]}… no longer hashes to it")

    roster = {str(m.identity) for m in await u.user.swarm_status()}
    assert roster == {ids["node1"], ids["node2"]}, (
        f"node1's roster is {sorted(roster)}, not node1 and node2 — "
        f"stranger in it: {ids['stranger'] in roster}")
    expelled = await u.user.list_expelled()
    assert not expelled, f"node1 lists {len(expelled)} expulsion(s): {expelled}"

    await absent(u.dir.resolve(ALIAS), "unknown identity")
    await absent(u.tree.get(TREE_PATH), "not found")
    intruder = str(object_id_of(INTRUDER))
    assert not await u.objects.contains("local", intruder), (
        f"node1 holds the intruder object {intruder[:20]}… a probe stored")

    tokens = await u.apphost.list_tokens()
    named = [t for t in tokens if str(t.identity) == ids["stranger"]]
    assert not named, f"node1 holds {len(named)} token(s) for the stranger"

    # why "names the stranger" and not "is unset": policy-delegate runs before
    # adopt-node in the shared world and may leave its own app_register
    # delegate, so unset is not the state this test found. The harm the probes
    # attempted is a delegate naming the stranger's op.
    for path in POLICIES:
        d = await bound_path(u, path)
        who = str(getattr(d, "identity", None))
        op = str(getattr(d, "operation", ""))
        assert who != ids["stranger"] and op != POLICY_OP, (
            f"{path} delegates to {who}:{op} — a probe set node1's policy")
    return bytes(held)


async def own_probes(nodes: dict, bait: str, held: bytes) -> str:
    """The oracle's own three probes; answers who owns the stranger."""
    n1, n2, s = nodes["node1"], nodes["node2"], nodes["stranger"]
    t = {"target": n1["identity"]}
    async with await astral.connect(s["endpoint"], token=s["token"]) as c:
        who = await asyncio.wait_for(c.apphost.whoami(**t), DEADLINE)
        assert str(who) == s["identity"], (
            f"node1 sees the stranger as {who}, not {s['identity']}")
        try:
            got = await asyncio.wait_for(c.objects.read(bait, **t), DEADLINE)
        except AstralError as e:
            assert "query rejected" in str(e).lower() or "not permitted" in str(e), (
                f"the stranger's read of the bait failed without a refusal: {e}")
        else:
            raise AssertionError(f"node1 served the bait to the stranger: {got!r}")
    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        got = await asyncio.wait_for(c.objects.read(bait, **t), DEADLINE)
        assert bytes(got) == held, (
            f"the sibling read {got!r} from node1, not the bait {held!r}")

    # why: the stranger's own User must be another User, or "stranger_user"
    # probed as a member of this swarm.
    async with await astral.connect(s["endpoint"], token=s["token"]) as c:
        info = await c.user.info()
    owner = str(info.contract.contract.issuer) if info.contract else None
    assert info.state == "claimed" and owner, (
        f"the stranger is {info.state}, owned by {owner}")
    return owner


async def main():
    doc = load()
    nodes, facts = doc["nodes"], doc["facts"]
    ids = {n: nodes[n]["identity"] for n in ("node1", "node2", "stranger")}
    bait = facts["bait_id"]

    check_matrix(facts["matrix"], ids)

    async with await astral.connect(nodes["node1"]["endpoint"],
                                    token=facts["user_token"]) as u:
        held = await check_state(u, bait, ids)

    owner = await own_probes(nodes, bait, held)
    resets = sum(r["outcome"] == "reset"
                 for row in facts["matrix"].values() for r in row.values())
    assert owner != facts["user_id"], (
        "the stranger is claimed by this swarm's User, not a stranger's")

    print(f"oracle: node1 answered the stranger {sorted(ALLOWED['stranger_node'])}, "
          f"the stranger's User nothing, and the sibling "
          f"{len(ALLOWED['sibling'])} probes; node1 holds nothing the "
          f"{len(GUARDED)} guarded probes tried to write, {resets} resets "
          "judged by that state; the oracle's own probes agree")


asyncio.run(main())
