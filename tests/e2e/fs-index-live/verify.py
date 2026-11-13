#!/usr/bin/env python3
"""Oracle: node1's index of the watched directory converges to the regular
files on disk, without a restart.

The expected set is the oracle's own: it walks the watched directory on
node1's machine through lib/nodefs.py, keeps regular files (no symlinks) and
hashes the bytes it reads there. It first checks that set against the ids of
the facts' `present` cases, so the test cannot pass on an empty or wrong
expectation. Then:

1. `objects.scan` of the repository lists exactly that set within 20 s (the
   write debounce is 3 s). On a timeout every case is reported with its
   state, and the test fails once.
2. Every present file is contained, read by repository returns its bytes, and
   `objects.describe` places it at its path on node1.
3. Every absent id (the old bytes of d and e, f0, sub/s1, sub/s2) is not
   contained, a read by repository is refused, and no file location names
   its old path. The symlink's target is not contained and no file location
   names the link.
4. Four seconds later the scan still lists exactly the set: no late flip.
5. Three bad paths (relative, missing, a regular file) are refused by
   `fs.new_watch`, from the driver and again here, and none is registered.

Case a2 (one-go create and write) exists only where node1 runs on Linux; the
driver skips it elsewhere, and node1's platform is printed.
"""
import asyncio

import astral

from lib import fsfixture, nodefs
from lib.sessionio import load

DEADLINE = 20.0
SETTLE = 4.0


def present(cases: dict) -> dict:
    return {k: v for k, v in cases.items() if v["expect"] == "present"}


def absent(cases: dict) -> dict:
    return {k: v for k, v in cases.items() if v["expect"] == "absent"}


def oid(case: dict) -> str:
    return fsfixture.object_id(case["payload"].encode())


def expected_set(n1: dict, w: str, cases: dict) -> set:
    """The disk walk, checked against the facts."""
    on_disk = fsfixture.disk_ids(n1, w)
    want = {oid(c) for c in present(cases).values()}
    assert set(on_disk.values()) == want, (
        f"the regular files under {w} hash to {sorted(i[:16] for i in on_disk.values())}, "
        f"not the present cases {sorted(i[:16] for i in want)}: the driver's "
        f"changes did not land as recorded")
    for name, c in present(cases).items():
        assert on_disk.get(c["path"]) == oid(c), (
            f"case {name}: {c['path']} does not hold its payload")
    for name, c in absent(cases).items():
        if c["kind"] == "symlink":
            parent, _, base = c["rel"].rpartition("/")
            assert nodefs.listdir(n1, parent).get(base) == "link", (
                f"case {name}: {c['path']} is no symlink")
        else:
            assert on_disk.get(c["path"]) != oid(c), (
                f"case {name}: {c['path']} still holds its old bytes")
    return want


def case_table(cases: dict, got: set) -> str:
    rows = []
    for name, c in sorted(cases.items()):
        listed = oid(c) in got
        ok = listed == (c["expect"] == "present")
        state = "ok" if ok else ("missing" if c["expect"] == "present" else "stale")
        rows.append(f"  {name:6} {c['expect']:7} {state:7} {c['path']}")
    return "\n".join(rows)


async def check_present(c, repo: str, cases: dict, node1: str) -> None:
    for name, case in sorted(present(cases).items()):
        i, data = oid(case), case["payload"].encode()
        assert await c.objects.contains(repo, i), (
            f"case {name}: {repo!r} does not contain {i[:16]}…")
        got = await c.objects.read(i, repo=repo)
        assert got == data, (
            f"case {name}: {repo!r} serves {len(got)} B for {i[:16]}…, not "
            f"the {len(data)} B of {case['path']}")
        locs = await fsfixture.file_locations(c, i)
        assert (case["path"], node1) in locs, (
            f"case {name}: {fsfixture.LOCATION} puts {i[:16]}… at {locs}, "
            f"not at {case['path']} on node1")


async def check_absent(c, repo: str, cases: dict) -> None:
    for name, case in sorted(absent(cases).items()):
        i = oid(case)
        assert not await c.objects.contains(repo, i), (
            f"case {name}: {repo!r} still contains {i[:16]}… ({case['path']})")
        served = await fsfixture.read_refused(c, i, repo)
        assert served is None, (
            f"case {name}: {repo!r} {served} for {i[:16]}…, which no file "
            f"under the watch holds")
        locs = await fsfixture.file_locations(c, i)
        stale = [p for p, _ in locs if p == case["path"]]
        assert not stale, (
            f"case {name}: {fsfixture.LOCATION} still places {i[:16]}… at "
            f"{case['path']}")


async def check_bad_paths(c, bad: dict) -> None:
    for name, b in sorted(bad.items()):
        assert b["error"], (
            f"the driver's fs.new_watch of {b['path']!r} as {name} was acked")
        again = await fsfixture.new_watch(c, b["path"], name, temporary=True)
        assert again, f"fs.new_watch of {b['path']!r} as {name} was acked"
    listed = {str(i.name) for i in await c.objects.repositories()}
    registered = sorted(set(bad) & listed)
    assert not registered, f"refused watches are registered: {registered}"


async def main():
    doc = load()
    facts = doc["facts"]
    n1 = doc["nodes"]["node1"]
    node1 = n1["identity"]
    repo, w, cases = facts["repo"], facts["watch_dir"], facts["cases"]
    print(f"oracle: platform {facts['platform']}; cases {sorted(cases)}")

    want = expected_set(n1, facts["watch_rel"], cases)

    async with await astral.connect(n1["endpoint"],
                                    token=facts["user_token"]) as c:
        loop = asyncio.get_running_loop()
        started = loop.time()
        ok, got = await fsfixture.wait_scan(c, repo, want, DEADLINE)
        took = loop.time() - started
        assert ok, (
            f"{repo!r} did not converge to the files under {w} in "
            f"{DEADLINE:.0f}s ({fsfixture.scan_diff(want, got)}):\n"
            f"{case_table(cases, got)}")

        await check_present(c, repo, cases, node1)
        await check_absent(c, repo, cases)

        await asyncio.sleep(SETTLE)
        got = {str(i) for i in await c.objects.scan(repo)}
        assert got == want, (
            f"{repo!r} changed {SETTLE:.0f}s after converging "
            f"({fsfixture.scan_diff(want, got)}):\n{case_table(cases, got)}")

        await check_bad_paths(c, facts["bad_paths"])

    print(f"oracle: {repo!r} lists exactly the {len(want)} regular files "
          f"under {w} ({took:.1f}s into the oracle), serves and places each, holds none of "
          f"{sorted(absent(cases))}, and refused "
          f"{sorted(facts['bad_paths'])}:\n{case_table(cases, want)}")


asyncio.run(main())
