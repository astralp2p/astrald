#!/usr/bin/env python3
"""Oracle: the ban lands while node2 reads, and node2 cannot read again.

Every check is the oracle's own read, never the driver's facts, except the
transfer record, which is printed and never judged:

- the read was a transfer in flight when the ban was issued: the driver took
  some bytes of the object, and fewer than all of them, before `user.expel`;
- node1 lists node2 as expelled by the User and drops it from its roster;
  the object is still held in node1's `local`;
- the ban reached node2: it settles as `unclaimed`, never `fresh`;
- a new read by node2 of the same object from node1 is refused by node1, as a
  query rejection (the see-objects authorizer excludes the expelled subject),
  not as an unreachable node and not at node2's own gate;
- control: the User still reads the object's first bytes from node1.

Open question, deliberately not asserted: whether the stream node2 opened
before the ban may finish. astral-docs (user-and-swarm.md, user.expel) are
silent on in-flight streams. astrald authorizes a query when it arrives and
never re-checks a running one (mod/user/src/authorizers.go); node2, on
receiving the ban, closes its links to its former siblings (expel.go
leaveSwarm), which carries the stream with them. The `transfer` fact says
which of the two happened in this run.
"""
import asyncio
import time

import astral

from lib.sessionio import load

# why a window: the ban reaches node2 by an asynchronous push (expel.go Expel,
# Objects.Push), so a state read the instant expel returns asks whether the
# push already landed, not whether it does.
CONVERGE = 30.0
POLL = 1.0


async def until(probe, want):
    deadline = time.monotonic() + CONVERGE
    got = await probe()
    while not want(got) and time.monotonic() < deadline:
        await asyncio.sleep(POLL)
        got = await probe()
    return got


async def refusal(coro) -> Exception | None:
    """The exception coro raised, or None when it answered."""
    try:
        await asyncio.wait_for(coro, 20)
    except Exception as e:  # noqa: BLE001 — the refusal is what is judged
        return e
    return None


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    facts = doc["facts"]
    user_id, user_token = facts["user_id"], facts["user_token"]
    object_id, transfer = facts["object_id"], facts["transfer"]
    id2 = n2["identity"]
    print(f"oracle: driver recorded the in-flight transfer {transfer}")

    assert facts["expelled_id"] == id2, (
        f"expelled_id {facts['expelled_id']} is not node2 {id2}")
    size, before = transfer["size"], transfer.get("before_ban")
    assert before is not None and 0 < before < size, (
        f"the ban was not issued mid-read: {before} of {size} B were read "
        "before it, so the test never raced a transfer")

    # --- node1: the issuer's side ---------------------------------------
    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        bans = {str(e.expulsion.subject): str(e.expulsion.issuer)
                for e in await c.user.list_expelled()
                if e.expulsion is not None and e.expulsion.subject is not None}
        roster = {str(m.identity) for m in await c.user.swarm_status()
                  if m.identity is not None}
        held = await c.objects.contains("local", object_id)
        head = await c.objects.read(object_id, offset=0, limit=16, repo="local")
    assert bans.get(id2) == user_id, (
        f"node1 holds no ban on node2 issued by the User {user_id}: {bans}")
    assert id2 not in roster and n1["identity"] in roster, (
        f"node1's roster {sorted(roster)} is not node1 without node2")
    assert held, f"node1's local repository lost the object {object_id}"
    assert len(head) == 16, f"control: the User read {len(head)} B, not 16"

    # --- node2: the banned node ------------------------------------------
    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        async def state():
            # why: while node2 leaves it holds the ban but not yet a cleared
            # contract, and user.info answers its own token "not permitted"
            # (expel-propagation's driver says more). Only the settled state
            # is judged.
            try:
                return (await c.user.info()).state
            except astral.errors.RemoteError as e:
                if e.message != "not permitted":
                    raise
                return "leaving"
        s2 = await until(state, lambda s: s not in ("claimed", "leaving"))
        assert s2 == "unclaimed", (
            f"node2 is {s2!r} {CONVERGE:.0f} s after the ban; an expelled "
            "node must be 'unclaimed' (contract cleared), never 'fresh'")

        read = await refusal(c.objects.read(object_id, limit=1, target=n1["identity"]))
    assert read is not None, (
        "node1 served node2 a new read of the object after the ban")
    assert isinstance(read, astral.QueryRejected), (
        f"node2's new read failed, but not as node1's rejection: {read!r}")

    ended = transfer.get("ended")
    print(f"oracle: User {user_id[:8]}… banned node2 {id2[:8]}… {before} of "
          f"{size} B into its read; node1 lists the ban and dropped node2; "
          f"node2 is unclaimed; node1 rejects its new read "
          f"({type(read).__name__})")
    print(f"oracle: note: the in-flight stream ended {ended!r} with "
          f"{transfer.get('received')} of {size} B, "
          f"{transfer.get('after_ban_s')} s after the ban (reported, not judged)")


asyncio.run(main())
