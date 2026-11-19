#!/usr/bin/env python3
"""Driver: four swarm nodes, 12 provider/consumer pairs, each first call routed
with no relay contract pushed.

The flow runs in a Go program (driver/) built from this repository's pinned
astral-go: node3 and node4 join the swarm, and each node's consumer calls each
remote provider once, on the provider's first available update. This script
taps the log of every node meanwhile and keeps, per consumer node and remote
provider, the node's asks for the provider's relay contracts and the contracts
it pulled. It prints one JSON line of facts and judges nothing.
"""
import asyncio
import json
import os
import sys
from contextlib import AsyncExitStack
from pathlib import Path

from lib.nodelog import LogTap
from lib.sessionio import load, write_facts

ROOT = Path(__file__).resolve().parents[3]
NAMES = ["node1", "node2", "node3", "node4"]
ASK = "asking linked siblings for the relay contracts of "
PULLED = "pulled the relay contract of "
# why: an ask logs its end once the contract is indexed; the call has
# returned by then, the log line may still be on its way
SETTLE = 1.0


async def drive() -> tuple[int, str, str]:
    proc = await asyncio.create_subprocess_exec(
        "go", "run", "./tests/e2e/relay-contract-race/driver", "drive",
        os.environ["ASTRAL_TESTS_SESSION"], cwd=ROOT,
        stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE,
        env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")})
    out, err = await proc.communicate()
    return proc.returncode, out.decode(), err.decode()


def pulls(tap: LogTap, at: str, providers: dict) -> dict:
    """Per remote provider node: the asks and pulls node `at` logged."""
    return {of: {"asks": [e["text"] for e in tap.find(ASK + app)],
                 "pulled": [e["text"] for e in tap.find(PULLED + app)]}
            for of, app in providers.items() if of != at}


async def main() -> int:
    nodes = load()["nodes"]
    async with AsyncExitStack() as stack:
        taps = {n: await stack.enter_async_context(LogTap(nodes[n])) for n in NAMES}
        code, out, err = await drive()
        await asyncio.sleep(SETTLE)
    sys.stderr.write(err)
    lines = [l for l in out.splitlines() if l.startswith("{")]
    facts = json.loads(lines[-1])["relay_contract_race"] if lines else {}
    providers = facts.get("providers") or {}
    facts["logs"] = {n: pulls(taps[n], n, providers) for n in NAMES}
    facts["taps"] = {n: taps[n].state() for n in NAMES}
    write_facts({"relay_contract_race": facts})
    if code != 0:
        sys.exit(f"driver: exit {code}")
    pairs = facts.get("pairs") or []
    answered = sum(1 for p in pairs if p["answered"])
    print(f"driver: {len(pairs)} first calls made, {answered} answered")
    return 0


asyncio.run(main())
