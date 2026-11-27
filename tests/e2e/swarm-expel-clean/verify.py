#!/usr/bin/env python3
"""Oracle: an expel leaves the other three siblings talking, cuts node4 and
its app off in both directions, and nothing crosses the ban once answered.

From the `expel_clean` fact and the `f4` apps' state files and call logs
(`lib/meshapps.py`):

- setup: all 12 pairs answered before the expel, every tap stayed whole, and
  every member logged the ban (node1 its expel, node2 and node3 the removal
  of node4) and node4 its own removal;
- the six pairs among node1-node3 answered every call that started after the
  expel (the E2E tests exhibit's F4), at least MIN_CALLS each;
- from GRACE after the ban landed on the caller's node, every call to node4's
  app ended `route_not_found` or `rejected:<code>`, at least one per caller
  (astral-docs topics/app-routing.md:131-146, Failures: "every host a former
  member" is route not found, "the host was expelled" is a refusal; spec
  conflict C6 of the plan: both are accepted), and from GRACE after node4's
  own removal no call by node4's app answered (topics/app-routing.md:95-110,
  Authorizing the relay: a claimed node refuses a relaying node that is not a
  current member);
- every member's follow removed node4's offering within FOLLOW of the ban on
  its node and never showed it again: with cause `member_left`, or
  `member_lost` when the carry ended before the ban landed there (node4 tore
  its links down first) (protocols/services/types/services.removed.md:14-22;
  a key retracted twice keeps its first cause);
- logs, per member X, from t0(X) = max(ban on X, node4's ban answer on X) +
  GRACE to the window's end: no services.discover naming node4, no rejected
  contract pushed by node4; node1, the expelling node, logged node4's ban
  answer, since it pushes the ban to node4 until node4 answers
  (mod/user/src/expel.go Expel); and on node1, and on another member that
  logged the answer, no new link with node4 after the answer + GRACE and
  none listed at the end (protocols/user/README.md:142-152,
  protocols/nodes/README.md Link admission; plan conflict C3: links are
  admitted until the answer); node4 logs no services.discover naming a
  member from its own removal + GRACE.
"""
from lib import meshapps
from lib.sessionio import load

NAMES = ["node1", "node2", "node3", "node4"]
MEMBERS = ["node1", "node2", "node3"]
PREFIX = "f4"
# why a grace: a push, a discovery or a call already in flight when the ban
# lands ends after it, and only a new one is a failure
GRACE = 1.0
# why 15 s: node1 republishes its member set on the ban, and
# services-swarm-membership bounds a member change on an open follow to 15 s
FOLLOW = 15.0
# why 10: a 30 s window at 1 call per second; fewer means the app stalled
MIN_CALLS = 10
BAN_ANSWERED = " answered its ban, refusing its links"
LINK_ADDED = ("added ", "-link with ")
PUSH_REJECTED = "rejecting pushed contract from "
LEFT = "this node is expelled"
CUT = ("route_not_found",)


def first(entries: list, since: float, *needles: str) -> float | None:
    for e in entries:
        if e["t"] >= since and all(n in e["text"] for n in needles):
            return e["t"]
    return None


def bans(f: dict, ids: dict) -> dict:
    """When the ban landed on each node, from its own log."""
    logs, since, id4 = f["logs"], f["expel_at"] - 1.0, ids["node4"]
    out = {"node1": first(logs["node1"], since, "expelled", id4),
           "node2": first(logs["node2"], since, "removing sibling", id4),
           "node3": first(logs["node3"], since, "removing sibling", id4),
           "node4": first(logs["node4"], since, LEFT)}
    missing = [n for n, t in out.items() if t is None]
    assert not missing, (f"{missing} logged no ban of node4 (want node1 'expelled', "
                         f"node2/node3 'removing sibling', node4 '{LEFT}'): {out}")
    return out


def judge_members(logs: dict, app: dict, f: dict) -> None:
    """The six pairs among node1-node3 answered every call after the expel."""
    for c in MEMBERS:
        for p in MEMBERS:
            if c == p:
                continue
            got = meshapps.window(logs[c], app[p], f["expel_at"], f["hold_end"])
            bad = [e for e in got if e["outcome"] != "ok"]
            assert not bad, (f"{c}'s app -> {p}'s app failed after the expel: "
                             f"{[(round(e['t0'] - f['expel_at'], 2), e['outcome']) for e in bad][:10]}")
            assert len(got) >= MIN_CALLS, (
                f"{c}'s app made {len(got)} calls to {p}'s app in the window; want ≥ {MIN_CALLS}")


def cut_off(outcome: str) -> bool:
    return outcome in CUT or outcome.startswith("rejected:")


def judge_cut(logs: dict, app: dict, f: dict, ban: dict) -> None:
    """node4's app is unreachable from the members, and reaches none."""
    for c in MEMBERS:
        got = meshapps.window(logs[c], app["node4"], ban[c] + GRACE, f["hold_end"])
        assert got, f"{c}'s app made no call to node4's app after the ban landed on {c}"
        bad = [e for e in got if not cut_off(e["outcome"])]
        assert not bad, (
            f"{c}'s app -> node4's app after the ban on {c}: want route_not_found or "
            f"rejected, got {[(round(e['t0'] - ban[c], 2), e['outcome']) for e in bad][:10]}")
    for p in MEMBERS:
        got = meshapps.window(logs["node4"], app[p], ban["node4"] + GRACE, f["hold_end"])
        ok = [e for e in got if e["outcome"] == "ok"]
        assert not ok, (f"node4's app reached {p}'s app {len(ok)} times after node4 "
                        f"left the swarm, the first {round(ok[0]['t0'] - ban['node4'], 2)} s after")


def judge_follows(states: dict, app: dict, f: dict, ban: dict) -> dict:
    """{member: (removal after its ban, cause)}."""
    out = {}
    for c in MEMBERS:
        rec = (states[c].get("providers") or {}).get(app["node4"]) or {}
        removed = [r for r in rec.get("removed") or [] if r["t"] >= f["expel_at"] - GRACE]
        assert removed, f"{c}'s follow never removed node4's offering after the expel: {rec}"
        r = removed[0]
        assert r["cause"] == "member_left" or (r["cause"] == "member_lost" and r["t"] <= ban[c]), (
            f"{c}'s follow removed node4's offering with cause {r['cause']} "
            f"{r['t'] - ban[c]:+.2f} s from the ban on {c}; want member_left, or member_lost "
            "before the ban landed there")
        took = r["t"] - ban[c]
        assert took <= FOLLOW, (f"{c}'s follow removed node4's offering {took:.1f} s after "
                                f"the ban on {c}; want ≤ {FOLLOW:.0f} s")
        back = [t for t in rec.get("restored") or [] if t > r["t"]]
        assert not back, f"{c}'s follow showed node4's offering again at {back}"
        out[c] = (round(took, 2), r["cause"])
    return out


def judge_log(f: dict, ids: dict, ban: dict) -> list:
    """Nothing crosses the ban; the members whose links were judged."""
    logs, id4, judged = f["logs"], ids["node4"], []
    for x in MEMBERS:
        answer = first(logs[x], f["expel_at"] - 1.0, id4 + BAN_ANSWERED)
        # why node1 must answer: the expelling node pushes the ban to node4
        # until node4 answers (protocols/user/README.md:142-152;
        # mod/user/src/expel.go Expel, onBanAnswered)
        assert answer is not None or x != "node1", (
            "node1, the expelling node, logged no answer of node4 to its ban "
            "within the window, so its links to node4 were never refused")
        t0 = max(ban[x], answer or ban[x]) + GRACE
        late = [e for e in logs[x] if e["t"] >= t0 and id4 in e["text"]
                and ("services.discover" in e["text"] or PUSH_REJECTED + id4 in e["text"])]
        assert not late, (f"{x} logged a discovery or a rejected push of node4 after the ban: "
                          f"{[(round(e['t'] - t0, 2), e['text'][:200]) for e in late][:5]}")
        if answer is None:
            continue
        judged.append(x)
        relinked = [e for e in logs[x] if e["t"] >= answer + GRACE and id4 in e["text"]
                    and all(n in e["text"] for n in LINK_ADDED)]
        assert not relinked, (f"{x} added a link with node4 after node4 answered its ban there: "
                              f"{[(round(e['t'] - answer, 2), e['text'][:200]) for e in relinked]}")
        assert not f["links"][x], f"{x} still listed links {f['links'][x]} to node4 at the end"
    since = ban["node4"] + GRACE
    late = [e for e in logs["node4"] if e["t"] >= since and "services.discover" in e["text"]
            and any(ids[m] in e["text"] for m in MEMBERS)]
    assert not late, (f"node4 logged a discovery with a member after its removal: "
                      f"{[(round(e['t'] - since, 2), e['text'][:200]) for e in late][:5]}")
    return judged


def main():
    doc = load()
    nodes, f = doc["nodes"], doc["facts"]["expel_clean"]
    ids = {n: nodes[n]["identity"] for n in NAMES}
    assert f.get("ready_at") is not None, (
        f"the 12 pairs did not all answer before the expel; missing {f.get('missing')}")
    for node, state in f["taps"].items():
        assert state["refused"] is None and state["broken"] is None, (
            f"{node}'s log tap is incomplete: {state}")

    states = meshapps.states(PREFIX, NAMES)
    app = meshapps.ids(states)
    logs = {n: meshapps.calls(PREFIX, n) for n in NAMES}
    ban = bans(f, ids)
    judge_members(logs, app, f)
    judge_cut(logs, app, f, ban)
    follows = judge_follows(states, app, f, ban)
    judged = judge_log(f, ids, ban)
    landed = ", ".join(f"{n} {t - f['expel_at']:+.2f} s" for n, t in ban.items())
    print(f"oracle: ban landed {landed}")
    print(f"oracle: node1-node3 kept answering each other; node4's app cut off both ways; "
          f"follows removed node4 (s after the ban, cause) {follows}; links judged on "
          f"{judged}")


main()
