#!/usr/bin/env python3
"""Oracle: the window admits the claim contract, from a stranger's key and from
an identity node1 minted, refuses its near misses, and node1 is still
unclaimed.

The control first: the claim contract must pass the guard and fail only at
signing, since node1 holds no key for its issuer. Without that the refusals
could be a closed window rather than a narrow one.
"""
from lib.sessionio import load

FOREIGN = "cannot sign with another identity's key"

GUARD = {
    "claim issued by the node": "authorize issuer: cannot sign with the node's key",
    "claim carrying a sudo permit": f"authorize issuer: {FOREIGN}",
    "claim valid for a century": f"authorize issuer: {FOREIGN}",
    "claim asked by an app as itself": f"authorize issuer: {FOREIGN}",
}


def main():
    window = load()["facts"]["claim_window"]
    probes = window["probes"]

    control = probes["claim, stranger issuer"]
    assert control["refused"] and "authorize" not in control["detail"] \
        and "sign as issuer: no signing scheme available for key" in control["detail"], (
            f"the claim contract answered {control['detail']} — the guard did "
            "not admit it, so the refusals below do not show a narrow window")

    for name, text in GUARD.items():
        r = probes[name]
        assert r["refused"] and text in r["detail"], f"{name}: {r['detail']}"

    r = probes["claim issued by an app"]
    assert not r["refused"], (
        f"claim issued by an app: {r['detail']} — the guard refused an issuer "
        "node1 minted")

    u = window["unclaimed_after"]
    assert u["refused"] and "no active contract" in u["detail"], (
        f"user.info answered {u['detail']} — a probe claimed node1")

    print(f"oracle: the claim contract passed the guard, an app's claim signed; "
          f"{len(GUARD)} near misses refused by it; node1 is unclaimed")


main()
