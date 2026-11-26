#!/usr/bin/env python3
"""Oracle: while a peer is frozen, calls to it end within the keepalive bound
and the other apps keep answering; after the thaw every pair answers again,
and the whole churn chain recovered within its bounds.

From the `survives_churn` fact and the `f3` apps' records (the callers are
the apps on node1, node2, node5 and the late app on node1):

- every call to node3's app in flight at any moment of the first 15 s of
  the freeze ended within 27 s of the freeze as `unavailable`, and each
  caller made at least one such call: the keepalive closes a link whose peer
  stopped within N + T of the last frame received, 25 s at tcp's defaults,
  plus slack, and never earlier (astral-docs topics/link-mux.md:180-208,
  Keepalive; :244-253, Detection bound; D11 tcp 27 s), so these calls still
  wait on the link when it closes, and a query whose link closed while it
  waited for its answer is unavailable (core-definitions/query.md:54-59).
  A call whose answer node3 sent before it stopped ends `ok` within 1 s of
  the freeze;
- no call to node3's app lasted over 62 s: a call started after the links
  closed may end `timeout` at the 60 s routing deadline
  (core-definitions/query.md:60-62; topics/app-routing.md:131-146, "timeout:
  host behind a dead Link"; plan conflict C4);
- the 12 pairs that do not involve node3 answered every call started during
  the freeze;
- every pair of the five apps answered within 60 s of the thaw (D11), and
  each pair's last call answered `ok`;
- the chain's recovery table: each step's recovery from its event marker,
  from the apps' call logs. The join (from the join's answer) and the freeze
  (from the thaw) are held to 60 s here; the restart and late-app steps have
  only their driver's end as a marker, before the harness's restart, so
  their rows are printed, and each step's own oracle held it to 60 s from
  the node's return;
- the oracle stops every `f3` app.
"""
from lib import apps, meshapps
from lib.sessionio import load

NAMES = ["node1", "node2", "node3", "node5", "late"]
CALLERS = ["node1", "node2", "node5", "late"]
ALL = ["node1", "node2", "node3", "node4", "node5", "late"]
PREFIX = "f3"
BOUND = 27.0
# why 15 s: a link to node3 closes no earlier than N + T after node3's last
# frame, 20 s on kcp and 25 s on tcp (topics/link-mux.md:220-224, :244-253);
# the apps' 1 Hz calls carry a frame from node3 about every second, so a call
# started in the first 15 s of the freeze is in flight when its link closes
OPEN = 15.0
# why 1 s: an answer node3 wrote before SIGSTOP still reaches the caller
SENT = 1.0
DEADLINE = 62.0
RECOVER = 60.0


def judge_freeze(logs: dict, app3: str, f: dict) -> list:
    """The calls to node3's app in flight in the first OPEN seconds of the
    freeze; each ended promptly, and each caller made one."""
    judged = []
    for c in CALLERS:
        mine = []
        for e in logs[c]:
            if e["to"] != app3 or e["t1"] <= f["freeze_at"]:
                continue
            took = e["t1"] - e["t0"]
            assert took <= DEADLINE, (f"{c}'s call to node3's app lasted {took:.1f} s "
                                      f"({e['outcome']}); want ≤ {DEADLINE:.0f} s")
            if e["t0"] >= f["freeze_at"] + OPEN:
                continue
            ended = e["t1"] - f["freeze_at"]
            sent = e["outcome"] == "ok" and ended <= SENT
            assert sent or (e["outcome"] == "unavailable" and ended <= BOUND), (
                f"{c}'s call to node3's app started {e['t0'] - f['freeze_at']:.1f} s after "
                f"the freeze ended {e['outcome']} {ended:.1f} s after it; want unavailable "
                f"within {BOUND:.0f} s")
            mine.append((c, e["outcome"], round(ended, 1)))
        assert mine, (f"{c}'s app made no call to node3's app in the first "
                      f"{OPEN:.0f} s of the freeze")
        judged += mine
    return judged


def judge_others(states: dict, logs: dict, f: dict) -> None:
    others = [n for n in NAMES if n != "node3"]
    for c, p in meshapps.pairs(others):
        got = meshapps.window(logs[c], states[p]["app"], f["freeze_at"], f["thaw_at"])
        bad = [e for e in got if e["outcome"] != "ok"]
        assert not bad, (f"{c}'s app -> {p}'s app failed while node3 was frozen: "
                         f"{[(round(e['t0'] - f['freeze_at'], 1), e['outcome']) for e in bad][:5]}")
        assert got, f"{c}'s app made no call to {p}'s app while node3 was frozen"


def judge_end(states: dict, logs: dict, f: dict) -> float:
    assert f["recovered_at"] is not None, (
        f"pairs not answering within {RECOVER:.0f} s of the thaw: {f['missing']}")
    took = f["recovered_at"] - f["thaw_at"]
    assert took <= RECOVER, f"every pair answered {took:.1f} s after the thaw; want ≤ {RECOVER:.0f} s"
    for c, p in meshapps.pairs(NAMES):
        to = [e for e in logs[c] if e["to"] == states[p]["app"]]
        last = max(to, key=lambda e: e["t1"], default=None)
        assert last and last["outcome"] == "ok", (
            f"{c}'s app's last call to {p}'s app ended {last and last['outcome']}")
    return took


def table(states: dict, logs: dict, facts: dict) -> dict:
    """{step: seconds} from each step's marker; None where a pair never answered."""
    def since(names, t):
        at = meshapps.recovered_at(states, logs, names, t)
        return None if at is None else round(at - t, 1)

    four = ["node1", "node2", "node3", "node4"]
    setup, join = facts["churn_setup"], facts["churn_join"]
    leave = facts["churn_leave"]
    removed = [r["t"] for n in NAMES
               for r in ((states[n].get("providers") or {}).get(leave["app"]) or {}).get("removed") or []
               if r["t"] >= leave["driver_end"]]
    return {"setup": round(setup["ready_at"] - setup["spawned_at"], 1),
            "restart (from the driver's end)": since(four, facts["churn_restart"]["driver_end"]),
            "join": round(join["ready_at"] - join["joined_at"], 1),
            "late app (from the driver's end)": since(ALL, facts["churn_late_app"]["driver_end"]),
            "leave (last removal of node4)": round(max(removed) - leave["driver_end"], 1) if removed else None,
            "freeze (from the thaw)": round(facts["survives_churn"]["recovered_at"]
                                            - facts["survives_churn"]["thaw_at"], 1)}


def main():
    facts = load()["facts"]
    f = facts["survives_churn"]
    states = meshapps.states(PREFIX, ALL)
    logs = {n: meshapps.calls(PREFIX, n) for n in ALL}
    try:
        judged = judge_freeze(logs, states["node3"]["app"], f)
        judge_others(states, logs, f)
        took = judge_end(states, logs, f)
        rows = table(states, logs, facts)
        print("oracle: recovery per churn step, seconds:")
        for step, s in rows.items():
            print(f"  {step:34} {s}")
        for step in ("join", "freeze (from the thaw)"):
            assert rows[step] <= RECOVER, f"{step} recovered in {rows[step]} s; want ≤ {RECOVER:.0f} s"
    finally:
        meshapps.stop(PREFIX, [n for n in ALL if n != "node4"])
    print(f"oracle: calls to node3's app in the first {OPEN:.0f} s of the freeze {judged}; "
          f"the 12 pairs without node3 "
          f"answered throughout; every pair answered {took:.1f} s after the thaw")


main()
