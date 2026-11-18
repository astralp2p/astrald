#!/usr/bin/env python3
"""Oracle: node1's link task relinks a node behind NAT through its gateway, with no nodes.new_link.

Observed by the driver, judged here (fact `gateway_relink`):

- node1 held a gw link to node2 when the driver began (gateway-relay's), and
  the driver closed it;
- a new outbound gw link from node1 to node2 stood within WITHIN s of the
  close: node1 dialed node2's gw row. node2 holds no row that reaches node1,
  so an inbound link would be node2's doing, and the driver called no
  `nodes.new_link`;
- node1's log shows its link task taking the closed link as broken and
  restoring it (`link to <node2> is broken` then `restored after N attempts`
  or a further `established`), which names MaintainLinkTask as the dialer.
"""
import re

from lib.sessionio import load

WITHIN = 90.0
ID = r"([0-9a-f]{66})"
BROKEN = re.compile(rf"link to {ID} is broken, trying to reconnect$")
RESTORED = re.compile(rf"link to {ID} (?:restored after \d+ attempts|established)$")


def main():
    doc = load()
    f = doc["facts"]["gateway_relink"]
    id2 = doc["nodes"]["node2"]["identity"]
    print(f"oracle: before {f['before']}; timeline {f['timeline']}")
    print(f"oracle: node1 log {f['log']}")

    assert not f["tap"]["refused"] and not f["tap"]["broken"], f"node1's log record is incomplete: {f['tap']}"
    assert any(l["network"] == "gw" for l in f["before"]), (
        f"node1 held no gw link to node2 when the driver began: {f['before']}")
    assert f["back_at"] is not None, (
        f"no new outbound gw link from node1 to node2 within {f['timeline'][-1]['t']} s of the close: "
        f"{f['timeline'][-1]['links']}")
    took = f["back_at"] - f["closed_at"]
    assert took <= WITHIN, f"the gw link came back {took:.1f} s after the close, not within {WITHIN} s"

    broken = [e["t"] for e in f["log"] if (m := BROKEN.search(e["text"])) and m.group(1) == id2]
    assert broken, "node1's log shows no link task taking the closed link to node2 as broken"
    restored = [e["t"] for e in f["log"] if (m := RESTORED.search(e["text"])) and m.group(1) == id2
                and e["t"] >= broken[0]]
    assert restored, "node1's log shows its link task never restoring the link to node2"

    print(f"oracle: node1's link task relinked node2 over gw {took:.1f} s after the close "
          f"({restored[0] - broken[0]:.1f} s from broken to restored on node1's clock)")


main()
