#!/usr/bin/env python3
"""Oracle: a kcp-only sibling that restarts relinks over kcp within seconds of its link task starting.

Observed by the driver, judged here (fact `kcp_relink`):

- the restart took: node2 stopped answering and answered again;
- node2 lists an outbound kcp link to node1: node2 dialed it. node1 keeps
  the kcp link it held to node2's old process pooled, so its own link task
  does not redial, and an inbound link proves nothing about node2;
- node2 linked within WITHIN s of its link task starting. The measure is
  node2's own log: `starting to maintain link to <node1>` to `link to
  <node1> established`, on node2's clock.

why a second measure: the tap opens only once node2 answers again, and the
user module starts its link tasks concurrently with apphost, so the tap can
miss the start line. Then the measure is the host's polls, from node2's first
answer to the first poll that lists the outbound kcp link, which counts
neither node2's shutdown nor its boot. It is judged only with node2's log
showing no failed attempt to node1: the link line, if the tap caught it, is
`established` (the task's first attempt produced the link), not `restored
after N attempts`. A task that started before apphost answered is thus
measured short only by that gap, never across a backoff.
"""
import re

from lib.sessionio import load

WITHIN = 5.0
ID = r"([0-9a-f]{66})"
START = re.compile(rf"starting to maintain link to {ID}$")
LINKED = re.compile(rf"link to {ID} (?:established|restored after \d+ attempts)$")
RESTORED = re.compile(rf"link to {ID} restored after \d+ attempts$")


def first(entries: list, rx: re.Pattern, peer: str):
    return next((e["t"] for e in entries if (m := rx.search(e["text"])) and m.group(1) == peer), None)


def main():
    doc = load()
    f = doc["facts"]["kcp_relink"]
    id1 = doc["nodes"]["node1"]["identity"]
    print(f"oracle: down {f['down']} s, up {f['up']} s, outbound kcp {f['kcp']} s; links {f['links']}")
    print(f"oracle: node2 log {f['log2']}")

    assert f["down"] is not None, f"node2 never stopped answering in {f['watched']} s: the restart did not take"
    assert f["up"] is not None, f"node2 did not answer again within {f['watched']} s of the driver's start"
    assert not f["tap2"]["refused"] and not f["tap2"]["broken"], f"node2's log record is incomplete: {f['tap2']}"
    assert f["kcp"] is not None and any(l["network"] == "kcp" and l["outbound"] for l in f["links"]), (
        f"node2 lists no outbound kcp link to node1 after its restart: {f['links']}")

    start, linked = first(f["log2"], START, id1), first(f["log2"], LINKED, id1)
    if start is not None and linked is not None:
        took, how = linked - start, "node2's log, task start to link"
    else:
        assert first(f["log2"], RESTORED, id1) is None, (
            "node2's link task failed an attempt to node1 before it linked, and the tap missed its start line: "
            f"{f['log2']}")
        took, how = f["kcp"] - f["up"], "host polls, node2's first answer to the outbound kcp link listed"
    assert took <= WITHIN, (
        f"node2 linked {took:.2f} s after its link task started ({how}); want within {WITHIN} s")
    print(f"oracle: node2 relinked to node1 over kcp {took:.2f} s after its link task started ({how})")


main()
