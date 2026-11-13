#!/usr/bin/env python3
"""While node1 is stopped: change the watched directory W.

The runner runs this between stopping node1 and starting it again, so no
live watcher sees a change; only the start-up scan can. Through
lib/nodefs.py, which reaches node1's files whether or not astrald runs:

- edit.txt: overwritten in place with bytes of the same length;
- del.txt: deleted;
- mv-src.txt: renamed to moved.txt;
- moveout.txt: moved out of W, into the staging directory S;
- over.txt: replaced by S/over.new, of another length;
- new.txt: moved into W from S;
- sub/: renamed to sub2/, with its two files;
- fresh/f1.txt: written in a new directory.

It first checks that node1 does not answer: a change made while node1 runs
would test its watcher, not its start-up scan.
"""
import asyncio

import astral

from lib import nodefs
from lib.sessionio import load


async def answers(n1: dict, token: str) -> bool:
    try:
        async with await astral.connect(n1["endpoint"], token=token,
                                        connect_timeout=3.0) as c:
            await c.apphost.whoami()
    except Exception:                       # noqa: BLE001
        return False
    return True


def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    facts = doc["facts"]
    w, s = facts["watch_rel"], facts["staging_rel"]
    cases = facts["cases"]

    assert not asyncio.run(answers(n1, facts["user_token"])), (
        "node1 answers while it should be stopped")

    nodefs.overwrite(n1, f"{w}/edit.txt",
                     cases["edit-new"]["payload"].encode())
    nodefs.remove(n1, f"{w}/del.txt")
    nodefs.rename(n1, f"{w}/mv-src.txt", f"{w}/moved.txt")
    nodefs.rename(n1, f"{w}/moveout.txt", f"{s}/moveout.txt")
    nodefs.rename(n1, f"{s}/over.new", f"{w}/over.txt")
    nodefs.rename(n1, f"{s}/new.txt", f"{w}/new.txt")
    nodefs.rename(n1, f"{w}/sub", f"{w}/sub2")
    nodefs.write_bytes(n1, f"{w}/fresh/f1.txt",
                       cases["fresh"]["payload"].encode())
    print(f"while_down: changed {facts['watch_dir']} while node1 was stopped")


main()
