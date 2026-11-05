#!/usr/bin/env python3
"""Oracle: a Silent node sends no ether datagram, and still hears its peers.

The reflector stored Silent and logged `mode 0`. From that line on, through a
window that holds the reflector's own nearby.broadcast and nearby.scan, two
rounds of scans from node1 and node2, and one full status interval, the
capture on the reflector's own interfaces saw no UDP 8822 datagram leave the
reflector, while it saw node1's and node2's arrive. node1 and node2 kept the
reflector's last status from before the switch, while each refreshed the
other after its scan; the reflector refreshed both.

What this does not prove: what the reflector sends on a port other than
8822, or outside the window.
"""
import re

from lib.sessionio import load

MODE = re.compile(r"mode (\d+)$")   # mod/nearby/src/send.go runPeriodic
# why a grace: a scan reply composed in the old mode may leave a moment after
# the switch is logged; any later send is the Silent node's own
GRACE = 0.5
# why: the window has to hold the reflector's next periodic status, at most
# 95 s after the switch
COVER = 100.0


def acked(a: dict) -> bool:
    return not a["refused"] and "ack" in a["types"]


def switched_at(f: dict) -> float:
    lines = [(e["t"], int(m.group(1))) for e in f["mode_log"]
             if (m := MODE.search(e["text"]))]
    assert acked(f["switch"]), f"tree.set of the reflector's mode answered {f['switch']}"
    assert f["stored"] == 0, f"the reflector stores nearby mode {f['stored']}, not Silent (0)"
    silent = [t for t, m in lines if m == 0]
    assert silent, (f"the reflector never logged `mode 0` after the switch "
                    f"(mode lines: {lines}; tap {f['tap']})")
    return silent[-1]


def check_capture(f: dict, t_mode: float, ips: dict) -> None:
    cap = f["capture"]
    assert "error" not in cap, f"the capture left no record: {cap['error']}"
    assert cap["start"] <= t_mode, (
        f"the capture began {cap['start'] - t_mode:.1f}s after the switch; it "
        "has to hold the switch itself")
    assert cap["end"] >= t_mode + COVER, (
        f"the capture ended {cap['end'] - t_mode:.1f}s after the switch; it has "
        f"to hold one status interval ({COVER:.0f}s)")
    sent = [s for s in cap["sent"] if s["t"] > t_mode + GRACE]
    assert not sent, (
        f"the Silent reflector sent {len(sent)} ether datagram(s) after the "
        f"switch: {sent[:5]}")
    assert cap["sent_total"] == len(cap["sent"]) or cap["sent"][-1]["t"] <= t_mode + GRACE, (
        f"the record kept {len(cap['sent'])} of {cap['sent_total']} sent datagrams "
        "and cannot show the rest were before the switch")
    for k in ("node1", "node2"):
        h = cap["heard"].get(ips[k])
        assert h and h["last"] > t_mode, (
            f"the capture heard nothing from {k} ({ips[k]}) after the switch "
            f"(heard: {cap['heard']}); it shows nothing about a window it did not see")


def check_views(f: dict) -> None:
    v = f["views"]
    base = v["after_switch"]
    for k in ("node1", "node2"):
        held = base[k]["reflector"]
        for step in ("round1", "round2"):
            now = v[step][k]["reflector"]
            assert now is None or now == held, (
                f"{k} holds a status of the reflector newer than the switch "
                f"at {step} ({held} -> {now})")
    for a, b in (("node1", "node2"), ("node2", "node1")):
        assert v["round1"][a][b] and v["round1"][a][b] != base[a][b], (
            f"{a} did not refresh {b}'s status after scanning: the scans "
            "reached nothing, so the reflector's silence shows nothing")
    for k in ("node1", "node2"):
        assert v["round2"]["reflector"][k] and v["round2"]["reflector"][k] != base["reflector"][k], (
            f"the Silent reflector stopped hearing {k}: Silent sends nothing and "
            "still receives")


def main():
    doc = load()
    f = doc["facts"]["nearby_silent"]
    ips = {k: doc["nodes"][k]["lan_endpoint"].split(":")[1] for k in ("node1", "node2")}

    t_mode = switched_at(f)
    for op, a in f["own"].items():
        assert acked(a), f"nearby.{op} on the Silent reflector answered {a}; want a no-op ack"
    check_capture(f, t_mode, ips)
    check_views(f)
    print(f"oracle: the Silent reflector sent no ether datagram in "
          f"{f['capture']['end'] - t_mode:.0f}s after the switch, and still heard both peers")


main()
