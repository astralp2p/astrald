#!/usr/bin/env python3
"""Oracle: a kcp-only sibling that restarts relinks over kcp within seconds of its link task starting.

Observed by the driver, judged here (fact `kcp_relink`):

- the restart took: node2 stopped answering and answered again;
- node2 lists a kcp link to node1;
- node2 linked within WITHIN s of its link task starting. The measure is
  node2's own log: `starting to maintain link to <node1>` to `link to
  <node1> established`, on node2's clock. When the tap opened too late for
  the first line, the bound is the host's: from the first poll node2 did not
  answer, which precedes the task's start, to the first poll that listed the
  kcp link, less one poll interval.
"""
import re

from lib.sessionio import load

WITHIN = 5.0
POLL = 0.2
ID = r"([0-9a-f]{66})"
START = re.compile(rf"starting to maintain link to {ID}$")
LINKED = re.compile(rf"link to {ID} (?:established|restored after \d+ attempts)$")


def first(entries: list, rx: re.Pattern, peer: str):
    return next((e["t"] for e in entries if (m := rx.search(e["text"])) and m.group(1) == peer), None)


def main():
    doc = load()
    f = doc["facts"]["kcp_relink"]
    id1 = doc["nodes"]["node1"]["identity"]
    print(f"oracle: down {f['down']} s, up {f['up']} s, kcp {f['kcp']} s; links {f['links']}")
    print(f"oracle: node2 log {f['log2']}")

    assert f["down"] is not None, f"node2 never stopped answering in {f['watched']} s: the restart did not take"
    assert f["up"] is not None, f"node2 did not answer again within {f['watched']} s of the driver's start"
    assert f["kcp"] is not None and any(l["network"] == "kcp" for l in f["links"]), (
        f"node2 lists no kcp link to node1 after its restart: {f['links']}")

    start, linked = first(f["log2"], START, id1), first(f["log2"], LINKED, id1)
    if start is not None and linked is not None:
        took, how = linked - start, "node2's log, task start to link"
    else:
        took, how = f["kcp"] - f["down"] - POLL, "host polls, node2 down to the kcp link listed"
    assert took <= WITHIN, (
        f"node2 linked {took:.2f} s after its link task started ({how}); want within {WITHIN} s")
    print(f"oracle: node2 relinked to node1 over kcp {took:.2f} s after its link task started ({how})")


main()
