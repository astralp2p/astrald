"""The harness answerer: the User's stand-in for the apps a driver registers.

    python3 -m lib.answerer      # with ASTRAL_TESTS_SESSION set; runs until killed

Since G7 an app holds its permits only once a ContractRequest for them is
answered with an offer, and its host co-signed the offer and signed the
app's contract (astral-docs protocols/apphost/ops/apphost.request_contract.md). Before G7 the harness
configured the five admission policies to accept; this is what replaces them.
While a test runs, from its driver to its oracle, the runner runs this
answerer beside it, unless the test's manifest sets `answers` (the test
answers SwarmState requests itself, as the swarm-state tests do).

While a driver waits for an answer (a file in `$ASTRAL_TESTS_WAITING`, which
`swarmstate.waiting` and `tests/e2e/internal/contracts` write), every `POLL`
seconds it lists each node's pending requests with that node's
own token (`user.swarm_requests`) and approves every pending ContractRequest
(`user.answer_request`), which publishes an offer, as the accepting policies
admitted apps before. Only a node whose tier answers requests can: a
Privileged node, or an unclaimed node, which is its own root. A Member's
request reaches a Privileged node through SwarmState, and is offered there.
A JoinRequest needs no answer: its carrier signs the membership in the
carrier session, and a privileged_member one needs the user key
(`user.confirm_join`). ExpelRequests are left alone.

It judges nothing: a refusal, a dead node or a node not yet up is skipped,
and the next round tries again.
"""
import asyncio
import os
import sys
import time
from pathlib import Path

import astral

from lib import jsonops
from lib import swarmstate as ss
from lib.sessionio import load

POLL = 0.5


async def answer_node(node: dict, done: set) -> None:
    """Approve every pending ContractRequest node lists, with its own token."""
    async with await astral.connect(node["endpoint"], token=node["token"]) as c:
        for r in await ss.requests(c, timeout=10):
            # why pending only: one offer per request is enough, and the host
            # co-signs one
            if r.type != ss.CONTRACT or r.status != "pending" or r.id in done:
                continue
            try:
                await ss.answer(c, r.id, "approve", timeout=10)
            except jsonops.OpError as e:
                # note: a Member cannot answer; a Privileged node will
                print(f"{time.strftime('%H:%M:%S')} {node['identity'][:8]} "
                      f"{r.id}: {e}", flush=True)
                continue
            done.add(r.id)
            print(f"{time.strftime('%H:%M:%S')} {node['identity'][:8]} offered "
                  f"{r.id} for {r.obj.get('Identity')}", flush=True)


def waiting() -> bool:
    """Whether a driver waits for an answer: a file in $ASTRAL_TESTS_WAITING
    (lib/swarmstate.waiting, tests/e2e/internal/contracts)."""
    d = os.environ.get("ASTRAL_TESTS_WAITING")
    try:
        return d is None or any(Path(d).iterdir())
    except OSError:
        return False


async def main() -> None:
    done: set = set()
    while True:
        # why: a poll is a query each node logs, and some tests judge a
        # quiet log, so the answerer asks only while a driver waits
        if not waiting():
            await asyncio.sleep(POLL / 2)
            continue
        # why reread: the runner writes the view anew for each phase, and the
        # first one only once the test's world is ready
        try:
            nodes = load()["nodes"]
        except (OSError, ValueError):
            await asyncio.sleep(POLL)
            continue
        for node in nodes.values():
            if not node or not node.get("endpoint") or not node.get("token"):
                continue
            try:
                await answer_node(node, done)
            except Exception as e:     # noqa: BLE001 — a node down or not yet up
                print(f"{time.strftime('%H:%M:%S')} {node['identity'][:8]}: "
                      f"{type(e).__name__}: {e}", file=sys.stderr, flush=True)
        await asyncio.sleep(POLL)


if __name__ == "__main__":
    asyncio.run(main())
