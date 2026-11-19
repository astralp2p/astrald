#!/usr/bin/env python3
"""While node1 is stopped: empty its auth index, as a lost auth DB would.

The contracts stay in node1's repository; only the rows of the auth index
(`auth__contracts`, `auth__contract_permits` in `data/astrald.db`) go. The
expulsion of node2 (`users__expulsions`) stays. The database file and its
write-ahead log are read through lib/nodefs.py, changed with sqlite3 in a
scratch copy, and the merged file is written back in place of both.
"""
import sqlite3
import tempfile
from pathlib import Path

from lib import nodefs
from lib.sessionio import load

DB = "data/astrald.db"
TABLES = ("auth__contract_permits", "auth__contracts")


def main():
    n1 = load()["nodes"]["node1"]
    present = nodefs.listdir(n1, "data")

    with tempfile.TemporaryDirectory() as tmp:
        copy = Path(tmp) / "astrald.db"
        copy.write_bytes(nodefs.read_bytes(n1, DB))
        # why the log too: rows node1 wrote last may sit in its write-ahead
        # log; sqlite3 reads them from the copy and the checkpoint merges them.
        if "astrald.db-wal" in present:
            (Path(tmp) / "astrald.db-wal").write_bytes(
                nodefs.read_bytes(n1, "data/astrald.db-wal"))
        con = sqlite3.connect(copy)
        rows = {t: con.execute(f"SELECT COUNT(*) FROM {t}").fetchone()[0] for t in TABLES}
        for t in TABLES:
            con.execute(f"DELETE FROM {t}")
        con.commit()
        con.execute("PRAGMA wal_checkpoint(TRUNCATE)")
        con.close()
        data = copy.read_bytes()

    for name in ("astrald.db-wal", "astrald.db-shm"):
        if name in present:
            nodefs.remove(n1, f"data/{name}")
    nodefs.write_bytes(n1, DB, data)
    print(f"while_down: emptied node1's auth index ({rows})")


main()
