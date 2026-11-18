#!/usr/bin/env python3
"""Oracle: a fresh node joins a Stealth swarm node on its LAN with user.join alone.

Before the join the reflector was unclaimed with a manager and resolved no
endpoint for node1, and node1 stores no nearby mode other than Stealth. The
manager's user.join toward node1 answered a contract the User issued to the
reflector, and the reflector is claimed by the User.
"""
from lib import nearby
from lib.sessionio import load

# why: the route wait is 3 s (mod/user/src/op_join.go joinWait); the rest is
# one link and the admission round trip on a LAN
JOIN_BOUND = 30.0


def main():
    doc = load()
    n1, r = doc["nodes"]["node1"], doc["nodes"]["reflector"]
    user = doc["facts"]["nearby_user"]["identity"]
    f = doc["facts"]["nearby_join"]

    assert f["fresh"] == "fresh" and f["unclaimed"] == "unclaimed", (
        f"the reflector went {f['fresh']} → {f['unclaimed']}; want fresh → unclaimed")
    assert f["endpoints_before"] == [], (
        f"before the join the reflector resolved {f['endpoints_before']} for node1; "
        "want none, so that only the join could find node1")
    assert f["node1_mode"] in (None, nearby.STEALTH), (
        f"node1 stores nearby mode {f['node1_mode']}; the test wants Stealth")

    j = f["joined"]
    assert "error" not in j, (
        f"user.join toward the Stealth node1 failed after {f['join_seconds']}s: {j['error']}")
    assert j["issuer"] == user, f"the membership contract's issuer is {j['issuer']}, not the User {user}"
    assert j["subject"] == r["identity"], (
        f"the membership contract names {j['subject']}, not the reflector {r['identity']}")
    assert f["join_seconds"] < JOIN_BOUND, (
        f"user.join took {f['join_seconds']}s; want under {JOIN_BOUND:.0f}s")

    a = f["after"]
    assert a["state"] == "claimed" and a["user"] == user, (
        f"after the join the reflector is {a['state']} under {a['user']}; want claimed by {user}")

    print(f"oracle: the fresh reflector joined node1's swarm with user.join alone in "
          f"{f['join_seconds']}s, with no endpoint before and nothing run on node1 "
          f"(node {n1['identity'][:12]}…)")


main()
