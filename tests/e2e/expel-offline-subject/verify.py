#!/usr/bin/env python3
"""Oracle: a node expelled while it was down learns its ban when it comes back,
leaves the swarm, and its swarm follow stops reaching its former siblings.

The runner stopped node3, `while_down.py` had the User expel node3 on node1,
and node3 started again, still holding its contract. Claims:

- setup was live: before the restart, node1 and node2 each logged a
  services.discover with node3 (the follower's carries);
- node3 was restarted exactly once; node1 and node2 hold the ban on node3,
  issued by the User;
- node3 reads itself `unclaimed` within LEAVE seconds of the oracle's start
  (the ban reaches it at its first link with a holder: the expulsion sync at
  that link, the issuer's retried push, or the sweep), and its log holds the
  leave (`mod/user/src/expel.go`, leaveSwarm);
- from GRACE after node3 reads unclaimed, for HOLD seconds, node1 and node2
  log no services.discover with node3 and reject no contract pushed by node3,
  while the follower app on node3 still runs and reopens its follow.

note: a carry that node3 starts in the moments before its ban arrives is not
judged; only what follows the leave is.
"""
import asyncio
import time
from contextlib import AsyncExitStack

import astral

from lib import apps, nodefs
from lib.nodelog import LogTap
from lib.sessionio import load

LEAVE = 60.0       # node3 reaches a holder of its ban at its first link
HOLD = 15.0        # a carry retries on a backoff from 1 s (mod/services/src/swarm.go)
# why a grace: a push or a discovery already in flight when node3 leaves ends
# after it, and only a new one is a failure.
GRACE = 1.0
POLL = 0.5
LEFT = "leaving the swarm: this node is expelled"
PUSH_REJECTED = "rejecting pushed contract from "


async def banned_by(node: dict, subject: str) -> str | None:
    """The issuer of node's ban on subject, None when it holds none."""
    async with await astral.connect(node["endpoint"], token=node["token"]) as c:
        for e in await c.user.list_expelled():
            if e.expulsion is not None and str(e.expulsion.subject) == subject:
                return str(e.expulsion.issuer)
    return None


async def node_state(c) -> str:
    """node3's state as node3 reads it, `leaving` while it cannot say.

    why: between storing the ban and clearing its contract, node3 is still
    claimed but no longer a member, so user.info answers its own token "not
    permitted" (expel-propagation, Also observed).
    """
    try:
        return (await c.user.info()).state
    except astral.errors.RemoteError as e:
        if e.message != "not permitted":
            raise
        return "leaving"


async def until_left(node: dict) -> tuple[str, float]:
    """node3's settled state and the epoch time it was first read."""
    async with await astral.connect(node["endpoint"], token=node["token"]) as c:
        deadline = time.monotonic() + LEAVE
        state = await node_state(c)
        while state in ("claimed", "leaving") and time.monotonic() < deadline:
            await asyncio.sleep(POLL)
            state = await node_state(c)
    return state, time.time()


def crossing(entries: list, since: float, id3: str) -> list:
    """Entries at or after since that tell a discovery with node3 or a
    rejected push from node3."""
    return [e for e in entries if e["t"] >= since and (
        ("services.discover" in e["text"] and id3 in e["text"])
        or PUSH_REJECTED + id3 in e["text"])]


async def judge_after_leave(nodes: dict, id3: str, follower) -> dict:
    async with AsyncExitStack() as stack:
        taps = {n: await stack.enter_async_context(LogTap(nodes[n]))
                for n in ("node1", "node2")}
        before = follower.state().get("boundaries", 0)
        state, left_at = await until_left(nodes["node3"])
        assert state == "unclaimed", (
            f"node3 reads itself {state!r} {LEAVE:.0f} s after it came back; "
            "want unclaimed: it never learned its ban")
        await asyncio.sleep(max(0.0, left_at + GRACE + HOLD - time.time()))
        assert follower.alive(), (
            f"the follower on node3 exited:\n{follower.log_tail()}")
        for n, t in taps.items():
            s = t.state()
            assert s["refused"] is None and s["broken"] is None, (
                f"{n}'s log tap is incomplete: {s}")
            late = crossing(t.entries, left_at + GRACE, id3)
            assert not late, (
                f"{n} logged a discovery with or a rejected push from node3 "
                f"more than {GRACE} s after node3 left the swarm: "
                f"{[(round(e['t'] - left_at, 3), e['text'][:200]) for e in late]}")
        return {"boundaries_since_restart": follower.state().get("boundaries", 0) - before,
                "retries_on_node1": [e["text"][:160] for e in
                                     taps["node1"].find("signed_expulsion")]}


async def main():
    doc = load()
    nodes = {n: doc["nodes"][n] for n in ("node1", "node2", "node3")}
    facts = doc["facts"]["offline_subject"]
    user_id = doc["facts"]["user_id"]
    id3 = nodes["node3"]["identity"]

    assert all(facts["discover_before"].values()), (
        f"before the restart, node3's carries did not reach both members "
        f"({facts['discover_before']}); the checks below prove nothing")
    restarts = (nodes["node3"].get("restarts") or 0) - facts["node3_restarts_before"]
    assert restarts == 1, f"node3 was restarted {restarts} times, not once"
    for n in ("node1", "node2"):
        issuer = await banned_by(nodes[n], id3)
        assert issuer == user_id, (
            f"{n} holds a ban on node3 issued by {issuer}; want the User {user_id}")

    follower = apps.get(facts["follower"])
    try:
        seen = await judge_after_leave(nodes, id3, follower)
    finally:
        follower.stop()

    assert LEFT in nodefs.log(nodes["node3"]), (
        f"node3's log holds no {LEFT!r}: leaveSwarm never ran")
    print(f"oracle: node3 learned its ban after its restart and left; no "
          f"discovery or relay push from it reached node1 or node2 for {HOLD:.0f} s "
          f"after (follows reopened since the restart: "
          f"{seen['boundaries_since_restart']}; node1 ban push log: "
          f"{seen['retries_on_node1']})")


asyncio.run(main())
