#!/usr/bin/env python3
"""Oracle: four apps started together on four siblings find each other through
a reach=swarm follow, and each first call to each reported provider answers,
over tcp and again over kcp alone.

Per phase:

- the four apps started within 2 s of each other (the exhibit's F1 flow);
- 12 of 12 ordered pairs: the consumer's follow reported the provider, and
  the consumer's first call to it answered `ok`, no `route_not_found` and no
  other failure (astral-docs topics/app-routing.md:31-94, Resolving the host:
  a held or pulled relay contract routes the call);
- each first call ended at most 10 s after the follow reported the provider
  (the umbrella's bound, operator decision D11: discovery to first query
  10 s);
- each consumer called each provider once, and each provider served each
  remote consumer exactly once: no retry hides a failure (`period=0`).

Phase `kcp` also: the kcp join settled; every link each node listed at the
phase's start and end is kcp; tcp dial and listen read false on every node
(lib/kcponly.py; astrald mod/tcp/src/module.go settings).
"""
from lib import meshapps
from lib.sessionio import load

NAMES = ["node1", "node2", "node3", "node4"]
STARTS_S = 2.0
FIRST_S = 10.0


def check_starts(name: str, ph: dict) -> None:
    starts = [t for t in ph["starts"].values() if t]
    assert len(starts) == len(NAMES), f"{name}: app starts {ph['starts']}"
    spread = max(starts) - min(starts)
    assert spread <= STARTS_S, f"{name}: the apps started {spread:.2f} s apart; want ≤ {STARTS_S}"


def check_pairs(name: str, ph: dict) -> list:
    """Every pair's first call; the ms of each."""
    st, ms, bad = ph["states"], [], []
    for c, p in meshapps.pairs(NAMES):
        peer = meshapps.peer(st, c, p)
        first = (peer or {}).get("first")
        if peer is None or first is None:
            bad.append(f"{c}->{p}: {'not reported' if peer is None else 'never called'}")
            continue
        if first["outcome"] != "ok":
            bad.append(f"{c}->{p}: first call {first['outcome']} ({first.get('err')})")
        elif first["end"] - peer["reported_at"] > FIRST_S:
            bad.append(f"{c}->{p}: answered {first['end'] - peer['reported_at']:.1f} s "
                       f"after the report; want ≤ {FIRST_S:.0f} s")
        if peer["calls"] != 1:
            bad.append(f"{c}->{p}: {peer['calls']} calls; want 1")
        ms.append(first["ms"])
    assert not bad, f"{name}: pairs that failed: {bad}"
    return ms


def check_served(name: str, ph: dict) -> None:
    st = ph["states"]
    app = meshapps.ids(st)
    for p in NAMES:
        want = {app[c]: 1 for c in NAMES if c != p}
        got = st[p].get("served") or {}
        assert got == want, f"{name}: the app on {p} served {got}; want each remote app once: {want}"


def kcp_only(where: str, links: dict) -> None:
    other = {n: [r for r in rows if r["network"] != "kcp"] for n, rows in links.items()}
    other = {n: rows for n, rows in other.items() if rows}
    assert not other, f"kcp: links that are not kcp at the phase's {where}: {other}"


def check_kcp(f: dict) -> None:
    join, ph = f["kcp_join"], f["kcp"]
    assert join["settled"], f"kcp: the pairs never held kcp links only: {join['links']}"
    kcp_only("start", ph["links_start"])
    kcp_only("end", ph["links_end"])
    on = {n: v for n, v in ph["tcp_settings"].items() if any(x is not False for x in v.values())}
    assert not on, f"kcp: tcp dial or listen is not false: {on}"


def main():
    f = load()["facts"]["swarm_apps_talk"]
    summary = []
    for name in ("tcp", "kcp"):
        ph = f[name]
        assert ph["complete"], f"{name}: not every first call finished within the driver's 60 s"
        check_starts(name, ph)
        ms = sorted(check_pairs(name, ph))
        check_served(name, ph)
        summary.append(f"{name} 12/12 ok, {ms[0]:.0f}-{ms[-1]:.0f} ms")
    check_kcp(f)
    print(f"oracle: four apps started together reached each other on the first call: "
          f"{'; '.join(summary)}; the kcp phase ran on kcp links alone")


main()
