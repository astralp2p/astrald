#!/usr/bin/env python3
"""Oracle: dead kcp and gw rows are dialed, bounded, and do not keep node2 unlinked.

Observed by the driver, judged here (fact `dead_rows`, times on the host's
clock):

- beside: the round answered a tcp link within BESIDE s; the dead kcp row
  neither won nor held the round back;
- stranger: the round answered an error, no link, after at least DIALED s
  and at most KCP_LIMIT s. A round that never dials the kcp row answers at
  once; one that dials it and has no bound waits for the caller's timeout.
  basic bounds a kcp row to 5 s (mod/nodes/src/transport_profile.go
  transportProfiles);
- crowd: with 10 dead rows beside the live tcp row, the round still answered
  a tcp link, within CROWD_LIMIT s. The time is the measurement the ticket
  asks for, and the oracle prints it.
"""
from lib.sessionio import load

BESIDE = 2.0
DIALED = 4.0
KCP_LIMIT = 10.0
CROWD_LIMIT = 30.0
DEAD_ROWS = 10


def linked_tcp(r: dict, limit: float, phase: str) -> None:
    assert "error" not in r, f"{phase}: the round answered {r['error']}, not a link"
    assert r["network"] == "tcp", (
        f"{phase}: the round answered a {r['network']} link; node2's only live row is tcp")
    assert r["elapsed"] < limit, (
        f"{phase}: the round answered after {r['elapsed']} s, not under {limit} s")


def main():
    doc = load()
    n2 = doc["nodes"]["node2"]
    f = doc["facts"]["dead_rows"]
    print(f"oracle: beside {f['beside']}; stranger {f['stranger_round']}; crowd {f['crowd']}")

    linked_tcp(f["beside"], BESIDE, "beside")

    s = f["stranger_round"]
    assert "error" in s, f"stranger: the round answered a link {s} from a dead kcp row"
    assert DIALED <= s["elapsed"] <= KCP_LIMIT, (
        f"stranger: the round answered after {s['elapsed']} s; want {DIALED}..{KCP_LIMIT} s: "
        "a round that skips the kcp row answers at once, an unbounded one waits for the caller")

    added = f["rows"][n2["identity"]]
    assert len(added) == DEAD_ROWS, f"the driver added {len(added)} dead rows to node2, not {DEAD_ROWS}"
    linked_tcp(f["crowd"], CROWD_LIMIT, "crowd")

    print(f"oracle: a dead kcp row beside tcp linked in {f['beside']['elapsed']} s; "
          f"a kcp-only stranger gave up after {s['elapsed']} s; "
          f"{DEAD_ROWS} dead rows beside tcp linked in {f['crowd']['elapsed']} s")


main()
