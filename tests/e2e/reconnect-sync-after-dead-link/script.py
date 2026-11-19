#!/usr/bin/env python3
"""Driver: a new link to node2 replaces a black-holed one, and node1 syncs.

The flow runs in a Go program (driver/) built from this repository's pinned
astral-go, which serves the TCP relay the black-holed link runs through. It
prints one JSON line of facts: which steps held, and how long each took.

While the Go program runs, a LogTap records node1's log. The facts add the
entries in which node1 closed a link to node2 on its ping timeout, and the
entries in which node1 started a sibling sync with node2.
why the log: the sync that the close starts leaves no state an op can read;
the sync that the new link starts delivers the same contracts once the dead
link closes, so only the log tells the two apart.
"""
import asyncio
import json
import os
import sys
from pathlib import Path

from lib.nodelog import LogTap
from lib.sessionio import load, write_facts

ROOT = Path(__file__).resolve().parents[3]
DRIVER = "./tests/e2e/reconnect-sync-after-dead-link/driver"


async def run_driver() -> tuple:
    """Run the Go driver; its exit code, its stdout and its stderr."""
    proc = await asyncio.create_subprocess_exec(
        "go", "run", DRIVER, os.environ["ASTRAL_TESTS_SESSION"],
        cwd=ROOT, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE,
        env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
    )
    out, err = await proc.communicate()
    return proc.returncode, out.decode(), err.decode()


def entries(tap: LogTap, *needles: str) -> list:
    """The entries whose text holds every needle, as {t, text}."""
    return [{"t": e["t"], "text": e["text"]} for e in tap.entries
            if all(n in e["text"] for n in needles)]


async def main():
    nodes = load()["nodes"]
    n1, id2 = nodes["node1"], nodes["node2"]["identity"]
    async with LogTap(n1) as tap:
        code, out, err = await run_driver()
        # why the pause: the tap streams entries a moment after node1 logs them.
        await asyncio.sleep(1.0)
    sys.stderr.write(err)

    lines = [l for l in out.splitlines() if l.startswith("{")]
    facts = json.loads(lines[-1]) if lines else {"reconnect_sync": {}}
    facts["reconnect_sync"]["node1_log"] = {
        "closed_on_ping_timeout": entries(tap, "closed", id2, "ping timeout"),
        "syncs": entries(tap, "syncing with", id2),
        "tap": tap.state(),
    }
    write_facts(facts)
    if code != 0:
        sys.exit(f"driver: exit {code}")
    print("driver: node1 dropped the black-holed link and node2 learned node1's app")


asyncio.run(main())
