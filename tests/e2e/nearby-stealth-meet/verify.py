#!/usr/bin/env python3
"""Oracle: two stealth nodes of one User that hold no contract of each other find, verify and link each other.

Before the contact: the reflector was claimed for the User, it listed itself
alone, and neither node1 nor node2 listed it. Afterwards node1 and the
reflector list each other linked and node2 and the reflector list each other.
The contact ran through nearby: a node logged `introduced to nearby peer` for
the other, and node1 and node2 each acted on a PeerSeen of the reflector,
which they cached unowned before its claim: the user of a cached node changed. Both broadcast stealth: node1's and node2's entry of the reflector,
and the reflector's entry of node1, carry a stealth hint and no public
profile, and no node stores a mode other than stealth or none.
"""
import re

from lib import nearby
from lib.sessionio import load

# mod/user/src/peer_seen.go contactPeer
INTRODUCED = re.compile(r"introduced to nearby peer ([0-9a-f]{66}) at (\S+)$")
REFUSED = re.compile(r"not introducing to nearby peer ([0-9a-f]{66}) at (\S+): (.*)$")
# mod/user/src/peer_seen.go onPeerSeen: every line it logs for a peer of the User
ACTED = re.compile(r"(?:introduced to|not introducing to|introducing to) nearby peer ([0-9a-f]{66})"
                   r"|nearby peer ([0-9a-f]{66}) (?:is a linked sibling|was contacted recently)"
                   r"|sibling ([0-9a-f]{66}) is nearby at")


def check_claim(f: dict, ids: dict) -> None:
    assert f["claimed"] == f["user"], (
        f"the reflector was claimed for {f['claimed']}, not the User {f['user']}")
    b = f["before"]
    assert b["reflector"] is not None and set(b["reflector"]) == {ids["reflector"]}, (
        f"the reflector listed {b['reflector']} right after its claim; want itself alone")
    for k in ("node1", "node2"):
        assert b[k] is not None and ids["reflector"] not in b[k], (
            f"{k} listed the reflector before any contact: {b[k]}")


def check_met(f: dict, ids: dict) -> None:
    a = f["after"]
    r, n1, n2 = ids["reflector"], ids["node1"], ids["node2"]
    assert f["met"], (
        f"within {f['converged']}s: node1 lists the reflector "
        f"{(a['node1'] or {}).get(r, 'not at all')} (linked?), the reflector "
        f"lists node1 {(a['reflector'] or {}).get(n1, 'not at all')}, node2 "
        f"{'lists' if r in (a['node2'] or {}) else 'does not list'} the reflector, "
        f"the reflector {'lists' if n2 in (a['reflector'] or {}) else 'does not list'} node2; "
        f"contact lines: {f['log']}")


def check_contact(f: dict, ids: dict) -> None:
    pairs = {("reflector", ids["node1"]), ("reflector", ids["node2"]),
             ("node1", ids["reflector"]), ("node2", ids["reflector"])}
    hits = [(k, m.group(1)) for k, lines in f["log"].items() for e in lines
            if (m := INTRODUCED.search(e["text"]))]
    assert any(h in pairs for h in hits), (
        "no node logged `introduced to nearby peer` for the other: the pair met "
        f"some other way, or the record is incomplete ({f['taps']}); "
        f"lines: {f['log']}")


def check_user_change(f: dict, ids: dict) -> None:
    """node1 and node2 cached the reflector unowned; its claim alone must reach their user module."""
    for k in ("node1", "node2"):
        acted = {next(g for g in m.groups() if g) for e in f["log"][k]
                 if (m := ACTED.search(e["text"]))}
        assert ids["reflector"] in acted, (
            f"{k} logged no contact step for the reflector after its claim: nearby "
            "emitted no PeerSeen when the user of its cached entry changed; "
            f"lines: {f['log'][k]}")


def check_stealth(f: dict) -> None:
    for name, s in f["entries"].items():
        assert s is not None, f"{name}: no verified entry; the pair never recognised each other"
        assert nearby.HINT in s["types"] and nearby.PROFILE not in s["types"], (
            f"{name}: the status carries {s['types']}; a stealth node sends a "
            "stealth hint and no public profile")
    for k, m in f["modes"].items():
        assert m in (None, nearby.STEALTH), f"{k} stores nearby mode {m}; the test wants stealth"


def main():
    doc = load()
    ids = {k: doc["nodes"][k]["identity"] for k in ("node1", "node2", "reflector")}
    f = doc["facts"]["nearby_stealth"]
    assert f["user"] == doc["facts"]["nearby_user"]["identity"], "the driver claimed for another User"

    check_claim(f, ids)
    check_met(f, ids)
    check_contact(f, ids)
    check_user_change(f, ids)
    check_stealth(f)
    print(f"oracle: the reflector, claimed with the User's words, and node1 met in "
          f"stealth through nearby within {f['converged']}s, and node2 joined them")


main()
