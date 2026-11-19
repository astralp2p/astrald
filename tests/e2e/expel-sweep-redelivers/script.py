#!/usr/bin/env python3
"""Driver: node2 misses a ban while it keeps a stable link to node1, and
node1's expulsion sweep delivers the ban again.

1. The User expels a fresh identity on node1 (no daemon holds it; only the
   ban matters). PushToLocalSwarm delivers the ban to node2.
2. node2's links to node1 are recorded.
3. The ban row is deleted from node2's database while node2 runs: node2 now
   misses the ban, and no link event follows.
4. The driver polls node2 until it lists the ban again and records how long
   that took.

The oracle judges by its own reads; the facts are the baseline it compares with.
"""
import asyncio
import sqlite3
import time
from pathlib import Path

import astral
from astral.api.crypto import public_key_to_identity

from lib import nodefs
from lib.sessionio import load, write_facts

DELIVER = 30.0     # how long the original push to node2 may take
RECOVER = 30.0     # how long the sweep may take: 5 s period plus margin
POLL = 0.5
TABLE = "users__expulsions"


def bans(expelled) -> dict:
    """subject -> issuer of every ban in a list_expelled answer."""
    return {str(e.expulsion.subject): str(e.expulsion.issuer)
            for e in expelled
            if e.expulsion is not None and e.expulsion.subject is not None}


def node_db(node: dict) -> Path:
    """The database file under the node's root that holds the ban table."""
    for p in sorted(Path(nodefs.path(node)).rglob("*.db")):
        with sqlite3.connect(p) as db:
            row = db.execute("SELECT name FROM sqlite_master WHERE type='table' "
                             "AND name=?", (TABLE,)).fetchone()
        if row:
            return p
    raise RuntimeError(f"no database under {nodefs.path(node)} holds {TABLE}")


def delete_ban(node: dict, subject: str) -> int:
    with sqlite3.connect(node_db(node), timeout=10) as db:
        return db.execute(f"DELETE FROM {TABLE} WHERE subject_id = ?",
                          (subject,)).rowcount


async def until_banned(c, subject: str, budget: float) -> bool:
    deadline = time.monotonic() + budget
    while time.monotonic() < deadline:
        if subject in bans(await c.user.list_expelled()):
            return True
        await asyncio.sleep(POLL)
    return False


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    id1 = n1["identity"]

    async with await astral.connect(n1["endpoint"],
                                    token=doc["facts"]["user_token"]) as c:
        key = await c.crypto.new_key()
        subject = str(public_key_to_identity(await c.crypto.public_key(key)))
        await c.user.expel(subject)

    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        assert await until_banned(c, subject, DELIVER), (
            f"node2 never received the ban on {subject} from the expel push")

        links_before = sorted(str(l.id) for l in await c.nodes.links(experimental=True)
                              if str(l.remote_identity) == id1)
        assert links_before, "node2 holds no link to node1 before the ban is dropped"

        deleted = delete_ban(n2, subject)
        start = time.monotonic()
        recovered = await until_banned(c, subject, RECOVER)
        recovered_after = round(time.monotonic() - start, 2) if recovered else None

    write_facts({"subject_id": subject, "deleted_rows": deleted,
                 "recovered_after": recovered_after, "links_before": links_before})
    print(f"driver: deleted {deleted} ban row(s) on node2; "
          f"ban back after {recovered_after} s; links to node1 {links_before}")


asyncio.run(main())
