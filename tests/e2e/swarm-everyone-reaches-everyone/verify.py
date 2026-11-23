#!/usr/bin/env python3
"""Oracle: every node and every app of a four-node swarm reaches every other
node and app in each direction, and a linked node of another User reaches no
app and no swarm read of it.

Swarm, 48 queries, 12 per kind, each answered `ok` within 10 s (so none
`route_not_found`, none `timeout`):

- node→node `user.info` and node→app `hello`, each from the node's token;
  each `hello` reply names the calling node;
- app→node `apphost.whoami`, whose answer names the calling app;
- app→app `hello`, the first call to each provider the reach=swarm follow
  reported; a mesh app counts it `ok` only when the reply names it.

A query to an app routes through its relay contract (astral-docs
topics/app-routing.md:31-94, Resolving the host); a local app's query to a
node over its link pushes the app's contract first (:111-130, Sending to a
node over its own link); a handler sees the original caller (:147-156,
Answering). The 10 s bound is the umbrella's discovery-to-first-query bound
(operator decision D11).

Stranger, a node of another User linked to node1:

- its node token's and its apps' calls to the swarm apps end
  `route_not_found` or rejected: an app outside the swarm addresses none
  (topics/app-routing.md:189-191, Reach);
- `user.swarm_status` on node1 is refused (topics/node-exposure.md:67-103,
  Linked peers; tests/e2e/swarm-authz-matrix/README.md, The contract);
- its follow lists no swarm offering, and no swarm follow lists its offering
  (protocols/services/ops/services.discover.md:23-41, Visibility);
- no swarm app served a caller from the stranger.
"""
from lib import meshapps
from lib.sessionio import load

NAMES = ["node1", "node2", "node3", "node4"]
STRANGER = "f2stranger"
BOUND_MS = 10000
# what a refusal of an op reads like (swarm-authz-matrix's REFUSAL_MARKS)
REFUSAL_MARKS = ("not permitted", "origin not allowed", "denied")


def bad_query(q: dict) -> str | None:
    if q["outcome"] != "ok":
        return f"{q['outcome']} ({q.get('err')})"
    if q["ms"] > BOUND_MS:
        return f"{q['ms']:.0f} ms"
    return None


def check_node_queries(f: dict) -> int:
    ids, bad = f["nodes"], []
    for kind in ("node_node", "node_app"):
        assert len(f[kind]) == 12, f"{kind}: {len(f[kind])} queries; want 12"
        for q in f[kind]:
            why = bad_query(q)
            if why is None and q["caller"] != ids[q["from"]]:
                why = f"called as {q['caller']}, not as the node"
            if why is None and kind == "node_app" and q["reply"] != f"hello, {ids[q['from']]}":
                why = f"reply {q['reply']!r} names another caller"
            if why:
                bad.append(f"{kind} {q['from']}->{q['to']}: {why}")
    assert not bad, f"node-token queries that failed: {bad}"
    return 24


def check_app_queries(f: dict) -> int:
    st, ids, bad = f["states"], f["nodes"], []
    app = meshapps.ids(st)
    for c, p in meshapps.pairs(NAMES):
        peer = meshapps.peer(st, c, p)
        first = (peer or {}).get("first")
        if first is None:
            bad.append(f"app_app {c}->{p}: {'not reported' if peer is None else 'never called'}")
        elif why := bad_query(first):
            bad.append(f"app_app {c}->{p}: {why}")
        probe = ((st[c].get("probes") or {}).get(ids[p]) or {}).get("first")
        if probe is None:
            bad.append(f"app_node {c}->{p}: never probed")
        elif why := bad_query(probe):
            bad.append(f"app_node {c}->{p}: {why}")
        elif probe.get("reply") != app[c]:
            bad.append(f"app_node {c}->{p}: whoami answered {probe.get('reply')}, not the app {app[c]}")
    assert not bad, f"app queries that failed: {bad}"
    return 24


def refused(q: dict) -> bool:
    if q["outcome"].startswith("rejected:"):
        return True
    return q["outcome"] == "error" and any(m in (q.get("err") or "").lower() for m in REFUSAL_MARKS)


def check_stranger(f: dict) -> None:
    s, st, ids = f["stranger"], f["states"], f["nodes"]
    reached = [f"{k} ->{q['to']}: {q['outcome']}" for k in ("node_app", "app_app") for q in s[k]
               if q["outcome"] != "route_not_found" and not q["outcome"].startswith("rejected:")]
    assert not reached, f"stranger calls to swarm apps that did not end route_not_found or rejected: {reached}"
    assert refused(s["swarm_status"]), f"node1 did not refuse the stranger user.swarm_status: {s['swarm_status']}"

    seen = sorted((st[STRANGER].get("providers") or {}))
    assert not seen, f"the stranger's follow listed offerings {seen}; want none of the swarm's"
    app = meshapps.ids(st)
    for n in NAMES:
        want = {app[p] for p in NAMES if p != n}
        got = set(st[n].get("providers") or {})
        assert got == want, f"the follow of the app on {n} listed {sorted(got)}; want the three remote swarm apps"

    outside = {ids[STRANGER], app[STRANGER]} | {q["caller"] for q in s["app_app"]}
    served = {n: sorted(set(st[n].get("served") or {}) & outside) for n in NAMES}
    served = {n: c for n, c in served.items() if c}
    assert not served, f"swarm apps served callers from the stranger: {served}"


def main():
    f = load()["facts"]["everyone_reaches_everyone"]
    n = check_node_queries(f) + check_app_queries(f)
    check_stranger(f)
    print(f"oracle: {n}/48 swarm queries answered ok within {BOUND_MS // 1000} s; the "
          "stranger reached no swarm app and was refused the roster")


main()
