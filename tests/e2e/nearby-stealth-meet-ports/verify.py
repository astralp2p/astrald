#!/usr/bin/env python3
"""Oracle: two freshly claimed Stealth nodes of one User, on tcp 1800 and 1801, link through nearby within two statuses.

user.bootstrap claimed node1 and node2 for the same User, and at node2's
claim neither held a link to the other. Within LINK_BOUND seconds of node2's
claim both list one link to each other under the same id, and every link
either dialed went to the other's LAN address on a port the other listens
on, never 1791 or 1792. The contact ran through nearby: a node logged
`introduced to nearby peer` or `sibling … is nearby at` for the other. Each
node's entry of the other carries a stealth hint and sealed ports, and no
public profile and no plain ports; no node stores a mode other than Stealth
or none. Each node resolves the other at its carried tcp and kcp ports, and
at no default port.
"""
import re
import tomllib
from pathlib import Path

from lib import nearby
from lib.sessionio import load

# why: two status intervals of at most 95 s each (mod/nearby/src/send.go
# maxBroadcastInterval), and 10 s for the contact's dial and handshake
LINK_BOUND = 2 * 95 + 10
CONFIG = tomllib.loads((Path(__file__).parent / "test.toml").read_text())["config"]
PAIRS = (("node1", "node2"), ("node2", "node1"))
# mod/user/src/peer_seen.go: a contact that dials the peer
CONTACT = re.compile(r"introduced to nearby peer ([0-9a-f]{66})"
                     r"|sibling ([0-9a-f]{66}) is nearby at")


def lan_ip(node: dict) -> str:
    """The address a peer dials node at: the host of its lan_endpoint."""
    return node["lan_endpoint"].split(":")[1]


def carried(node: str, nodes: dict) -> set:
    """The endpoints node listens on at its LAN address, as test.toml moves them."""
    ip, cfg = lan_ip(nodes[node]), CONFIG[node]
    return {f"tcp:{ip}:{cfg['tcp']['listen_port']}", f"kcp:{ip}:{cfg['kcp']['listen_port']}"}


def defaults(node: str, nodes: dict) -> set:
    ip = lan_ip(nodes[node])
    return {f"tcp:{ip}:1791", f"kcp:{ip}:1792"}


def check_claims(f: dict, ids: dict) -> None:
    c = f["claims"]
    assert c["node1"] == c["node2"], (
        f"user.bootstrap claimed node1 for {c['node1']} and node2 for {c['node2']}; "
        "the same words must give one User")
    for k, other in PAIRS:
        assert f["before"][k] == [], (
            f"{k} held links to {other} at node2's claim: {f['before'][k]}")


def check_link(f: dict) -> None:
    a, b = f["links"]["node1"], f["links"]["node2"]
    assert f["linked"], (
        f"node1 and node2 shared no link within {f['waited']}s of node2's claim "
        f"(node1 holds {a}, node2 holds {b}); contact lines: {f['log']}")
    assert f["waited"] <= LINK_BOUND, (
        f"node1 and node2 linked {f['waited']}s after node2's claim; want within "
        f"two statuses, {LINK_BOUND}s")


def check_dials(f: dict, nodes: dict) -> None:
    """Every link either node dialed went to a port the other listens on."""
    for k, other in PAIRS:
        want = carried(other, nodes)
        for link in f["links"][k]:
            if not link["outbound"]:
                continue
            assert link["remote_endpoint"] in want, (
                f"{k} dialed its link {link['id']} to {other} at "
                f"{link['remote_endpoint']}; {other} listens at {sorted(want)}")


def check_contact(f: dict, ids: dict) -> None:
    hits = {(k, next(g for g in m.groups() if g)) for k, lines in f["log"].items()
            for e in lines if (m := CONTACT.search(e["text"]))}
    assert hits & {("node1", ids["node2"]), ("node2", ids["node1"])}, (
        "no node logged a nearby contact for the other: the pair met some other "
        f"way, or the record is incomplete ({f['taps']}); lines: {f['log']}")


def check_stealth(f: dict) -> None:
    for k, other in PAIRS:
        s = f["entries"][k]
        assert s is not None, f"{k} holds no verified entry of {other}: it never recognised it"
        t = s["types"]
        assert nearby.HINT in t and nearby.PROFILE not in t, (
            f"{k}'s entry of {other} carries {t}; a Stealth node sends a stealth "
            "hint and no public profile")
        assert t.count(nearby.SEALED_PORTS) == 1 and nearby.LISTEN_PORTS not in t, (
            f"{k}'s entry of {other} carries {t}; a Stealth node off the default "
            "ports sends its ports sealed, once, and never plain")
    for k, m in f["modes"].items():
        assert m in (None, nearby.STEALTH), f"{k} stores nearby mode {m}; the test wants Stealth"


def check_endpoints(f: dict, nodes: dict) -> None:
    for k, other in PAIRS:
        eps = f["endpoints"][k]
        assert isinstance(eps, list), f"nodes.resolve_endpoints for {other} on {k} failed: {eps}"
        want = carried(other, nodes)
        assert want <= set(eps), (
            f"{k} resolves {other} at {eps}; want the unsealed ports {sorted(want)} among them")
        stale = defaults(other, nodes) & set(eps)
        assert not stale, f"{k} resolves {other} at default ports it does not listen on: {sorted(stale)}"


def main():
    doc = load()
    nodes = doc["nodes"]
    ids = {k: nodes[k]["identity"] for k in ("node1", "node2")}
    f = doc["facts"]["nearby_ports_meet"]

    check_claims(f, ids)
    check_link(f)
    check_dials(f, nodes)
    check_contact(f, ids)
    check_stealth(f)
    check_endpoints(f, nodes)
    print(f"oracle: node1 (tcp 1800) and node2 (tcp 1801), claimed with one User's "
          f"words, linked through their sealed ports {f['waited']}s after the second claim")


main()
