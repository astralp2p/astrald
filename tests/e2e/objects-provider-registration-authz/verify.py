#!/usr/bin/env python3
"""Oracle: node1 accepts a provider registration exactly where the grant
covers the role, and dials exactly the registrations it accepted.

Four judgements.

In `drive` mode the driver records every reply and dial count and stops only
on a wiring fault; the verdicts are the oracle's.

1. The matrix. The oracle holds its own table of which (app, role) cells a
   grant covers, and reads every reply the driver recorded against it: a 10 s
   lease for a covered cell, exactly `not permitted` for every other. An exact
   text, because a timeout or a lost route is also a failure and says nothing
   about the grant check.

2. The node token and the network. node1's own token is refused, with
   `not permitted` or the self-registration error; which one is recorded, not
   pinned (the self branch runs after the grant check). node2's token over the
   link is refused with exactly `origin not allowed: network`: without the
   origin guard node2 would read `not permitted`, so the text is what proves
   the guard fired first.

3. The dials. node1's find, describe and search dialed each accepted cell once
   and every other (app, role) pair never. The accepted cells being dialed is
   the control that gives the zeros their meaning.

4. On its own. The oracle checks live, with astral-py, that an app minted
   with no grant and node1's own token are both refused by
   `objects.register_finder`, then reruns the whole matrix with fresh apps
   (`driver verify`).
"""
import asyncio
import os
import subprocess
import sys
from pathlib import Path

import astral
from astral.errors import AstralError

from lib.sessionio import load

ROOT = Path(__file__).resolve().parents[3]
DEADLINE = 15

NOT_PERMITTED = "not permitted"
SELF_REFUSAL = "node identity cannot register as an external discoverer"
ORIGIN_REFUSAL = "origin not allowed: network"
LEASE = "lease 10s"

ROLES = ("finder", "describer", "searcher")
# The roles each app's grant covers: the oracle's own table, not the driver's.
COVERED = {
    "all": {"finder", "describer", "searcher"},  # ServeObjects, unconstrained
    "d": {"describer"},
    "f": {"finder"},
    "s": {"searcher"},
    "df": {"describer", "finder"},
    "i": set(),        # ServeObjects limited to the indexer role
    "store": set(),    # StoreObjects only
    "none": set(),     # no permit beyond what every registration gets
}


def judge_matrix(f: dict) -> None:
    wrong = []
    for app, covered in COVERED.items():
        for role in ROLES:
            cell = f"{app}/{role}"
            want = LEASE if role in covered else f"error: {NOT_PERMITTED}"
            got = f["cells"].get(cell)
            if got != want:
                wrong.append(f"{cell}: {got!r}, want {want!r}")
    assert not wrong, "register replies off the grant table:\n  " + "\n  ".join(wrong)


def judge_nodes(f: dict) -> str:
    seen = set()
    for role in ROLES:
        got = f["node_self"].get(role)
        assert got in (f"error: {NOT_PERMITTED}", f"error: {SELF_REFUSAL}"), (
            f"register_{role} with node1's own token answered {got!r}; "
            f"want a refusal, {NOT_PERMITTED!r} or {SELF_REFUSAL!r}")
        seen.add(got)
        got = f["node_network"].get(role)
        assert got == f"error: {ORIGIN_REFUSAL}", (
            f"register_{role} with node2's token over the link answered "
            f"{got!r}; want exactly {ORIGIN_REFUSAL!r}, the origin guard's "
            f"refusal before any grant check")
    return ", ".join(sorted(seen))


def judge_dials(f: dict) -> None:
    wrong = []
    for app, covered in COVERED.items():
        for role in ROLES:
            cell = f"{app}/{role}"
            want = 1 if role in covered else 0
            if f["dials"].get(cell) != want:
                wrong.append(f"{cell}: dialed {f['dials'].get(cell)}, want {want}")
    assert not wrong, ("node1's fan-out dialed otherwise than the accepted "
                       "registrations once each:\n  " + "\n  ".join(wrong))


async def refused(coro) -> str:
    try:
        lease = await coro
    except AstralError as e:
        return getattr(e, "message", str(e))
    raise AssertionError(f"objects.register_finder answered a lease: {lease!r}")


async def judge_live(n1: dict, user_token: str) -> str:
    async with await astral.connect(n1["endpoint"], token=user_token) as u:
        app = await u.apphost.register(timeout=DEADLINE)
    async with await astral.connect(n1["endpoint"], token=str(app.token)) as a:
        text = await refused(a.objects.register_finder(10, timeout=DEADLINE))
    assert text == NOT_PERMITTED, (
        f"an app with no grant was refused with {text!r}; want {NOT_PERMITTED!r}")
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as n:
        text = await refused(n.objects.register_finder(10, timeout=DEADLINE))
    assert text in (NOT_PERMITTED, SELF_REFUSAL), (
        f"node1's own token was refused with {text!r}")
    return text


def main():
    doc = load()
    f = doc["facts"]["objects_provider_registration_authz"]
    judge_matrix(f)
    node_self = judge_nodes(f)
    judge_dials(f)
    failed = [k for k, ok in f["checks"].items() if not ok]
    assert not failed, f"driver checks that did not hold: {failed}"
    live_self = asyncio.run(judge_live(doc["nodes"]["node1"], doc["facts"]["user_token"]))

    proc = subprocess.run(
        ["go", "run", "./tests/e2e/objects-provider-registration-authz/driver", "verify",
         os.environ["ASTRAL_TESTS_SESSION"]],
        cwd=ROOT, capture_output=True, text=True,
        env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
    )
    sys.stderr.write(proc.stderr)
    assert proc.returncode == 0, f"the matrix with fresh apps failed: exit {proc.returncode}"

    print(f"oracle: 24 cells match the grant table, accepted cells dialed once "
          f"and no other; node1's token refused ({node_self}; live: {live_self!r}); "
          f"node2 over the link refused with {ORIGIN_REFUSAL!r}")


main()
