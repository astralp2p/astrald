#!/usr/bin/env python3
"""Oracle: a finder that loses its grant is dropped while another stays, a
refused re-registration stays refused until the grant is back, and an
unrenewed finder or searcher lease lapses and revives on re-registration.

Three independent views.

1. The answers. Each find and search names what node1 relayed. The oracle
   holds its own table of what each step must answer: pid, qid and eid are
   identities only P, Q and E name, and zx is the object only Z returns.

2. The apps' counters. P, Q and E count every objects.find node1 sent them
   for X, and Z every objects.search for the word. Q registered twice, and is
   still asked once per find: the second registration renewed the first.

3. node1's log. Every dial node1 made is a `-> <id>:objects.find` line, so
   the dial counts are read a second time from node1's side. node1 logs an
   expiry for E's finder and Z's searcher registrations, and none for Q's.

Last, `driver verify` reruns the whole flow with fresh apps.
"""
import os
import re
import subprocess
import sys
from collections import Counter
from pathlib import Path

from lib.sessionio import load

ROOT = Path(__file__).resolve().parents[3]
ANSI = re.compile(r"\x1b\[[0-9;]*m")
NOT_PERMITTED = "not permitted"

# step -> (answer, counters after it)
EXPECTED = {
    "find1":               ("pid qid",     {"P": 1, "Q": 1, "E": 0}),
    "find2_revoked":       ("qid",         {"P": 1, "Q": 2, "E": 0}),
    "find3_refused":       ("qid",         {"P": 1, "Q": 3, "E": 0}),
    "find4_regranted":     ("pid qid",     {"P": 2, "Q": 4, "E": 0}),
    "find5_e_live":        ("pid qid eid", {"P": 3, "Q": 5, "E": 1}),
    "find6_e_lapsed":      ("pid qid",     {"P": 4, "Q": 6, "E": 1}),
    "find7_e_revived":     ("pid qid eid", {"P": 5, "Q": 7, "E": 2}),
    "find8_all_revoked":   ("",            {"P": 5, "Q": 7, "E": 2}),
    "search1_z_live":      ("zx@Z",        {"Z": 1}),
    "search2_z_lapsed":    ("",            {"Z": 1}),
    "search3_z_revived":   ("zx@Z",        {"Z": 2}),
    "search4_all_revoked": ("",            {"Z": 2}),
}
LEASES = {"P": "lease 2m0s", "Q": "lease 2m0s", "Q renewal": "lease 2m0s",
          "E": "lease 3s", "E revived": "lease 1m0s",
          "Z": "lease 3s", "Z revived": "lease 1m0s"}
# the dials node1's log must show in all, per app
DIALS = {"P": 5, "Q": 7, "E": 2, "Z": 2}


def short(identity: str) -> str:
    """node1 logs an identity as its first and last four bytes."""
    return f"{identity[:8]}:{identity[-8:]}"


def judge_steps(f: dict) -> None:
    wrong = []
    for name, (answer, counts) in EXPECTED.items():
        s = f["steps"].get(name)
        if s is None:
            wrong.append(f"{name}: not run")
            continue
        got = sorted(s["answer"] or [])
        if s["err"] or got != sorted(answer.split()):
            wrong.append(f"{name}: answered {got} (err {s['err']!r}), want {sorted(answer.split())}")
        off = {k: s["counts"].get(k) for k, v in counts.items() if s["counts"].get(k) != v}
        if off:
            wrong.append(f"{name}: counters {off}, want {counts}")
    assert not wrong, "find and search answered off the table:\n  " + "\n  ".join(wrong)


def judge_registrations(f: dict) -> None:
    assert f["refused_reregister"] == NOT_PERMITTED, (
        f"P's register_finder after its grant was revoked answered "
        f"{f['refused_reregister']!r}; want {NOT_PERMITTED!r}")
    assert f["regrant_reregister"].startswith("lease "), (
        f"P's register_finder after the grant came back answered {f['regrant_reregister']!r}")
    off = {k: f["leases"].get(k) for k, v in LEASES.items() if f["leases"].get(k) != v}
    assert not off, f"register replies {off}; want {LEASES}"


def judge_log(f: dict, root: str) -> None:
    log = ANSI.sub("", (Path(root) / "astrald.log").read_text(errors="replace"))
    lines = log.splitlines()
    dials = Counter()
    for name, app in f["apps"].items():
        op, arg = ("objects.search", f["word"]) if name == "Z" else ("objects.find", f"id={f['x']}")
        dials[name] = sum(1 for ln in lines if f"-> {short(app)}:{op}" in ln and arg in ln)
    assert dict(dials) == DIALS, f"node1's log shows dials {dict(dials)}; want {DIALS}"

    def expired(kind: str, app: str) -> bool:
        return re.search(rf"external {kind} {re.escape(short(app))}: registration expired", log) is not None

    assert expired("finder", f["apps"]["E"]), "node1 logged no expiry for E's finder registration"
    assert expired("searcher", f["apps"]["Z"]), "node1 logged no expiry for Z's searcher registration"
    assert not expired("finder", f["apps"]["Q"]), (
        "node1 logged an expiry for Q's finder registration, which it kept dialing")


def main():
    doc = load()
    f = doc["facts"]["objects_provider_revoke_lease"]
    judge_steps(f)
    judge_registrations(f)
    judge_log(f, doc["nodes"]["node1"]["root"])
    failed = [k for k, ok in f["checks"].items() if not ok]
    assert not failed, f"driver checks that did not hold: {failed}"

    proc = subprocess.run(
        ["go", "run", "./tests/e2e/objects-provider-revoke-lease/driver", "verify",
         os.environ["ASTRAL_TESTS_SESSION"]],
        cwd=ROOT, capture_output=True, text=True,
        env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
    )
    sys.stderr.write(proc.stderr)
    assert proc.returncode == 0, f"the flow with fresh apps failed: exit {proc.returncode}"

    print("oracle: a revoked finder is dropped while Q stays, asked once per find; "
          "re-registration refused until the grant is back; E's and Z's 3 s leases "
          "lapse, are logged, and revive; node1's log shows the same dials")


main()
