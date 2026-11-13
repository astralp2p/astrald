#!/usr/bin/env python3
"""Driver: drop a file into a directory and let node1 watch it become an object.

`fs.new_watch` registers a directory as an object repository, scans it in the
background and adds it to the `objects.RepoLocal` group; the watcher then tracks
the directory and the indexer hashes what appears. This is how a user's own
files enter the object graph — the one path by which astrald holds data nobody
typed into it — and no test touched `fs.new_repo`, `fs.new_watch`,
`objects.scan` or `objects.repositories`.

The watched directory lives under node1's own root, written through
lib/nodefs.py, never anywhere else on its machine. A test that registers a
watch is telling a daemon to index a path, and the only path it has any
business indexing is one this run made. Under the root it lies on node1's
machine in every world.

astral-py binds no `fs` module, so the watch is a raw query. The wire form is
`op?param=value` — the `-flag value` spelling the docs' astral-query examples
use fails as `route_not_found` rather than as a bad argument.

The file is written **before** the watch is registered, so the initial scan is
what must find it. Writing afterwards would test the fsnotify path instead, and
the two are different mechanisms with different failure modes.
"""
import asyncio
import os
from pathlib import Path

import astral

from lib import nodefs
from lib.sessionio import load, write_facts

REPO = "fs-watch-probe"
FILENAME = "dropped.txt"


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]

    session_dir = Path(os.environ["ASTRAL_TESTS_SESSION"]).resolve().parent
    run_stamp = session_dir.parent.name        # results/<stamp>/session/…
    watch_rel = "fs-watch"
    nodefs.mkdir(n1, watch_rel)
    watch_dir = nodefs.path(n1, watch_rel)

    # Run-unique, so an id from one run can never be mistaken for another's.
    payload = (f"fs-watch probe in {run_stamp}: a file dropped into a "
               f"watched directory becomes an object\n").encode()
    nodefs.write_bytes(n1, f"{watch_rel}/{FILENAME}", payload)
    target = nodefs.path(n1, f"{watch_rel}/{FILENAME}")

    async with await astral.connect(n1["endpoint"],
                                    token=doc["facts"]["user_token"]) as c:
        await c.call_one(f"fs.new_watch?path={watch_dir}&name={REPO}")

    write_facts({
        "repo": REPO,
        "watch_dir": watch_dir,
        "file_path": target,
        "payload": payload.decode(),
    })
    print(f"driver: watching {watch_dir} as {REPO!r}; "
          f"{FILENAME} holds {len(payload)} B")


asyncio.run(main())
