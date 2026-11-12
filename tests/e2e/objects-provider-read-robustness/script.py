#!/usr/bin/env python3
"""Driver: bad object-provider candidates in node1's way.

The driver stores a unique raw blob O1 on node2 alone, as the User on node1,
then runs driver/ (Go, on tests/e2e/internal/provider) in `drive` mode. The Go
driver prints one JSON line of facts: what node1 answered, how long each call
took, and what each app was asked. It judges nothing and stops only on a
wiring fault.
"""
import asyncio
import json
import os
import subprocess
import sys
from pathlib import Path

import astral

from lib.sessionio import load, write_facts

ROOT = Path(__file__).resolve().parents[3]


async def store_on_node2(doc: dict) -> str:
    """Store a unique raw blob on node2 only and return its id."""
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    blob = b"objects-provider-read-robustness O1 " + os.urandom(16).hex().encode()
    # why create and not store: store canonicalizes a typed object and refuses
    # raw bytes (object-store-peer/script.py).
    async with await astral.connect(n1["endpoint"], token=doc["facts"]["user_token"]) as c:
        async with c.objects.create(target=n2["identity"]) as w:
            await w.write(blob)
            return str(await w.commit())


def main():
    o1 = asyncio.run(store_on_node2(load()))
    proc = subprocess.run(
        ["go", "run", "./tests/e2e/objects-provider-read-robustness/driver", "drive",
         os.environ["ASTRAL_TESTS_SESSION"], o1],
        cwd=ROOT, capture_output=True, text=True,
        env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
    )
    sys.stderr.write(proc.stderr)
    lines = [l for l in proc.stdout.splitlines() if l.startswith("{")]
    if lines:
        write_facts(json.loads(lines[-1]))
    if proc.returncode != 0:
        sys.exit(f"driver: exit {proc.returncode}")
    print(f"driver: node1 ran find, read, describe and search with bad candidates in play (O1 {o1[:16]}…)")


main()
