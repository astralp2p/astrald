#!/usr/bin/env python3
"""Oracle: a node's own query to an app on its sibling answers, names the node
as the caller, and never hangs.

- 6 of 6 calls (3 from node1's node token to node2's app, 3 from node2's to
  node1's) answer `ok` within 5 s; none ends `timeout` at the 15 s deadline;
- each reply is `hello, <calling node>`, and `meshapp call` saw the calling
  node as its own identity: the relayed frame names the original caller
  (astral-docs topics/app-routing.md:42-44, Resolving the host), and a caller
  that is the relaying node itself is not a relay (:103-104, Authorizing the
  relay);
- each app counted the remote node as a `hello` caller exactly 3 times.

The 5 s bound is derived: the 3 s wait for a pull on a miss
(topics/app-routing.md:69-70) plus one relayed round trip on loopback;
apphost.register pushed, or the sibling sync carried, each app's contract, so
no pull is expected.
"""
from lib.sessionio import load

BOUND_MS = 5000
ROUNDS = 3


def main():
    doc = load()
    nodes, f = doc["nodes"], doc["facts"]["node_query_sibling_app"]
    ids = {n: nodes[n]["identity"] for n in ("node1", "node2")}
    calls = f["calls"]
    assert len(calls) == 2 * ROUNDS, f"{len(calls)} calls made; want {2 * ROUNDS}"

    failed = [c for c in calls if c["outcome"] != "ok"]
    assert not failed, f"node-token calls that did not answer ok: {failed}"
    slow = [c for c in calls if c["ms"] > BOUND_MS]
    assert not slow, f"calls over {BOUND_MS} ms: {slow}"
    for c in calls:
        me = ids[c["from"]]
        assert c["caller"] == me, (
            f"{c['from']}'s node token called as {c['caller']}, not as the node {me}")
        assert c["reply"] == f"hello, {me}", (
            f"{c['to']}'s app answered {c['reply']!r} to {c['from']}; want "
            f"'hello, {me}': the relay did not name the original caller")

    for host, caller in (("node1", "node2"), ("node2", "node1")):
        served = (f["states"][host].get("served") or {}).get(ids[caller], 0)
        assert served == ROUNDS, (
            f"the app on {host} served {caller} {served} times; want {ROUNDS}: "
            f"{f['states'][host].get('served')}")

    ms = sorted(c["ms"] for c in calls)
    print(f"oracle: 6/6 node-token calls to the sibling's app answered ok as the "
          f"calling node, {ms[0]:.0f}-{ms[-1]:.0f} ms")


main()
