#!/usr/bin/env python3
"""Oracle: with two links to a peer and one of them dark, queries answer over
the live link, the dark link closes within N + T, and no query waits for the
routing deadline.

N is 4 s and T 10 s on both nodes (test.toml). From the `prefer_live` fact:

- each dark link closed on node1 within N + T + 2 = 16 s of the last byte
  its relay forwarded (astral-docs topics/link-mux.md:180-208, Keepalive:
  a link whose peer stopped responding closes within N + T of its last
  frame, plus the slack of :244-253, Detection bound);
- phase A, the older link dark: every query answered `ok` within 2 s, and at
  least 5 answered while node1 still listed the dark link. The spec states
  no rule for choosing among duplicate links (plan conflict C1); astrald
  routes a new query over the newest link that is not suspect
  (mod/nodes/src/link_pool.go:192-267, SelectLinkWith, betterLink);
- phase B, the newest link dark: every query ended `ok` or `unavailable`,
  none `timeout` (core-definitions/query.md:54-59: a query whose link closed
  while it waited is unavailable), each either answered `ok` within 2 s or
  ended within 16 s of the black-hole; every query started from black-hole
  + N + 1 s until the dark link closed answered `ok` within 2 s, at least 3
  of them (code: a link is suspect once a probe is due or in flight,
  mod/nodes/src/link.go Suspect, and a suspect link loses to a live one);
- after each close, every query answered `ok`.
"""
from lib.sessionio import load

N = 4.0
T = 10.0
CLOSE = N + T + 2.0
FAST = 2.0
MIN_A = 5
MIN_B = 3


def ms(q: dict) -> float:
    return q["end"] - q["start"]


def fast_ok(q: dict) -> bool:
    return q["outcome"] == "ok" and ms(q) < FAST


def show(qs: list, ph: dict) -> list:
    return [(round(q["start"] - ph["blackhole_at"], 2), q["outcome"], round(ms(q), 2))
            for q in qs][:10]


def judge_close(name: str, ph: dict) -> float:
    assert ph.get("closed_at") is not None, f"phase {name}: node1 never closed the dark link"
    took = ph["closed_at"] - ph["last_frame_at"]
    assert took <= CLOSE, (f"phase {name}: node1 closed the dark link {took:.1f} s after its "
                           f"last frame; want ≤ {CLOSE:.0f} s (N + T + 2)")
    late = [q for q in ph["queries"] if q["start"] >= ph["closed_at"] and q["outcome"] != "ok"]
    assert not late, f"phase {name}: queries after the close that failed: {show(late, ph)}"
    return took


def judge_a(ph: dict) -> int:
    slow = [q for q in ph["queries"] if not fast_ok(q)]
    assert not slow, (f"phase A: queries not answered ok within {FAST:.0f} s while the older "
                      f"link was dark: {show(slow, ph)}")
    before = [q for q in ph["queries"] if q["end"] < ph["closed_at"]]
    assert len(before) >= MIN_A, (f"phase A: {len(before)} queries answered while node1 still "
                                  f"listed the dark link; want ≥ {MIN_A}")
    return len(before)


def judge_b(ph: dict) -> int:
    qs, bh = ph["queries"], ph["blackhole_at"]
    odd = [q for q in qs if q["outcome"] not in ("ok", "unavailable")]
    assert not odd, f"phase B: queries that ended neither ok nor unavailable: {show(odd, ph)}"
    hung = [q for q in qs if not fast_ok(q) and q["end"] > bh + CLOSE]
    assert not hung, (f"phase B: queries that ended over {CLOSE:.0f} s after the black-hole: "
                      f"{show(hung, ph)}")
    window = [q for q in qs if bh + N + 1.0 <= q["start"] < ph["closed_at"]]
    slow = [q for q in window if not fast_ok(q)]
    assert not slow, (f"phase B: queries started once the dark link was suspect, before it "
                      f"closed, not answered ok within {FAST:.0f} s: {show(slow, ph)}")
    assert len(window) >= MIN_B, (f"phase B: {len(window)} queries between suspect and close; "
                                  f"want ≥ {MIN_B}")
    return len(window)


def main():
    f = load()["facts"]["prefer_live"]
    a, b = f["phase_a"], f["phase_b"]
    assert a and b, f"the driver did not finish both phases: {f}"
    close_a, close_b = judge_close("A", a), judge_close("B", b)
    over_a, over_b = judge_a(a), judge_b(b)
    print(f"oracle: phase A {over_a} queries answered over the live link while the dark older "
          f"link was listed, closed {close_a:.1f} s after its last frame; phase B {over_b} "
          f"queries moved off the suspect newest link, closed {close_b:.1f} s after its last frame")


main()
