#!/usr/bin/env python3
"""Oracle: two siblings reach each other over Tor when the LAN goes, again after Tor restarts, and over tcp when the LAN returns.

From the `fallback_paths` fact (times on the host's clock, from the driver's
start; the driver started after lan-outage had cut node2 off the LAN):

- phase A, LAN gone: node2 first answered node1 within PHASE_A s, with node1
  holding only tor links to node2. tcp's link_idle_ping + link_ping_timeout
  + one 60 s query + 2 s, as tor-link's oracle (soYSZDACSo);
- phase B, Tor restarts on node2: node2 resolved no tor row for itself from
  the down line until Tor's start (`down_seen` + --down, less START_SLACK
  for the tap's delay); queries in both directions answered over a tor link
  again within PHASE_B s of Tor's start, and node2's address returned no
  sooner than PHASE_B s after it (else tcp carries the queries and Tor's
  recovery is not judged); node2's log tap stayed whole, so
  astrald did not restart (a restart ends the stream, lib/nodelog.py);
- phase C, LAN back: node1 and node2 share a tcp link within PHASE_C s of the
  driver seeing node2's address return; every query in either direction
  started RECOVERED s after that link answered; node1 listed no tor link to
  node2 within RETIRE s of the tcp link;
- no query outlived its QUERY s deadline by more than SLACK s.

The spec is silent on the onion's withdrawal (mod/tor/src/server.go,
endpoint_resolver.go, commit 8535c9cb) and on the retire of a lower-ranked
link after an upgrade (mod/nodes/src/connectivity_upgrade.go, retireMax,
commit 0da99afa); those bounds follow the code.
"""
from lib.sessionio import load

# why: tcp's defaults, 15 s idle ping and 10 s ping timeout
# (mod/tcp/src/config.go), one 60 s query and 2 s (tor-link's bound)
PHASE_A = 15.0 + 10.0 + 60.0 + 2.0
START_SLACK = 2.0
# why 180 s: at most 60 s of re-register backoff (mod/tor/src/server.go
# retryMax), tor's link_idle_ping + link_ping_timeout of 60 s (astral-docs
# topics/link-mux.md, Keepalive, Per-transport values), and 60 s for a dial
PHASE_B = 60.0 + 60.0 + 60.0
# why 90 s: the 5 s announce wait and one 20 s pull (astral-docs
# protocols/nodes/README.md, Announcing a change), the user module's wake on
# an address change (AOcdNCbukM), and a dial
PHASE_C = 90.0
RECOVERED = 1.0
# why 130 s: retireMax of 2 min (mod/nodes/src/connectivity_upgrade.go) and
# 10 s; tcp ranks 50 over tor's 10 (astral-docs protocols/nodes/README.md)
RETIRE = 130.0
QUERY = 30.0
SLACK = 2.0
DIRECTIONS = ("node1_to_node2", "node2_to_node1")


def check_phase_a(f: dict) -> None:
    first = next((r for r in f["queries"]["node1_to_node2"] if r["outcome"] == "ok"), None)
    assert first is not None, "node2 answered no query from node1 while the driver ran"
    print(f"oracle: phase A: node2 first answered at {first['ended_at']} s "
          f"(limit {PHASE_A} s) with node1 linked over {first.get('networks')}")
    assert first["ended_at"] <= PHASE_A, (
        f"node2 first answered node1 {first['ended_at']} s after the driver started; "
        f"want within {PHASE_A} s")
    assert first.get("networks") == ["tor"], (
        f"node1 held links over {first.get('networks')} to node2 at the first answer; "
        "with the LAN gone, only tor links")


def check_withdrawn(f: dict) -> float:
    """Tor's start, after checking node2 advertised no onion while Tor was down."""
    down = f["down_seen"]
    assert down is not None, (
        f"node2 never logged the loss of its onion service; tap {f['tap']}, lines {f['log']}")
    start = down + f["ops"]["restart-tor"]["down"]
    window = [s for s in f["samples"] if down < s["t"] <= start - START_SLACK and "error" not in s]
    assert window, f"no sample fell between the stop ({down} s) and Tor's start ({start} s)"
    held = [s["t"] for s in window if s["tor"]]
    assert not held, f"node2 still advertised its onion while Tor was down, at {held} s"
    return start


def check_phase_b(f: dict) -> None:
    start = check_withdrawn(f)
    back = f["lan_back_at"]
    assert back is None or back >= start + PHASE_B, (
        f"node2's address returned at {back} s, before phase B's {PHASE_B} s bound from "
        f"Tor's start at {start} s ran out: tcp may carry the queries, so Tor's recovery "
        "is not judged; lan-outage's --for leaves too little time")
    for d in DIRECTIONS:
        back = next((r for r in f["queries"][d] if r["outcome"] == "ok"
                     and r["ended_at"] > start and "tor" in (r.get("networks") or [])), None)
        assert back is not None, f"{d}: no query answered over tor after Tor started at {start} s"
        assert back["ended_at"] - start <= PHASE_B, (
            f"{d}: the first answer over tor after Tor's start came "
            f"{back['ended_at'] - start:.1f} s after it; want within {PHASE_B} s")
        print(f"oracle: phase B: {d} answered over tor at {back['ended_at']} s, "
              f"{back['ended_at'] - start:.1f} s after Tor's start at {start} s")
    tap = f["tap"]
    assert tap["refused"] is None and tap["broken"] is None, (
        f"node2's log tap did not stay whole ({tap}): astrald restarted or the record is incomplete")


def check_phase_c(f: dict) -> None:
    back, tcp, gone = f["lan_back_at"], f["tcp_both_at"], f["tor_gone_at"]
    assert back is not None, "the driver never saw node2's 10.77 address return"
    assert tcp is not None, f"node1 and node2 shared no tcp link after the LAN returned at {back} s"
    assert tcp - back <= PHASE_C, (
        f"node1 and node2 shared a tcp link {tcp - back:.1f} s after the LAN returned; "
        f"want within {PHASE_C} s")
    for d in DIRECTIONS:
        failed = [r for r in f["queries"][d]
                  if r["sent_at"] >= tcp + RECOVERED and r["outcome"] != "ok"]
        assert not failed, f"{d}: queries failed after the tcp link at {tcp} s: {failed[:5]}"
    assert gone is not None, f"node1 still listed a tor link to node2 at the driver's end ({f['ended_at']} s)"
    assert gone - tcp <= RETIRE, (
        f"node1 dropped its tor link to node2 {gone - tcp:.1f} s after the tcp link; "
        f"want within {RETIRE} s")
    print(f"oracle: phase C: LAN back at {back} s, tcp link at {tcp} s, tor link gone at {gone} s")


def check_deadlines(f: dict) -> None:
    for d in DIRECTIONS:
        late = [r for r in f["queries"][d] if r["ended_at"] - r["sent_at"] > QUERY + SLACK]
        assert not late, f"{d}: queries outlived their {QUERY} s deadline: {late[:5]}"


def main():
    f = load()["facts"]["fallback_paths"]
    if f["seeded"]:
        print(f"oracle: the driver gave {f['seeded']} the other's onion: the cut came before the sync")
    check_phase_a(f)
    check_phase_b(f)
    check_phase_c(f)
    check_deadlines(f)
    print(f"oracle: {len(f['queries']['node1_to_node2'])} + "
          f"{len(f['queries']['node2_to_node1'])} queries, none past its deadline")


main()
