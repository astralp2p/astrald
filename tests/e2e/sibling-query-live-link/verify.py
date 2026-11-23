#!/usr/bin/env python3
"""Oracle: queries a node, its User and its sibling send to the sibling's node
identity over a live link answer, both ways, across a new link and the close
of the older one.

- the User opened a second link from node1 to node2, and after round 15 node1
  held no link to node2 from before it;
- before every round, node1 and node2 each listed a link to the other;
- 100 of 100 queries answered: `user.info` an `Info` naming the queried node,
  `objects.contains` a bool (false, the object is absent);
- no query ended `route_not_found`, and each took at most 2 s.

A link carries queries between its two parties (astral-docs
core-definitions/link.md:4; topics/link-mux.md:306-316, Sessions), and route
not found means no node on the path routes the query
(core-definitions/query.md:51-53), so it is never the outcome of a query to a
node the caller's node holds a live link to. A
sibling and the User reach `user.info` and `objects.contains` on a member
(tests/e2e/swarm-authz-matrix/README.md:30; mod/user/src/authorizers.go).
The 2 s bound is derived: a loopback round trip with wide slack, far below the
5 s deadline; no source bounds the answer time of these ops.
"""
from lib.sessionio import load

ROUNDS = 20
PER_ROUND = 5
BOUND_MS = 2000
CLOSE_AT = 15


def check_links(f: dict) -> None:
    opened, closed = f.get("opened") or {}, f.get("closed") or {}
    assert opened.get("id"), f"the User on node1 opened no second link to node2: {opened}"
    assert not closed.get("errors"), f"node1 failed to close an older link: {closed}"
    assert closed.get("older"), "node1 held no link to node2 before the new one"
    for r in f["rounds"]:
        assert r["links"]["both"], (
            f"before round {r['i']}, a node listed no link to the other: {r['links']}")
        if r["i"] >= CLOSE_AT:
            left = {x["id"] for x in r["links"]["node1"]} & set(closed["older"])
            assert not left, f"round {r['i']}: node1 still lists the older link(s) {left}"


def check_queries(f: dict, ids: dict) -> list:
    qs = [dict(q, round=r["i"]) for r in f["rounds"] for q in r["queries"]]
    assert len(f["rounds"]) == ROUNDS and len(qs) == ROUNDS * PER_ROUND, (
        f"{len(f['rounds'])} rounds, {len(qs)} queries; want {ROUNDS}, {ROUNDS * PER_ROUND}")
    lost = [q for q in qs if q["outcome"] == "route_not_found"]
    assert not lost, f"{len(lost)} queries ended route_not_found over a live link: {lost[:5]}"
    failed = [q for q in qs if q["outcome"] != "ok"]
    assert not failed, f"{len(failed)} queries did not answer: {failed[:5]}"
    for q in qs:
        a = q["answer"]
        if q["op"] == "user.info":
            assert a["type"] == "Info" and a["node"] == ids[q["to"]], (
                f"round {q['round']}: {q['asker']}'s user.info to {q['to']} answered {a}")
        else:
            assert a == {"type": "bool", "value": False}, (
                f"round {q['round']}: {q['asker']}'s objects.contains answered {a}; want false")
    slow = [q for q in qs if q["ms"] > BOUND_MS]
    assert not slow, f"queries over {BOUND_MS} ms: {slow[:5]}"
    return qs


def main():
    doc = load()
    nodes, f = doc["nodes"], doc["facts"]["sibling_query_live_link"]
    ids = {n: nodes[n]["identity"] for n in ("node1", "node2")}
    check_links(f)
    qs = check_queries(f, ids)
    ms = sorted(q["ms"] for q in qs)
    print(f"oracle: {len(qs)}/{len(qs)} sibling-targeted queries answered, "
          f"{ms[0]:.0f}-{ms[-1]:.0f} ms, across new link {f['opened']['id']} and the "
          f"close of {f['closed']['closed'] or f['closed']['gone_before']}; links at "
          f"the end {f['links_end']}")


main()
