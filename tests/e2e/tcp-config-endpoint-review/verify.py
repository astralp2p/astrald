#!/usr/bin/env python3
"""Oracle: a sibling sees the configured endpoint with a 7 d TTL and reaches it."""
from lib.sessionio import load

CONFIGURED = "198.51.100.12:1791"
WEEK = 7 * 24 * 3600


def main():
    doc = load()
    f = doc["facts"]
    n2 = doc["nodes"]["node2"]

    own = [r for r in f["resolved_on_node2"] if r[:2] == ["tcp", CONFIGURED]]
    assert own and own[0][2] == WEEK, (
        f"node2 does not advertise {CONFIGURED} with TTL {WEEK}: "
        f"{f['resolved_on_node2']}")

    seen = [r for r in f["resolved_on_node1"] if r[:2] == ["tcp", CONFIGURED]]
    assert seen, f"node1 holds no row tcp:{CONFIGURED} for node2: {f['resolved_on_node1']}"
    ttl = seen[0][2]
    assert ttl is not None and WEEK - 3600 < ttl <= WEEK, (
        f"node1's row for tcp:{CONFIGURED} has TTL {ttl}, want just under {WEEK}")

    link = f["dial_link"]
    assert link["network"] == "tcp" and link["remote"] == CONFIGURED, (
        f"node1's link to node2 is not over tcp:{CONFIGURED}: {link}")
    assert link["remote_identity"] == n2["identity"], link
    print(f"oracle: node1 resolves tcp:{CONFIGURED} for node2 with TTL {ttl} "
          f"and links to it: {link}")


main()
