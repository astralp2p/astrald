#!/usr/bin/env python3
"""Oracle: after two address changes within 5 s, node1's final address reaches its siblings.

Endpoint-change ticket, criterion 6: an address changed twice within 5 s;
the final address set reaches siblings. node1 (B) moves to an intermediate
and then a final address; node2 (A, behind NAT) and the reflector (C) are its
siblings. Observed by the driver, judged here (fact `endpoint_change_double`):

- node1 changed twice within 5 s: the driver saw it take the intermediate
  address and then the final one less than GAP_MAX s later, and node1's own
  log holds at least two `network addresses changed` lines naming the
  intermediate address (mod/ip saw it come and go);
- node2 and the reflector each resolve a row of node1 at the final address
  within WITHIN s of node1 taking it;
- node2 lists an outbound link to node1 at the final address within WITHIN s,
  so the final set is the one node2 uses.

why WITHIN is 30 s: the criterion names no bound; criterion 5's bound for a
single move is the one reused.
note: a row at the intermediate address may stay on a sibling. Removed
addresses are not withdrawn (A3G5, deferred); they age out with their TTL.
"""
from lib import addrmove
from lib.sessionio import load

WITHIN = 30.0
GAP_MAX = 5.0


def main():
    doc = load()
    f = doc["facts"]["endpoint_change_double"]
    mid, final = f["targets"]
    moved, seen = f["moved"], f["watch"]
    print(f"oracle: node1 -> {mid} -> {final}; changes {moved['changes']}")
    print(f"oracle: watch {seen}")
    print(f"oracle: after {f['after']}")

    assert not {mid, final} & set(moved["start"] or []), (
        f"node1 already held {mid} or {final} when the driver began: {moved['start']}")
    t_mid = next((c["t"] for c in moved["changes"] if mid in c["added"]), None)
    t_final = next((c["t"] for c in moved["changes"] if final in c["added"]), None)
    assert t_mid is not None, f"the driver never saw node1 take {mid}: {moved['changes']}"
    assert t_final is not None, f"node1 never took {final}: {moved['changes']}, holds {moved['ips']}"
    assert 0 < t_final - t_mid < GAP_MAX, (
        f"node1 took {final} {t_final - t_mid:.2f} s after {mid}; want two changes within {GAP_MAX} s")
    assert mid not in moved["ips"], f"node1 still holds {mid}: {moved['ips']}"

    tap = f["taps"]["node1"]
    assert not tap["refused"] and not tap["broken"], f"node1's log record is incomplete: {tap}"
    seen_mid = [e["text"] for e in f["logs"]["node1"]
                if "network addresses changed" in e["text"] and mid in e["text"]]
    assert len(seen_mid) >= 2, (
        f"node1's mod/ip did not see {mid} come and go as two changes: {seen_mid}")

    for key, who in (("sibling_row_at", "node2"), ("witness_row_at", "the reflector")):
        at = seen[key]
        assert at is not None and at - t_final <= WITHIN, (
            f"{who} resolved no row of node1 at {final} within {WITHIN} s of the change "
            f"(first at {at} s, change at {t_final} s)")
    for rows, who in ((f["after"]["node2_rows"], "node2"), (f["after"]["reflector_rows"], "the reflector")):
        assert addrmove.holds(rows, final), f"{who} does not resolve node1 at {final} at the end: {rows}"
    assert seen["linked_at"] is not None and seen["linked_at"] - t_final <= WITHIN, (
        f"node2 listed no outbound link to node1 at {final} within {WITHIN} s of the change: {seen['timeline']}")
    print(f"oracle: node1 changed twice in {t_final - t_mid:.2f} s; {final} reached node2 in "
          f"{seen['sibling_row_at'] - t_final:.2f} s and the reflector in {seen['witness_row_at'] - t_final:.2f} s; "
          f"node2 linked in {seen['linked_at'] - t_final:.2f} s")


main()
