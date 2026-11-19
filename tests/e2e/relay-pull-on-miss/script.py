#!/usr/bin/env python3
"""Driver: node1 routes its first call to a new app on node2 by pulling the
app's relay contract from node2.

node2 pushes no relay contract at apphost.register in this world
(`skip_sibling_push`), so node1 holds none when its visitor app calls the
greeter app. The flow runs in a Go program (driver/) built from this
repository's pinned astral-go; this script taps node1's log meanwhile and
keeps the lines of node1's ask for the greeter. It prints one JSON line of
facts and judges nothing.
"""
import asyncio
import json
import os
import sys
from pathlib import Path

from lib.nodelog import LogTap
from lib.sessionio import load, write_facts

ROOT = Path(__file__).resolve().parents[3]
ASK = "relay contracts of "
PULLED = "pulled the relay contract of "
# why: the ask logs once it ends; the call has returned by then, the log line
# may still be on its way
SETTLE = 1.0


async def drive() -> tuple[int, str, str]:
    proc = await asyncio.create_subprocess_exec(
        "go", "run", "./tests/e2e/relay-pull-on-miss/driver", "drive",
        os.environ["ASTRAL_TESTS_SESSION"], cwd=ROOT,
        stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE,
        env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")})
    out, err = await proc.communicate()
    return proc.returncode, out.decode(), err.decode()


async def main() -> int:
    node1 = load()["nodes"]["node1"]
    async with LogTap(node1) as tap:
        code, out, err = await drive()
        await asyncio.sleep(SETTLE)
        sys.stderr.write(err)
        lines = [l for l in out.splitlines() if l.startswith("{")]
        facts = json.loads(lines[-1])["relay_pull_on_miss"] if lines else {}
        greeter = facts.get("greeter", "")
        facts["asks"] = [e["text"] for e in tap.find(ASK + greeter)] if greeter else []
        facts["pulled"] = [e["text"] for e in tap.find(PULLED + greeter)] if greeter else []
        facts["tap"] = tap.state()
    write_facts({"relay_pull_on_miss": facts})
    if code != 0:
        sys.exit(f"driver: exit {code}")
    print(f"driver: node1 asked {len(facts['asks'])} time(s) for the greeter "
          f"and its first call took {facts.get('call_ms')} ms")
    return 0


asyncio.run(main())
