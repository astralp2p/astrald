#!/usr/bin/env python3
"""Oracle: after a restart, node1's index of a persisted watch matches the
files on disk, though every change happened while node1 was stopped.

The expected set is the oracle's own: it walks the watched directory on
node1's machine through lib/nodefs.py, keeps regular files and hashes the
bytes it reads there. It first checks that set against the facts' `present`
cases, and that no file sits at a path a case left, so the test cannot pass
on changes that never landed. Then:

1. node1 was restarted exactly once since the driver ran.
2. `objects.scan` of the repository lists exactly that set within 30 s.
   On a timeout every case is reported with its state.
3. Every present file is contained, read by repository returns its bytes,
   and `objects.describe` places it at its path on node1, and at no path it
   left.
4. Every absent id (edit.txt's and over.txt's old bytes, del.txt,
   moveout.txt) is not contained, a read by repository is refused, and no
   file location names its old path.
5. `objects.repositories` lists the repository in the `local` group.
"""
import asyncio

import astral

from lib import fsfixture, nodefs
from lib.sessionio import load

DEADLINE = 30.0


def present(cases: dict) -> dict:
    return {k: v for k, v in cases.items() if v["expect"] == "present"}


def absent(cases: dict) -> dict:
    return {k: v for k, v in cases.items() if v["expect"] == "absent"}


def oid(case: dict) -> str:
    return fsfixture.object_id(case["payload"].encode())


def exists(n1: dict, rel: str) -> bool:
    parent, _, base = rel.rpartition("/")
    try:
        return base in nodefs.listdir(n1, parent)
    except OSError:
        return False


def expected_set(n1: dict, w: str, cases: dict) -> set:
    """The disk walk, checked against the facts."""
    on_disk = fsfixture.disk_ids(n1, w)
    want = {oid(c) for c in present(cases).values()}
    assert set(on_disk.values()) == want, (
        f"the regular files under {w} hash to "
        f"{sorted(i[:16] for i in on_disk.values())}, not the present cases "
        f"{sorted(i[:16] for i in want)}: while_down.py's changes did not "
        f"land as recorded")
    for name, c in present(cases).items():
        assert on_disk.get(c["path"]) == oid(c), (
            f"case {name}: {c['path']} does not hold its payload")
        assert not (c["old_rel"] and exists(n1, c["old_rel"])), (
            f"case {name}: {c['old_path']} is still on disk")
    for name, c in absent(cases).items():
        assert on_disk.get(c["path"]) != oid(c), (
            f"case {name}: {c['path']} still holds its old bytes")
    return want


def case_table(cases: dict, got: set) -> str:
    rows = []
    for name, c in sorted(cases.items()):
        listed = oid(c) in got
        ok = listed == (c["expect"] == "present")
        state = ("ok" if ok else
                 "missing" if c["expect"] == "present" else "stale")
        rows.append(f"  {name:9} {c['expect']:7} {state:7} {c['path']}")
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
        stale = [p for p, _ in locs if p == case["old_path"]]
        assert not stale, (
            f"case {name}: {fsfixture.LOCATION} still places {i[:16]}… at "
            f"{case['old_path']}, which it left while node1 was stopped")


async def check_absent(c, repo: str, cases: dict) -> None:
    for name, case in sorted(absent(cases).items()):
        i = oid(case)
        assert not await c.objects.contains(repo, i), (
            f"case {name}: {repo!r} still contains {i[:16]}… "
            f"({case['path']})")
        served = await fsfixture.read_refused(c, i, repo)
        assert served is None, (
            f"case {name}: {repo!r} {served} for {i[:16]}…, which no file "
            f"under the watch holds")
        locs = await fsfixture.file_locations(c, i)
        stale = [p for p, _ in locs if p == case["path"]]
        assert not stale, (
            f"case {name}: {fsfixture.LOCATION} still places {i[:16]}… at "
            f"{case['path']}")


async def check_listed(c, repo: str) -> None:
    infos = await c.objects.repositories()
    local = next((i for i in infos if str(i.name) == "local"), None)
    assert local is not None, "objects.repositories lists no `local` group"
    assert repo in {str(i.name) for i in infos} and repo in {
        str(m) for m in local.children}, (
        f"{repo!r} is not listed in the local group after the restart")


async def main():
    doc = load()
    facts = doc["facts"]
    n1 = doc["nodes"]["node1"]
    repo, w, cases = facts["repo"], facts["watch_dir"], facts["cases"]

    restarts = (n1.get("restarts") or 0) - facts["restarts_before"]
    assert restarts == 1, (
        f"node1 was restarted {restarts} times between driver and oracle, "
        "not once")

    want = expected_set(n1, facts["watch_rel"], cases)

    async with await astral.connect(n1["endpoint"],
                                    token=facts["user_token"]) as c:
        loop = asyncio.get_running_loop()
        started = loop.time()
        ok, got = await fsfixture.wait_scan(c, repo, want, DEADLINE)
        took = loop.time() - started
        assert ok, (
            f"{repo!r} did not reconcile with the files under {w} in "
            f"{DEADLINE:.0f}s after the restart "
            f"({fsfixture.scan_diff(want, got)}):\n{case_table(cases, got)}")

        await check_present(c, repo, cases, n1["identity"])
        await check_absent(c, repo, cases)
        await check_listed(c, repo)

    print(f"oracle: after the restart {repo!r} lists exactly the {len(want)} "
          f"regular files under {w} ({took:.1f}s into the oracle), serves and "
          f"places each at its new path, and holds none of "
          f"{sorted(absent(cases))}:\n{case_table(cases, want)}")


asyncio.run(main())
