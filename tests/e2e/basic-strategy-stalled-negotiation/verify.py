#!/usr/bin/env python3
"""Oracle: a peer stalled in mux negotiation neither holds back a round that
has a winner nor blocks the next round, and node1 lets go of it.

Observed by the driver, judged here (fact `stalled_negotiation`; every time
is the host's clock, which the fake peer and the driver share):

- round 1 answered a link within 5 s; a round held by the worker stuck in
  mux negotiation hangs until the caller's 15 s timeout;
- round 2 answered a link within 5 s.

The fake peer's own record, its final state file (lib/apps.py):

- during round 1, node1 completed the Noise handshake with `stall`: the
  stall sat in mux negotiation, after the handshake. No such connection
  means the stall was never exercised;
- in each round that answered, a live listener negotiated while a `stall`
  connection sat open after its handshake: the winner came while a sibling
  was stuck in mux negotiation;
- during round 2, `live3` accepted a connection: the second round dialed.
  A round still running makes the next Signal a no-op, and `live3` hears
  nothing;
- node1 closed every `stall` connection no later than 5 s after the round
  that opened it answered.

Independent read, with node1's own token, once node1's links to the fake
peer stay put for 5 polls: node1 holds exactly the two returned ids.
The oracle then stops the fake peer and judges the state it writes on exit.
"""
import asyncio

import astral

from lib import apps, linkview
from lib.sessionio import load

PROMPT = 5.0
RELEASE = 5.0
SETTLE = 20.0
STOP_GRACE = 30.0


def within(r: dict, t: float) -> bool:
    return r["start"] <= t <= r["end"]


def judge_rounds(rounds: list) -> None:
    assert len(rounds) == 2, f"the driver ran {len(rounds)} rounds, not 2"
    why = {1: "the worker stuck in mux negotiation with the stalled peer "
              "held back a round that had a winner",
           2: "the strategy did not run a later round"}
    for r in rounds:
        assert "error" not in r and r["elapsed"] <= PROMPT, (
            f"round {r['round']} answered {r.get('error') or r['id']} after "
            f"{r['elapsed']} s, not a link within {PROMPT:.0f} s: "
            f"{why[r['round']]}")
    assert rounds[0]["id"] != rounds[1]["id"], (
        f"both rounds returned the link {rounds[0]['id']}")


def judge_peer(rounds: list, conns: list) -> None:
    r1, r2 = rounds
    stalled = [c for c in conns if c["listener"] == "stall"
               and within(r1, c["accepted_at"]) and c["handshaked_at"]]
    assert stalled, (
        "node1 completed no Noise handshake with the stall listener during "
        f"round 1, so no worker sat in mux negotiation: {conns}")
    for r in rounds:
        won = [c for c in conns if c["listener"] != "stall"
               and within(r, c["accepted_at"]) and c["negotiated_at"]
               and c["stall_seen"]]
        assert won or "error" in r, (
            f"no live connection of round {r['round']} negotiated while a "
            f"stall connection sat open after its handshake: {conns}")
    live3 = [c for c in conns if c["listener"] == "live3"
             and within(r2, c["accepted_at"])]
    assert live3, (
        "live3 accepted no connection during round 2: the second round never "
        f"dialed (round 1 still running?): {conns}")
    for c in conns:
        if c["listener"] != "stall":
            continue
        owner = next((r for r in rounds if within(r, c["accepted_at"])), None)
        assert owner is not None, f"node1 dialed stall outside both rounds: {c}"
        assert c["closed_at"] is not None, (
            f"node1 still holds the stall connection of round "
            f"{owner['round']} open: {c}")
        late = c["closed_at"] - owner["end"]
        assert late <= RELEASE, (
            f"node1 closed the stall connection of round {owner['round']} "
            f"{late:.1f} s after the round answered, more than "
            f"{RELEASE:.0f} s: {c}")


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    fact = doc["facts"]["stalled_negotiation"]
    rounds, x = fact["rounds"], fact["peer"]
    print(f"oracle: rounds {rounds}")
    judge_rounds(rounds)
    peer = apps.get(fact["app"])
    try:
        async with await astral.connect(n1["endpoint"],
                                        token=n1["token"]) as c:
            async def probe():
                return sorted(linkview.ids(await linkview.links(c), x))
            held, settled = await linkview.settle(probe, timeout=SETTLE)
    finally:
        # why stop first: the state file can lag on a loaded disk, and the
        # fake peer writes its complete state when it stops
        peer.stop(grace=STOP_GRACE)
    st = peer.state()
    conns = st.get("conns", [])
    print(f"oracle: fake peer connections {conns}")
    assert st.get("final"), (
        f"the fake peer did not write its final state within {STOP_GRACE:.0f} "
        f"s of SIGTERM; last state {st}")
    judge_peer(rounds, conns)
    want = sorted(r["id"] for r in rounds)
    assert settled and held == want, (
        f"node1 holds {held} to the fake peer (settled {settled}), not the "
        f"two returned ids {want}")
    print(f"oracle: both rounds answered within "
          f"{max(r['elapsed'] for r in rounds):.3f} s; node1 released the "
          "stalled negotiation and holds the two winners")


asyncio.run(main())
