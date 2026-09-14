#!/usr/bin/env python3
"""Oracle: the User reaches the token ops on node1, and a sibling does not.

Three judgements, and the order matters. The control first: if the User cannot
reach these ops locally either, the refusals say nothing about the guard and
this test has proven nothing. Then the refusals, checked for being refusals — a
timeout, a lost route or an unreachable node all leave the caller with no
result, and a test accepting any failure would stay green if the link simply
broke.

Last, what the node holds. Whether the call was refused and whether anything was
minted are two claims, and the driver is the wrong party for the second. The
oracle asks node1 for the tokens it holds for the probed identity itself.

The difference between the control and the probes is the origin alone: the same
User identity, the same two ops, the same node. That is what makes this a test
of the origin refusal rather than of the authorization check, which the User
satisfies either way.
"""
import asyncio

import astral

from lib.sessionio import load

# What a refusal must not read like — each means the call never got a verdict.
NOT_A_REFUSAL = ("timeout", "timed out", "route_not_found", "routenotfound",
                 "nodeunavailable", "unknown target", "connection refused")


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    facts = doc["facts"]
    probe_id = facts["probe_identity"]

    assert facts["control_minted"], (
        "apphost.create_token did not answer the User on node1 — the op is "
        "unreachable for everyone, so the refusals below are not evidence "
        "about the guard")
    assert facts["control_list_count"] > 0, (
        f"apphost.list_tokens answered the User {facts['control_list_count']} "
        "tokens on node1 — the op is not working locally, so a refusal over "
        "the link proves nothing")

    for op, r in facts["refusals"].items():
        assert r["refused"], (
            f"{op} answered a sibling relaying as the User: {r['detail']} — "
            "the op is reachable from the network, and a swarm node mints and "
            "reads credentials on its peer")

        detail = r["detail"].lower()
        assert not any(m in detail for m in NOT_A_REFUSAL), (
            f"{op} failed without being refused: {r['detail']} — the query did "
            "not reach the guard, so its failure proves nothing")
        assert "rejected" in detail, (
            f"{op} failed with {r['detail']}, which names no refusal — a "
            "refusal the caller cannot read is indistinguishable from a fault")

    # The oracle measures node1 rather than taking the driver's word for it.
    async with await astral.connect(n1["endpoint"],
                                    token=facts["user_token"]) as c1:
        for_probe = await c1.apphost.list_tokens(probe_id)

    assert not for_probe, (
        f"node1 holds {len(for_probe)} token(s) for {probe_id[:16]}… — the "
        "query was refused and a token was minted anyway")

    ops = ", ".join(facts["refusals"])
    print(f"oracle: node1 answers the User locally ({facts['control_list_count']} "
          f"tokens) and refused {ops} to a sibling relaying as that same User; "
          f"no token exists for {probe_id[:16]}…")


asyncio.run(main())
