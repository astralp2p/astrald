#!/usr/bin/env python3
"""Oracle: on four siblings joined by kcp links only, with no relay contract
pushed or carried, each of the 12 first calls answers through the
routing-time pull.

- the kcp join settled, and every link each node listed after the join and
  after the calls is kcp;
- the four providers became ready, and no app opened a follow: a follow in
  an app's name pushes the app's relay contract to every swarm member
  (astral-docs protocols/services/ops/services.discover.md:86-89, Discovery
  in an app's name), so a provider's contract reaches a consumer's node by
  no follow;
- every consumer finished a first call to each remote provider; 12 of 12
  answered `ok`, each within 4 s: the 3 s wait for the pull
  (topics/app-routing.md:69-70, Resolving the host) plus a relayed round
  trip;
- each consumer called each remote provider once, and each provider served
  each remote consumer exactly once and called no app: a call pushes the
  caller's contract to the callee's node (topics/app-routing.md:128-129),
  so no call carried a provider's contract;
- each consumer node logged one ask for each remote provider app's relay
  contracts and a pulled contract for it (mod/nodes/src/pull_relays.go:142,
  :156): the push was off (`apphost.skip_sibling_push`) and the pull routed
  the call (topics/app-routing.md:61-68, :167-168, Reach);
- every log tap is whole.
"""
from lib import meshapps
from lib.sessionio import load

NAMES = ["node1", "node2", "node3", "node4"]
BOUND_MS = 4000


def check_links(f: dict) -> None:
    assert f["join"]["settled"], f"the pairs never held kcp links only: {f['join']['links']}"
    for where, links in (("after the join", f["join"]["links"]), ("after the calls", f["links_end"])):
        other = {n: [r for r in rows if r["network"] != "kcp"] for n, rows in links.items()}
        other = {n: rows for n, rows in other.items() if rows}
        assert not other, f"links that are not kcp {where}: {other}"


def check_apps(f: dict) -> None:
    assert f["providers_ready"], f"the providers never became ready: {f['providers']}"
    followed = {f"{role}@{n}": s.get("follow_opens")
                for role in ("providers", "consumers") for n, s in f[role].items()
                if s.get("follows") is not False or s.get("follow_opens")}
    assert not followed, f"apps that followed: {followed}"
    called = {n: list(s["providers"]) for n, s in f["providers"].items() if s.get("providers")}
    assert not called, f"providers that called an app: {called}"


def check_calls(f: dict) -> list:
    cons, prov, bad, ms = f["consumers"], f["providers"], [], []
    for c, p in meshapps.pairs(NAMES):
        peer = (cons[c].get("providers") or {}).get(prov[p].get("app"))
        first = (peer or {}).get("first")
        if first is None:
            bad.append(f"{c}->{p}: {'not a target' if peer is None else 'never called'}")
            continue
        if first["outcome"] != "ok":
            bad.append(f"{c}->{p}: first call {first['outcome']} ({first.get('err')})")
        elif first["ms"] > BOUND_MS:
            bad.append(f"{c}->{p}: first call took {first['ms']:.0f} ms; want ≤ {BOUND_MS}")
        if peer["calls"] != 1:
            bad.append(f"{c}->{p}: {peer['calls']} calls; want 1")
        ms.append(first["ms"])
    assert not bad, f"pairs that failed: {bad}"
    consumer = meshapps.ids(cons)
    for p in NAMES:
        want = {consumer[c]: 1 for c in NAMES if c != p}
        got = prov[p].get("served") or {}
        assert got == want, f"the provider on {p} served {got}; want each remote consumer once: {want}"
    return ms


def check_pulls(f: dict) -> None:
    broken = {n: t for n, t in f["taps"].items() if t["refused"] or t["broken"]}
    assert not broken, f"log taps incomplete: {broken}"
    for c, by_provider in f["logs"].items():
        for p, log in by_provider.items():
            assert len(log["asks"]) == 1, (
                f"{c} asked {len(log['asks'])} times for the provider on {p}; want 1: {log['asks']}")
            assert log["pulled"], f"{c} logged no relay contract pulled for the provider on {p}"


def main():
    f = load()["facts"]["apps_matrix_kcp"]
    check_links(f)
    check_apps(f)
    assert f["complete"], "not every first call finished within the driver's 60 s"
    ms = sorted(check_calls(f))
    check_pulls(f)
    print(f"oracle: 12/12 first calls answered over kcp links alone through a pull, "
          f"{ms[0]:.0f}-{ms[-1]:.0f} ms")


main()
