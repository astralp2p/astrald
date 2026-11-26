#!/usr/bin/env python3
"""Oracle: node2 behind NAT, node1 and the reflector form one linked swarm.

Observed by the driver, judged here (fact `endpoint_change_swarm`):

- the reflector joined through node1: the membership's subject is the reflector;
- node2 (A) holds a link to node1 (B) and one to the reflector (C), and every
  link node2 holds is outbound: nothing dials a node behind this NAT;
- node1 holds a link to the reflector;
- each node's swarm lists the other two, so each accepts and forwards an
  endpoint-change notice about the others (mod/nodes/src/endpoints_changed.go
  acceptsEndpointsChanged, forwardEndpointsChanged).
"""
from lib.sessionio import load


def main():
    doc = load()
    f = doc["facts"]["endpoint_change_swarm"]
    ids = {name: doc["nodes"][name]["identity"] for name in ("node1", "node2", "reflector")}
    snap = f["nodes"]
    print(f"oracle: admitted {f['admitted']}; ready={f['ready']} after {f['waited']} s")
    for name, s in snap.items():
        print(f"oracle: {name} links {s['links']}; swarm {s['swarm']}")

    assert f["admitted"].get("subject") == ids["reflector"], (
        f"the reflector was not admitted: {f['admitted']}")

    def links(a, b):
        return [r for r in snap[a]["links"] if r["remote"] == ids[b]]

    assert links("node2", "node1"), f"node2 holds no link to node1: {snap['node2']['links']}"
    assert links("node2", "reflector"), f"node2 holds no link to the reflector: {snap['node2']['links']}"
    assert links("node1", "reflector"), f"node1 holds no link to the reflector: {snap['node1']['links']}"
    inbound = [r for r in snap["node2"]["links"] if not r["outbound"]]
    assert not inbound, f"node2, behind a NAT that admits no inbound connection, holds inbound links: {inbound}"
    for name in ids:
        missing = sorted(set(ids.values()) - {ids[name]} - set(snap[name]["swarm"]))
        assert not missing, f"{name}'s swarm does not list {missing}: {snap[name]['swarm']}"
    print("oracle: node2 behind NAT dials node1 and the reflector, node1 links the reflector, "
          "and each swarm lists the other two")


main()
