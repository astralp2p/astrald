#!/usr/bin/env python3
"""Driver: the F3 chain's last churn step; node3 freezes, then thaws.

1. Freezes node3's astrald (SIGSTOP, `lib/nodeproc.py`): its sockets stay
   open, so only the peers' keepalive tells its links are dead.
2. Holds FREEZE seconds, past the tcp bound N + T (25 s), and thaws node3 in
   `finally`, whatever happened.
3. Waits up to 60 s from the thaw until all 20 pairs of the five remaining
   `f3` apps answer since the thaw.

It judges nothing and records the `survives_churn` fact: {freeze_at,
thaw_at, recovered_at (null when the 20 pairs did not answer within 60 s),
missing}.
"""
import time

from lib import meshapps, nodeproc
from lib.sessionio import load, write_facts

NAMES = ["node1", "node2", "node3", "node5", "late"]
PREFIX = "f3"
# why 40 s: past tcp's N + T, 15 s + 10 s, so every link to node3 closes
# while it is frozen (astral-docs topics/link-mux.md, Per-transport values)
FREEZE = 40.0
RECOVER = 60.0

node3 = load()["nodes"]["node3"]
pid = nodeproc.freeze(node3["root"])
freeze_at = time.time()
try:
    time.sleep(FREEZE)
finally:
    nodeproc.thaw(pid)
    thaw_at = time.time()
recovered_at, missing = meshapps.wait_answering(PREFIX, NAMES, since=thaw_at, timeout=RECOVER)
write_facts({"survives_churn": {"freeze_at": freeze_at, "thaw_at": thaw_at,
                                "recovered_at": recovered_at,
                                "missing": [f"{c}->{p}" for c, p in missing]}})
if recovered_at is None:
    print(f"driver: the 20 pairs did not answer within {RECOVER:.0f} s of the thaw; "
          f"missing {missing}")
else:
    print(f"driver: node3 frozen {thaw_at - freeze_at:.1f} s; the 20 pairs answered "
          f"{recovered_at - thaw_at:.1f} s after the thaw")
