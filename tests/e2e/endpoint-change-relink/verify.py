#!/usr/bin/env python3
"""Oracle: node2 behind NAT links to node1 at its new subnet within 30 s of node1's move.

Endpoint-change ticket, criterion 5: A behind NAT, C public, B moves to a new
subnet; A links to B within 30 s of B's change, with no manual
nodes.add_endpoint. Here A is node2, B node1, C the reflector. Observed by
the driver, judged here (fact `endpoint_change_relink`):

- before the move node1 held neither new address, node2 held a link to the
  reflector, the path the notice takes, and node2 resolved no row of node1 at
  the new address;
- node1 moved: it took the new address and gave up its 10.77 one;
- node2 lists an outbound link to node1 whose remote end is the new address
  within WITHIN s of the driver seeing node1 take it. node2 dialed it: the
  NAT admits no inbound connection;
- node2 learned the new address from the reflector's forward: node2's log
  holds `pulled N new endpoints of <node1> from <reflector>`
  (mod/nodes/src/endpoints_changed.go pullChangedEndpoints).

why the change time is the driver's poll: node1's addresses are read live
from its interfaces (`ip.local_addrs`), so the first poll that lists the new
address follows the move by one poll, 0.25 s, and a query.
"""
from lib import addrmove
from lib.sessionio import load

WITHIN = 30.0


def main():
    doc = load()
    f = doc["facts"]["endpoint_change_relink"]
    ids = {n: doc["nodes"][n]["identity"] for n in ("node1", "node2", "reflector")}
    old = doc["nodes"]["node1"]["lan_endpoint"].split(":")[1]
    new = f["targets"][-1]
    moved, seen, before = f["moved"], f["watch"], f["before"]
    print(f"oracle: node1 {old} -> {new}; changes {moved['changes']}")
    print(f"oracle: watch {seen}")
    print(f"oracle: node2 log {[e['text'] for e in f['logs']['node2']]}")

    assert new not in (moved["start"] or []), f"node1 already held {new} when the driver began: {moved['start']}"
    assert addrmove.to(before["links"]["node2"], ids["reflector"]), (
        f"node2 held no link to the reflector before the move: {before['links']['node2']}")
    assert not addrmove.holds(before["node2_rows"], new), (
        f"node2 resolved node1 at {new} before the move: {before['node2_rows']}")
    t_move = next((c["t"] for c in moved["changes"] if new in c["added"]), None)
    assert t_move is not None, f"node1 never took {new}: changes {moved['changes']}, holds {moved['ips']}"
    assert old not in moved["ips"], f"node1 still holds {old} after its move: {moved['ips']}"

    assert seen["linked_at"] is not None, (
        f"node2 listed no outbound link to node1 at {new} in the driver's watch; its links to node1: "
        f"{seen['timeline']}")
    took = seen["linked_at"] - t_move
    assert took <= WITHIN, f"node2 linked to node1 at {new} {took:.2f} s after the move; want within {WITHIN} s"

    tap = f["taps"]["node2"]
    assert not tap["refused"] and not tap["broken"], f"node2's log record is incomplete: {tap}"
    forwarded = f"new endpoints of {ids['node1']} from {ids['reflector']}"
    assert any(forwarded in e["text"] for e in f["logs"]["node2"]), (
        "node2's log holds no pull of node1's endpoints from the reflector, so the link did not come "
        f"from the forwarded notice: {[e['text'] for e in f['logs']['node2']]}")
    print(f"oracle: node2 linked to node1 at {new} {took:.2f} s after the move, "
          "after pulling node1's new endpoints from the reflector")


main()
