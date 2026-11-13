#!/usr/bin/env python3
"""Driver: change files in a directory node1 watches, while node1 runs.

Before the watch, W holds a baseline: d0.txt, e0.txt, f0.txt, k.txt,
sub/s1.txt and sub/s2.txt. The driver attaches W with
`fs.new_watch ... temporary=true`, waits until node1 holds every baseline id,
and sleeps 2.1 s so every later write lands on a newer mtime tick. Then, in
about two seconds:

- a1: create W/a1.txt empty, sleep 1.5 s, write it;
- a2: write W/a2.txt in one go (node1 on Linux only, see README);
- a3: mkdir W/newdir, sleep 1.5 s, write W/newdir/n1.txt;
- d: overwrite W/d0.txt in place with bytes of the same length;
- e: rewrite W/e0.txt with a different length;
- f: unlink W/f0.txt;
- r: remove W/sub with its two files;
- g: symlink W/link to a file outside W.

It then asks node1 to watch three bad paths (relative, missing, a regular
file) and records the answers. The driver exits without waiting: the oracle
polls for the index to converge.

W and the file outside it lie under node1's root, written through
lib/nodefs.py, so node1's machine holds them in every world.

Every payload carries the run stamp and its case name, so no two ids collide.
"""
import asyncio
import os
import time
from pathlib import Path

import astral

from lib import fsfixture, nodefs
from lib.sessionio import load, write_facts

REPO = "fs-index-live"

# why: d's overwrite keeps the length, so the size check cannot tell old from new
D_LEN = 40

# why: on a 1 s or 2 s mtime filesystem a write right after the scan could
# keep the indexed mtime, and the indexer would validate the row unhashed
TICK = 2.1

# why: kqueue may report a file created and written in one go as a Create
# before the per-file watch exists; pacing makes the write its own event
PACE = 1.5

BASELINE_DEADLINE = 20.0


def payload(stamp: str, case: str, length: int | None = None) -> bytes:
    text = f"fs-index-live {stamp} case {case}: "
    if length is None:
        return (text + "a file node1 must index live\n").encode()
    data = (text + "x" * length).encode()[:length]
    assert len(data) == length, f"case {case}: stamp too long for {length} B"
    return data


def entry(n1: dict, rel: str, data: bytes, expect: str,
          kind: str = "file") -> dict:
    return {"path": nodefs.path(n1, rel), "rel": rel,
            "payload": data.decode(), "expect": expect, "kind": kind}


def write_baseline(n1: dict, w: str, out: str, stamp: str) -> dict:
    """The files W holds before the watch, and the file outside it."""
    nodefs.mkdir(n1, f"{w}/sub")
    base = {"d-old": (f"{w}/d0.txt", payload(stamp, "d-old", D_LEN)),
            "e-old": (f"{w}/e0.txt", payload(stamp, "e-old")),
            "f": (f"{w}/f0.txt", payload(stamp, "f")),
            "k": (f"{w}/k.txt", payload(stamp, "k")),
            "r-s1": (f"{w}/sub/s1.txt", payload(stamp, "r-s1")),
            "r-s2": (f"{w}/sub/s2.txt", payload(stamp, "r-s2"))}
    for rel, data in base.values():
        nodefs.write_bytes(n1, rel, data)
    nodefs.write_bytes(n1, out, payload(stamp, "g-outside"))
    return base


def mutate(n1: dict, w: str, out: str, stamp: str, base: dict) -> dict:
    """Apply every live change; answers the case map."""
    linux = nodefs.platform(n1).startswith("linux")
    cases = {"k": entry(n1, *base["k"], "present")}

    a1 = f"{w}/a1.txt"
    nodefs.write_bytes(n1, a1, b"")
    nodefs.mkdir(n1, f"{w}/newdir")
    time.sleep(PACE)
    a1_data = payload(stamp, "a1")
    nodefs.write_bytes(n1, a1, a1_data)
    cases["a1"] = entry(n1, a1, a1_data, "present")

    n1_file = f"{w}/newdir/n1.txt"
    n1_data = payload(stamp, "a3")
    nodefs.write_bytes(n1, n1_file, n1_data)
    cases["a3"] = entry(n1, n1_file, n1_data, "present")

    if linux:
        a2_data = payload(stamp, "a2")
        nodefs.write_bytes(n1, f"{w}/a2.txt", a2_data)
        cases["a2"] = entry(n1, f"{w}/a2.txt", a2_data, "present")

    d_rel, d_old = base["d-old"]
    d_new = payload(stamp, "d-new", D_LEN)
    nodefs.overwrite(n1, d_rel, d_new)
    cases["d-old"] = entry(n1, d_rel, d_old, "absent")
    cases["d-new"] = entry(n1, d_rel, d_new, "present")

    e_rel, e_old = base["e-old"]
    e_new = payload(stamp, "e-new") * 3
    nodefs.write_bytes(n1, e_rel, e_new)
    cases["e-old"] = entry(n1, e_rel, e_old, "absent")
    cases["e-new"] = entry(n1, e_rel, e_new, "present")

    nodefs.remove(n1, base["f"][0])
    cases["f"] = entry(n1, *base["f"], "absent")

    nodefs.remove(n1, f"{w}/sub")
    cases["r-s1"] = entry(n1, *base["r-s1"], "absent")
    cases["r-s2"] = entry(n1, *base["r-s2"], "absent")

    nodefs.symlink(n1, f"{w}/link", out)
    cases["g"] = entry(n1, f"{w}/link", nodefs.read_bytes(n1, out), "absent",
                       kind="symlink")
    return cases


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]

    session_dir = Path(os.environ["ASTRAL_TESTS_SESSION"]).resolve().parent
    stamp = session_dir.parent.name
    # why under node1's root: the watched directory must live on node1's
    # machine, which in a Docker world sees no host directory
    w, out = f"{REPO}/watched", f"{REPO}/outside.txt"
    base = write_baseline(n1, w, out, stamp)
    w_path = nodefs.path(n1, w)

    async with await astral.connect(n1["endpoint"],
                                    token=doc["facts"]["user_token"]) as c:
        err = await fsfixture.new_watch(c, w_path, REPO, temporary=True)
        assert err is None, f"fs.new_watch {w_path}: {err}"

        want = {fsfixture.object_id(d) for _, d in base.values()}
        missing = await fsfixture.wait_contains(c, REPO, want,
                                                BASELINE_DEADLINE)
        assert not missing, (
            f"the initial scan of {w_path} never indexed "
            f"{sorted(i[:16] for i in missing)} in {BASELINE_DEADLINE:.0f}s; "
            "nothing live can be judged")
        time.sleep(TICK)

        started = time.monotonic()
        cases = mutate(n1, w, out, stamp, base)
        took = time.monotonic() - started

        bad_paths = {
            "fs-index-live-bad1": "relative/dir",
            "fs-index-live-bad2": nodefs.path(n1, f"{REPO}/missing"),
            "fs-index-live-bad3": nodefs.path(n1, out),
        }
        answers = {name: await fsfixture.new_watch(c, path, name,
                                                   temporary=True)
                   for name, path in bad_paths.items()}

    platform = nodefs.platform(n1)
    write_facts({
        "repo": REPO,
        "watch_dir": w_path,
        "watch_rel": w,
        "outside_file": nodefs.path(n1, out),
        "platform": platform,
        "cases": cases,
        "bad_paths": {name: {"path": path, "error": answers[name]}
                      for name, path in bad_paths.items()},
    })
    print(f"driver: watching {w_path} as {REPO!r}; applied "
          f"{', '.join(sorted(cases))} in {took:.1f}s on {platform}; "
          f"bad paths answered {answers}")


asyncio.run(main())
