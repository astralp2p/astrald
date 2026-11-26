"""The harness answerer: the User's stand-in for the apps a driver registers.

    python3 -m lib.answerer      # with ASTRAL_TESTS_SESSION set; runs until killed

Since G7 an app holds its permits only once a ContractRequest for them is
approved and its host signed the contract (astral-docs
protocols/apphost/ops/apphost.request_contract.md). Before G7 the harness
configured the five admission policies to accept; this is what replaces them.
While a test runs, from its driver to its oracle, the runner runs this
answerer beside it, unless the test's manifest sets `answers` (the test
answers SwarmState requests itself, as the swarm-state tests do).

Every `POLL` seconds it lists each node's pending requests with that node's
own token (`user.swarm_requests`) and approves every ContractRequest and
JoinRequest (`user.answer_request`), as the accepting policies admitted apps
and joins before. Only a node whose tier answers requests can: a
Privileged node, or an unclaimed node, which is its own root. A Member's
request reaches a Privileged node through SwarmState, and is approved there.
A privileged_member JoinRequest needs the user key (`user.confirm_join`), so
no node approves it, and ExpelRequests are left alone.

It judges nothing: a refusal, a dead node or a node not yet up is skipped,
and the next round tries again.
"""
import asyncio
import sys
import time

import astral

from lib import jsonops
from lib import swarmstate as ss
from lib.sessionio import load

POLL = 0.5


async def answer_node(node: dict, done: set) -> None:
    """Approve every pending ContractRequest and JoinRequest node lists, with
    its own token."""
    async with await astral.connect(node["endpoint"], token=node["token"]) as c:
        for r in await ss.requests(c, timeout=10):
            if r.type not in (ss.CONTRACT, ss.JOIN) or r.status != "pending" \
                    or r.id in done:
                continue
            try:
                await ss.answer(c, r.id, "approve", timeout=10)
            except jsonops.OpError as e:
                # note: a Member cannot answer; a Privileged node will
                print(f"{time.strftime('%H:%M:%S')} {node['identity'][:8]} "
                      f"{r.id}: {e}", flush=True)
                continue
            done.add(r.id)
            print(f"{time.strftime('%H:%M:%S')} {node['identity'][:8]} approved "
                  f"{r.id} for {r.obj.get('Identity') or r.obj.get('Joiner')}", flush=True)


async def main() -> None:
    done: set = set()
    while True:
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
