#!/usr/bin/env python3
"""Driver: each node's own token calls the app on the sibling node, three
times each way.

A mesh app (tests/e2e/internal/meshapp, lib/meshapps.py) runs on node1 and on
node2 and serves `hello`. node1's node token then calls node2's app, and
node2's node token calls node1's app, three times each, with a 15 s deadline
per call (`meshapp call`). The driver stops the apps and records the
`node_query_sibling_app` fact: {apps, calls, states}. It judges nothing.

why the node token: the query is the node's own, the case of the feedback
report "a node's own query to a sibling app hangs" (fWdffmESP1).
"""
from lib import meshapps
from lib.sessionio import load, write_facts

PREFIX = "m3"
NAMES = ["node1", "node2"]
ROUNDS = 3
# why: a query no node routes would hang; 15 s bounds it well past the
# oracle's 5 s
DEADLINE = 15


def main():
    nodes = load()["nodes"]
    meshapps.spawn(PREFIX, nodes, NAMES, service=PREFIX, period=0)
    try:
        ready = meshapps.wait_ready(PREFIX, NAMES)
        app = meshapps.ids(ready)
        calls = []
        for i in range(ROUNDS):
            for caller, host in (("node1", "node2"), ("node2", "node1")):
                r = meshapps.call(nodes[caller], target=app[host], timeout=DEADLINE)
                calls.append({"round": i, "from": caller, "to": host, **r})
    finally:
        meshapps.stop(PREFIX, NAMES)
    states = meshapps.states(PREFIX, NAMES)
    write_facts({"node_query_sibling_app": {"apps": app, "calls": calls,
                                            "states": states}})
    ok = sum(1 for c in calls if c["outcome"] == "ok")
    print(f"driver: {len(calls)} node-token calls to the sibling's app, {ok} ok")


main()
