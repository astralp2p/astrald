#!/usr/bin/env python3
"""Oracle: a sibling heard again on the LAN is relinked within seconds, though its link task waits out a long backoff.

The outage took: when the driver began, node1 held no link to node2, and
node2's entry left node1's nearby cache before coming back. Then, on node1 or
on node2 (each maintains a link to the other, and whichever hears the other
first may relink), the node logged the wake (`sibling X is nearby at IP,
linking now`) and, within WITHIN seconds of it on its own clock, `link to X
restored after N attempts`, with:

- a real backoff: the wait after N failures, min(2^(N-1), 900) s, is at least
  MIN_BACKOFF s (mod/user/src/maintain_link_task.go linkRetryMin/Max);
- no natural retry: the success came before the earliest time the backoff
  alone allows for attempt N+1, counted from an anchor line `still trying to
  reconnect to X (attempt M)`, logged as attempt M+1 starts, M < N: the sum of
  the waits after failures M+1..N, every attempt taking no time. Only a wake
  ends a wait sooner.

node1 saw the link come back within WITHIN + POLL_SLACK seconds of hearing
node2 again, on the host's clock.
"""
import re

from lib.sessionio import load

WITHIN = 15.0          # "within seconds of being heard"
MIN_BACKOFF = 60.0     # a backoff past this is the threshold the test forces
POLL_SLACK = 3.0       # the driver polls node1 every second
CAP = 900.0            # linkRetryMax

ID = r"([0-9a-f]{66})"
WAKE = re.compile(rf"sibling {ID} is nearby at (\S+), linking now$")
RESTORED = re.compile(rf"link to {ID} restored after (\d+) attempts$")
ANCHOR = re.compile(rf"still trying to reconnect to {ID} \(attempt (\d+)\)$")


def wait_after(k: int) -> float:
    """The backoff after failure k: 1 s doubled per failure, capped."""
    return min(2.0 ** (k - 1), CAP)


def earliest(anchor_t: float, m: int, n: int) -> float:
    """The earliest attempt N+1 can start on its own, from attempt M+1's start."""
    return anchor_t + sum(wait_after(k) for k in range(m + 1, n + 1))


def lines(entries: list, rx: re.Pattern, peer: str) -> list:
    return [(e["t"], m) for e in entries if (m := rx.search(e["text"])) and m.group(1) == peer]


def proof(entries: list, peer: str) -> tuple:
    """(True, how) when this node's log shows a woken relink to peer, else (False, why not)."""
    wakes = lines(entries, WAKE, peer)
    if not wakes:
        return False, "no wake line"
    restored = [(t, int(m.group(2))) for t, m in lines(entries, RESTORED, peer)]
    anchors = [(t, int(m.group(2))) for t, m in lines(entries, ANCHOR, peer)]
    why = []
    for tw, wm in wakes:
        for tr, n in restored:
            if not 0.0 <= tr - tw <= WITHIN:
                continue
            if wait_after(n) < MIN_BACKOFF:
                why.append(f"restored after {n} attempts: a {wait_after(n):.0f}s backoff "
                           f"is under {MIN_BACKOFF:.0f}s")
                continue
            bounds = [earliest(ta, m, n) for ta, m in anchors if ta <= tw and m < n]
            if not bounds:
                why.append(f"restored after {n} attempts, and no anchor line before the wake")
                continue
            if tr < max(bounds):
                return True, (f"woken on hearing the peer at {wm.group(2)}, linked {tr - tw:.1f}s later after "
                              f"{n} failed attempts; a {wait_after(n):.0f}s backoff would "
                              f"have retried {max(bounds) - tr:.0f}s later at the earliest")
            why.append(f"restored {tr - tw:.1f}s after the wake, but a retry could have "
                       f"fallen due {tr - max(bounds):.0f}s before it")
    return False, "; ".join(why) or (
        f"no `restored` line within {WITHIN:.0f}s of a wake (restored: {restored})")


def check_outage(f: dict) -> tuple:
    """(t node2 was heard again, t the link was back), the host clock, s since the driver began."""
    assert not f["first"]["links"], (
        f"node1 still held a link to node2 when the driver began: {f['first']['links']}; "
        "the outage did not take")
    tl = f["timeline"]
    gone = next((i for i, s in enumerate(tl) if s["nonce"] is None), None)
    assert gone is not None, (
        f"node2's entry never left node1's nearby cache in {f['watched']}s: "
        "nearby could report it back only at a new IP")
    heard = next((s for s in tl[gone:] if s["nonce"] is not None), None)
    assert heard is not None, f"node1 never heard node2 again in {f['watched']}s"
    assert f["back_at"] is not None, (
        f"node1 held no link to node2 again in {f['watched']}s; heard it at {heard['t']}s")
    return heard["t"], f["back_at"]


def main():
    doc = load()
    f = doc["facts"]["nearby_relink"]
    ids = {k: doc["nodes"][k]["identity"] for k in ("node1", "node2")}
    for k, s in f["taps"].items():
        assert not s["refused"] and not s["broken"], f"{k}'s log record is incomplete: {s}"

    heard, back = check_outage(f)
    # why either order: one poll reads the list before the links, so a link
    # that forms between the two reads is seen a poll before node2's entry
    assert abs(back - heard) <= WITHIN + POLL_SLACK, (
        f"node1 listed the link {back - heard:.1f}s after it heard node2 again "
        f"({heard}s -> {back}s); want within {WITHIN:.0f}s")

    found = {"node1": proof(f["log"]["node1"], ids["node2"]),
             "node2": proof(f["log"]["node2"], ids["node1"])}
    assert any(ok for ok, _ in found.values()), (
        "neither node's log shows a link task woken out of its backoff: "
        + "; ".join(f"{k}: {how}" for k, (_, how) in found.items()))

    side, (_, how) = next((k, v) for k, v in found.items() if v[0])
    print(f"oracle: node2 back after {f['outage']}s off the LAN; relinked "
          f"{back - heard:.1f}s after node1 heard it; {side} {how}")


main()
