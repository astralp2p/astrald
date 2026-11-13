#!/usr/bin/env python3
"""Driver: watch a directory with a persisted fs.new_watch and record the
changes while_down.py makes while node1 is stopped.

Before the watch, the watched directory W holds keep.txt, edit.txt, del.txt,
mv-src.txt, moveout.txt, over.txt, sub/s1.txt and sub/s2.txt, each with its
mtime an hour back. The staging directory S, a sibling of W, holds over.new
and new.txt. The driver attaches W as a persisted watch repository and waits
until node1 lists exactly W's files.

The case map in the facts names every id W must hold after the restart
(`present`) and every id it must not (`absent`), with the path the id is
judged at and, for a file renamed or moved, the path it left. The runner
then stops node1, runs while_down.py, and starts node1.

W and S lie under node1's root, written through lib/nodefs.py, so node1's
machine holds them in every world. Every payload carries the run stamp and
its case name, so no two ids collide.
"""
import asyncio
import os
import time
from pathlib import Path

import astral

from lib import fsfixture, nodefs
from lib.sessionio import load, write_facts

REPO = "fs-index-restart-reconcile"
WATCH = f"{REPO}/watched"
STAGING = f"{REPO}/staging"

# why: an edit that keeps the length leaves only the mtime to tell old from
# new, which is what the start-up scan compares
EDIT_LEN = 96

# why an hour back: a change made while node1 is down then lands on another
# mtime on any filesystem, whatever its granularity
# why W's files alone: a staged file keeps its mtime when moved in, and the
# start-up scan trusts a row whose path and mtime match, so over.new must
# not share over.txt's
AGE = 3600.0

INDEX_DEADLINE = 20.0


def payload(stamp: str, case: str, length: int | None = None) -> bytes:
    text = f"{REPO} {stamp} case {case}: "
    if length is None:
        return (text + "a file node1 reconciles at start\n").encode()
    data = (text + "x" * length).encode()
    assert len(text) < length, f"case {case}: stamp too long for {length} B"
    return data[:length]


def case(n1: dict, rel: str, data: bytes, expect: str,
         old_rel: str | None = None) -> dict:
    return {"rel": rel, "path": nodefs.path(n1, rel),
            "payload": data.decode(), "expect": expect,
            "old_rel": old_rel,
            "old_path": nodefs.path(n1, old_rel) if old_rel else None}


def plan(n1: dict, stamp: str) -> tuple[dict, dict]:
    """(files written before the watch {rel: bytes}, the case map)."""
    w, s = WATCH, STAGING
    p = {name: payload(stamp, name) for name in
         ("keep", "del", "moved", "moveout", "new", "sub-s1", "sub-s2",
          "fresh")}
    p["edit-old"] = payload(stamp, "edit-old", EDIT_LEN)
    p["edit-new"] = payload(stamp, "edit-new", EDIT_LEN)
    p["over-old"] = payload(stamp, "over-old")
    p["over-new"] = payload(stamp, "over-new") * 2
    before = {f"{w}/keep.txt": p["keep"], f"{w}/edit.txt": p["edit-old"],
              f"{w}/del.txt": p["del"], f"{w}/mv-src.txt": p["moved"],
              f"{w}/moveout.txt": p["moveout"],
              f"{w}/over.txt": p["over-old"],
              f"{w}/sub/s1.txt": p["sub-s1"], f"{w}/sub/s2.txt": p["sub-s2"],
              f"{s}/over.new": p["over-new"], f"{s}/new.txt": p["new"]}
    cases = {
        "keep": case(n1, f"{w}/keep.txt", p["keep"], "present"),
        "edit-old": case(n1, f"{w}/edit.txt", p["edit-old"], "absent"),
        "edit-new": case(n1, f"{w}/edit.txt", p["edit-new"], "present"),
        "del": case(n1, f"{w}/del.txt", p["del"], "absent"),
        "moved": case(n1, f"{w}/moved.txt", p["moved"], "present",
                      f"{w}/mv-src.txt"),
        "moveout": case(n1, f"{w}/moveout.txt", p["moveout"], "absent"),
        "over-old": case(n1, f"{w}/over.txt", p["over-old"], "absent"),
        "over-new": case(n1, f"{w}/over.txt", p["over-new"], "present",
                         f"{s}/over.new"),
        "new": case(n1, f"{w}/new.txt", p["new"], "present",
                    f"{s}/new.txt"),
        "sub-s1": case(n1, f"{w}/sub2/s1.txt", p["sub-s1"], "present",
                       f"{w}/sub/s1.txt"),
        "sub-s2": case(n1, f"{w}/sub2/s2.txt", p["sub-s2"], "present",
                       f"{w}/sub/s2.txt"),
        "fresh": case(n1, f"{w}/fresh/f1.txt", p["fresh"], "present"),
    }
    return before, cases


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    stamp = Path(os.environ["ASTRAL_TESTS_SESSION"]).resolve().parent.parent.name

    before, cases = plan(n1, stamp)
    old = time.time() - AGE
    for rel, data in before.items():
        nodefs.write_bytes(n1, rel, data)
        if rel.startswith(f"{WATCH}/"):
            nodefs.set_mtime(n1, rel, old)
    watch_dir = nodefs.path(n1, WATCH)
    want = set(fsfixture.disk_ids(n1, WATCH).values())

    async with await astral.connect(n1["endpoint"],
                                    token=doc["facts"]["user_token"]) as c:
        # why persisted: node1 restores the watch at start, and the
        # start-up scan of its root is what this test judges
        err = await fsfixture.new_watch(c, watch_dir, REPO)
        assert err is None, f"fs.new_watch {watch_dir}: {err}"
        ok, got = await fsfixture.wait_scan(c, REPO, want, INDEX_DEADLINE)
        assert ok, (f"{REPO!r} never listed the baseline under {watch_dir} "
                    f"in {INDEX_DEADLINE:.0f}s "
                    f"({fsfixture.scan_diff(want, got)})")

    write_facts({
        "restarts_before": n1.get("restarts") or 0,
        "repo": REPO,
        "watch_dir": watch_dir,
        "watch_rel": WATCH,
        "staging_rel": STAGING,
        "cases": cases,
    })
    print(f"driver: {REPO!r} lists the {len(want)} baseline files under "
          f"{watch_dir}; cases {sorted(cases)}")


asyncio.run(main())
