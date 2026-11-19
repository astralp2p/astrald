#!/usr/bin/env python3
"""Driver: an app on node1 calls an app identity no node hosts, twice.

The flow runs in a Go program (driver/) built from this repository's pinned
astral-go; this script taps node1's log meanwhile and keeps the lines of
node1's asks for the absent identity. It prints one JSON line of facts and
judges nothing.
"""
import asyncio
import json
import os
import sys
from pathlib import Path

from lib.nodelog import LogTap
from lib.sessionio import load, write_facts

ROOT = Path(__file__).resolve().parents[3]
# why: the ask logs its miss once it ends, which may follow the answer
SETTLE = 1.0


async def drive() -> tuple[int, str, str]:
    proc = await asyncio.create_subprocess_exec(
        "go", "run", "./tests/e2e/relay-pull-absent/driver", "drive",
        os.environ["ASTRAL_TESTS_SESSION"], cwd=ROOT,
        stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE,
        env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")})
    out, err = await proc.communicate()
    return proc.returncode, out.decode(), err.decode()


async def main() -> None:
    node1 = load()["nodes"]["node1"]
    async with LogTap(node1) as tap:
        code, out, err = await drive()
        await asyncio.sleep(SETTLE)
        sys.stderr.write(err)
        lines = [l for l in out.splitlines() if l.startswith("{")]
        facts = json.loads(lines[-1])["relay_pull_absent"] if lines else {}
        target = facts.get("target", "")
        # both the ask and its miss name the target
        facts["asks"] = [e["text"] for e in tap.find(target)] if target else []
        facts["tap"] = tap.state()
    write_facts({"relay_pull_absent": facts})
    if code != 0:
        sys.exit(f"driver: exit {code}")
    print(f"driver: calls {facts.get('calls')}; node1 logged {len(facts['asks'])} ask line(s)")


asyncio.run(main())
