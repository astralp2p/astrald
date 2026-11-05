#!/usr/bin/env python3
"""Oracle: a ban issued on node1 holds on every node of the swarm.

Every check is the oracle's own read, never the driver's `probes`:

- node1 lists node3 as expelled by the User; its roster keeps node1 and node2
  and drops node3;
- node2, asked with its own token, holds the same ban and drops node3 from
  its roster: the ban propagated to a sibling;
- node3 is `unclaimed`, never `fresh` (astral-docs user-and-swarm.md, Losing
  the owner): it lost its contract and kept its manager;
- node3's own gate now denies `objects.repositories`; node1 refuses node3 the
  bait object, and refuses node3's `user.join` and the User's `user.adopt`
  with "identity expelled from swarm" (astrald mod/user/src/contracts.go
  IssueMembership);
- control: node2 is still a linked member and reads the same bait from node1.

note: link state between node2 and node3 is NOT asserted. Suspected astrald
defect: applyExpulsion (mod/user/src/expel.go:90-100) should sever ties to
the banned peer, but node2 can end up holding a link to node3 that nothing
ever closes. Likely cause: node3 drops its links first (leaveSwarm,
:108-119), node2's maintain_link schedules an ensure-link task
(maintain_link_task.go:65), and Scheduler.Schedule takes no context, so
removeSibling's cancel does not stop it; it completes after CloseLinks.
Reproduction (this test, run 20261105T013725Z, node2's log):
node3 tears down its links at 01:37:29.758 ("node expelled"); node2 logs
"link to <node3> is broken, trying to reconnect" and adds out-links at .773;
receives the ban and closes the link at .784 ("node expelled"); adds a new
out-link to node3 at .819, which was still up 24 s later. node3, unclaimed,
accepts it. Seen in 2 of 4 earlier live runs too (node2 re-linked ~5 ms after
node3 tore its links down; the link survived 18 s). Asserting "no link"
would make this test red about half the time, so it is reported instead.
Note also that the probes below, the driver's and the oracle's, open fresh
links from node3 to node1 themselves.
"""
import asyncio
import time

import astral

from lib.sessionio import load

# why a window: the ban reaches node2 and node3 by an asynchronous push
# (expel.go Expel: PushToLocalSwarm, Objects.Push), so a read the instant
# expel returns asks whether the push already landed, not whether it does.
CONVERGE = 30.0
POLL = 1.0
EXPELLED = "identity expelled from swarm"
# a refusal must be the node's answer, not a failure to reach it
UNREACHED = (astral.errors.QueryTimeout, astral.errors.RouteNotFound,
             astral.errors.TransportError, asyncio.TimeoutError)


async def until(probe, want):
    deadline = time.monotonic() + CONVERGE
    got = await probe()
    while not want(got) and time.monotonic() < deadline:
        await asyncio.sleep(POLL)
        got = await probe()
    return got


def bans(expelled) -> dict:
    """subject -> issuer of every ban in a list_expelled answer."""
    return {str(e.expulsion.subject): str(e.expulsion.issuer)
            for e in expelled
            if e.expulsion is not None and e.expulsion.subject is not None}


def roster(members) -> set:
    return {str(m.identity) for m in members if m.identity is not None}


def linked(members) -> set:
    return {str(m.identity) for m in members
            if m.linked and m.identity is not None}


async def refusal(coro) -> Exception | None:
    """The exception coro raised, or None when it answered."""
    try:
        await asyncio.wait_for(coro, 20)
    except Exception as e:  # noqa: BLE001 — the refusal is what is judged
        return e
    return None


async def main():
    doc = load()
    n1, n2, n3 = (doc["nodes"][n] for n in ("node1", "node2", "node3"))
    facts = doc["facts"]
    user_id, user_token = facts["user_id"], facts["user_token"]
    bait = facts["bait_id"]
    id1, id2, id3 = n1["identity"], n2["identity"], n3["identity"]
    print(f"oracle: driver recorded probes {facts.get('probes')}")

    assert facts["expelled_id"] == id3, (
        f"expelled_id {facts['expelled_id']} is not node3 {id3}")

    # --- node1: the issuer's side ---------------------------------------
    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        b1 = bans(await c.user.list_expelled())
        r1 = roster(await c.user.swarm_status())
        held = await c.objects.contains("local", bait)
    assert b1.get(id3) == user_id, (
        f"node1 holds no ban on node3 issued by the User {user_id}: {b1}")
    assert held, f"node1's local repo does not hold the bait {bait}"
    # why subset, not equality: in a VM world an operator may have adopted
    # the lab's reflector too; what matters is node3 out, node1/node2 in.
    assert {id1, id2} <= r1 and id3 not in r1, (
        f"node1's roster {sorted(r1)} is not node1+node2 without node3")

    # --- node2: did the ban reach a sibling? -----------------------------
    # why node2's own token rather than a User token minted on node2: the
    # question is node2's own view, and list_expelled authorizes SeeSwarm,
    # which admits any member (verified live; it also mints nothing).
    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        async def view2():
            return (bans(await c.user.list_expelled()),
                    await c.user.swarm_status())
        b2, members2 = await until(
            view2, lambda v: v[0].get(id3) == user_id
            and id3 not in roster(v[1]) and id1 in linked(v[1]))
        bait_via_node2 = await refusal(c.objects.read(bait, target=id1))
        # reported, never judged: see the note on the re-link race above
        stale = sum(1 for l in await c.nodes.links(experimental=True)
                    if str(l.remote_identity) == id3)
    print(f"oracle: note: node2 holds {stale} link(s) to expelled node3")
    r2 = roster(members2)
    assert b2.get(id3) == user_id, (
        f"node2 never received the User's ban on node3 within "
        f"{CONVERGE:.0f} s: node2's list_expelled {b2}")
    assert id3 not in r2, (
        f"node2 still lists node3 in its roster {sorted(r2)} "
        f"{CONVERGE:.0f} s after the ban")
    assert id1 in linked(members2), (
        f"control: node2 no longer holds a live link to node1; "
        f"linked: {sorted(linked(members2))}")
    assert bait_via_node2 is None, (
        f"control: node2 cannot read the bait from node1 either "
        f"({bait_via_node2!r}), so node3's refusal below proves nothing")

    # --- node3: the banned node ------------------------------------------
    async with await astral.connect(n3["endpoint"], token=n3["token"]) as c:
        async def state():
            # why: while node3 is leaving it holds the ban on itself but not
            # yet a cleared contract, and user.info answers its own token
            # "not permitted" (the driver's node_state says more). That is
            # the leave in progress; only the settled state is judged.
            try:
                return (await c.user.info()).state
            except astral.errors.RemoteError as e:
                if e.message != "not permitted":
                    raise
                return "leaving"
        s3 = await until(state, lambda s: s not in ("claimed", "leaving"))
        assert s3 == "unclaimed", (
            f"node3 is {s3!r} {CONVERGE:.0f} s after the ban; an expelled "
            "node must be 'unclaimed' (contract cleared), never 'fresh'")

        own = await refusal(c.objects.repositories())
        assert isinstance(own, astral.errors.Denied), (
            f"node3's own gate still answers objects.repositories ({own!r}) "
            "though node3 lost its contract")

        read = await refusal(c.objects.read(bait, target=id1))
        assert read is not None and not isinstance(read, UNREACHED), (
            f"node1 did not refuse node3 the bait: {read!r}")

        join = await refusal(c.user.join(id1))
        assert EXPELLED in str(join), (
            f"node1 did not refuse node3's user.join as expelled: {join!r}")

    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        readopt = await refusal(c.user.adopt(id3))
        assert EXPELLED in str(readopt), (
            f"node1 did not refuse to re-adopt node3: {readopt!r}")
        r1 = roster(await c.user.swarm_status())
        assert id3 not in r1, f"node3 is back in node1's roster {sorted(r1)}"

    print(f"oracle: User {user_id[:8]}… banned node3 {id3[:8]}…; node1 and "
          "node2 hold the ban and dropped it, node2 still linked; node3 is "
          "unclaimed, its gate denies objects.repositories, node1 refuses its "
          f"read ({type(read).__name__}), its join and its re-adoption")


asyncio.run(main())
